// capture — GSI packet inspector for the inhouse experiment.
//
// Runs a local HTTP server that accepts raw Dota 2 GSI payloads and saves
// every packet to disk as pretty-printed JSON, organised by game phase:
//
//	packets/menu/     — no active match (hero picking menus, main menu)
//	packets/pregame/  — match loaded, before clock starts (draft, strategy, pre-game countdown)
//	packets/ingame/   — clock running (DOTA_GAMERULES_STATE_GAME_IN_PROGRESS)
//	packets/postgame/ — match ended (DOTA_GAMERULES_STATE_POST_GAME)
//
// Filenames: <seq>_<game_state>.json  (seq is zero-padded so they sort correctly)
//
// Usage:
//
//	go run ./cmd/capture            # listens on :7373
//	go run ./cmd/capture -port 9000 # custom port
//
// Point your GSI config at http://localhost:<port>/gsi and start a game.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

var seq atomic.Int64

const outDir = "packets"

var phases = []string{"menu", "pregame", "ingame", "postgame"}

func main() {
	port := flag.String("port", "7373", "port to listen on")
	flag.Parse()

	// Create phase directories upfront.
	for _, p := range phases {
		if err := os.MkdirAll(filepath.Join(outDir, p), 0o755); err != nil {
			log.Fatalf("mkdir %s: %v", p, err)
		}
	}

	http.HandleFunc("/gsi", handle)

	addr := fmt.Sprintf(":%s", *port)
	log.Printf("[capture] listening on http://localhost%s/gsi", addr)
	log.Printf("[capture] saving packets to ./%s/{menu,pregame,ingame,postgame}/", outDir)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	// Decode into a generic map so we can inspect any field Dota sends,
	// not just the ones our Payload struct knows about.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		log.Printf("[capture] bad JSON: %v", err)
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	phase, gameState := classify(top)
	n := seq.Add(1)

	// Sanitise game_state for use in a filename.
	safeName := strings.ReplaceAll(gameState, " ", "_")
	if safeName == "" {
		safeName = "no_state"
	}

	filename := fmt.Sprintf("%06d_%s.json", n, safeName)
	path := filepath.Join(outDir, phase, filename)

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		// Fall back to raw if indent fails (shouldn't happen).
		pretty.Write(raw)
	}

	if err := os.WriteFile(path, pretty.Bytes(), 0o644); err != nil {
		log.Printf("[capture] write %s: %v", path, err)
	} else {
		log.Printf("[capture] #%06d  %-10s  %s", n, phase, gameState)
	}

	w.WriteHeader(http.StatusOK)
}

// classify returns the phase directory name and the raw game_state string
// extracted from the payload's map block.
func classify(top map[string]json.RawMessage) (phase, gameState string) {
	mapRaw, ok := top["map"]
	if !ok {
		return "menu", ""
	}

	var m struct {
		MatchID   string `json:"matchid"`
		GameState string `json:"game_state"`
	}
	if err := json.Unmarshal(mapRaw, &m); err != nil || m.MatchID == "" {
		return "menu", m.GameState
	}

	switch m.GameState {
	case "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS":
		return "ingame", m.GameState
	case "DOTA_GAMERULES_STATE_POST_GAME":
		return "postgame", m.GameState
	default:
		// Everything else with a match ID is some pre-game phase:
		// WAIT_FOR_PLAYERS_TO_LOAD, HERO_SELECTION, STRATEGY_TIME, PRE_GAME, etc.
		return "pregame", m.GameState
	}
}
