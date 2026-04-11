// server — Inhouse League stats web server.
//
// Environment variables:
//
//	DB_PATH   path to the SQLite database file (default: inhouse.db)
//	PORT      HTTP listen port (default: 8080)
//	APP_ENV   set to "development" to seed datagen players on startup
package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/emilh/inhouse-e4/internal/bot"
	"github.com/emilh/inhouse-e4/internal/db"
	"github.com/emilh/inhouse-e4/internal/gsi"
	"github.com/emilh/inhouse-e4/internal/match"
	"github.com/emilh/inhouse-e4/internal/web"
)

func main() {
	loadEnv(".env")

	dbPath := getEnvOr("DB_PATH", "data/inhouse.db")
	port := getEnvOr("PORT", "8080")
	appEnv := getEnvOr("APP_ENV", "production")

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	gate := new(match.Gate)

	if appEnv == "development" {
		log.Println("[server] APP_ENV=development — seeding players and dev match data")
		if err := database.Seed(); err != nil {
			log.Printf("[server] seed warning: %v", err)
		}
		if err := database.SeedDevMatches(); err != nil {
			log.Printf("[server] seed matches warning: %v", err)
		}
		// Open the gate in dev mode so datagen can push packets without a bot.
		gate.Open()
		log.Println("[server] dev mode — match gate pre-opened")
	}

	manager := bot.NewManager(gate)

	gsiHandler := gsi.New(database, gate)
	// Pass manager as a web.LobbyCreator interface. When manager is nil (bot not
	// configured) we pass a nil interface, so h.bot != nil checks work correctly.
	var lobbyBot web.LobbyCreator
	if manager != nil {
		lobbyBot = manager
	}
	webHandler := web.New(database, lobbyBot)
	router := web.NewRouter(gsiHandler, webHandler)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("[server] listening on http://localhost%s (APP_ENV=%s, DB=%s)", addr, appEnv, dbPath)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

// loadEnv reads KEY=VALUE pairs from a file into the process environment.
// Silently ignores the file if it doesn't exist.
func loadEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
