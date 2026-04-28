// server — Inhouse League stats web server.
//
// Environment variables:
//
//	DB_PATH            path to the SQLite database file (default: data/inhouse.db)
//	ARCHIVE_DB_PATH    path to the cold-storage SQLite file for archived matches
//	                   (default: sibling of DB_PATH named "inhouse_archive.db")
//	PORT               HTTP listen port (default: 8080)
//	APP_ENV            set to "development" to seed datagen players on startup
//	CONFIRM_THRESHOLD  players that must agree on a match ID before the gate locks (default: 3)
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	archivePath := getEnvOr("ARCHIVE_DB_PATH", filepath.Join(filepath.Dir(dbPath), "inhouse_archive.db"))
	port := getEnvOr("PORT", "8080")
	appEnv := getEnvOr("APP_ENV", "production")
	confirmThreshold := getEnvInt("CONFIRM_THRESHOLD", 3)

	database, err := db.Open(dbPath, archivePath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	// Any matches still in 'in_progress' from a previous run can never be
	// completed (the gate starts closed on every boot), so move them to the
	// archive DB for forensics and free the live tables.
	if n, err := database.ArchiveInProgressMatches(context.Background()); err != nil {
		log.Printf("[server] archive orphaned matches: %v", err)
	} else if n > 0 {
		log.Printf("[server] archived %d orphaned in-progress match(es) from previous run", n)
	}

	gate := match.New(confirmThreshold)
	log.Printf("[server] match confirm threshold: %d", confirmThreshold)

	// Completed match closing (all POST_GAMEs in, idle timeout after completion,
	// or a new lobby being opened): finalize stats from the latest packets and
	// clear live_match_stats. Never deletes the match.
	gate.SetOnFinalize(func(dotaMatchID string) {
		if err := database.FinalizeMatch(context.Background(), dotaMatchID); err != nil {
			log.Printf("[gate] finalize match %s: %v", dotaMatchID, err)
		} else {
			log.Printf("[gate] finalized match %s", dotaMatchID)
		}
	})

	// Never-completed match closing (no POST_GAME ever arrived): archive to the
	// cold-storage DB instead of deleting, so we always have a forensic copy.
	gate.SetOnAbandon(func(dotaMatchID string) {
		if err := database.ArchiveMatch(context.Background(), dotaMatchID); err != nil {
			log.Printf("[gate] archive abandoned match %s: %v (left in main DB)", dotaMatchID, err)
		} else {
			log.Printf("[gate] archived abandoned match %s", dotaMatchID)
		}
	})

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

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
		log.Printf("[server] invalid %s=%q, using default %d", key, v, fallback)
	}
	return fallback
}
