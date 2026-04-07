// Package gsi handles incoming Dota 2 Game State Integration payloads.
package gsi

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/emilh/inhouse-e6/internal/db"
)

// Payload mirrors the JSON structure that Dota 2 sends via GSI.
// Fields not needed for the MVP are omitted.
type Payload struct {
	Auth   AuthBlock   `json:"auth"`
	Map    MapBlock    `json:"map"`
	Player PlayerBlock `json:"player"`
	Hero   HeroBlock   `json:"hero"`
}

// AuthBlock carries the player's pre-shared auth token.
type AuthBlock struct {
	Token string `json:"token"`
}

// MapBlock carries match-level state.
type MapBlock struct {
	MatchID      string `json:"matchid"`
	ClockTime    int    `json:"clock_time"`
	GameTime     int    `json:"game_time"`
	GameState    string `json:"game_state"`
	RadiantScore int    `json:"radiant_score"`
	DireScore    int    `json:"dire_score"`
}

// PlayerBlock carries per-player stats (from the reporting player's own perspective).
type PlayerBlock struct {
	SteamID  string `json:"steamid"`
	TeamName string `json:"team_name"`
	Kills    int    `json:"kills"`
	Deaths   int    `json:"deaths"`
	Assists  int    `json:"assists"`
	Gold     int    `json:"gold"`
	GPM      int    `json:"gpm"`
	XPM      int    `json:"xpm"`
	LastHits int    `json:"last_hits"`
	Denies   int    `json:"denies"`
}

// HeroBlock carries hero-specific state.
type HeroBlock struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
}

const postGameState = "DOTA_GAMERULES_STATE_POST_GAME"

// Handler handles POST /gsi requests from Dota 2 clients.
type Handler struct {
	db *db.DB
}

// New creates a new GSI handler backed by the given database.
func New(database *db.DB) *Handler {
	return &Handler{db: database}
}

// Receive processes a single GSI payload from a player's client.
func (h *Handler) Receive(w http.ResponseWriter, r *http.Request) {
	var p Payload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	// Authenticate via pre-shared per-player token.
	player, err := h.db.PlayerByToken(r.Context(), p.Auth.Token)
	if err != nil {
		http.Error(w, "", http.StatusUnauthorized)
		return
	}

	// No match ID means we're in a menu or draft — nothing to record.
	if p.Map.MatchID == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	matchID, err := h.db.UpsertMatch(r.Context(), p.Map.MatchID)
	if err != nil {
		log.Printf("[gsi] upsert match %s: %v", p.Map.MatchID, err)
		w.WriteHeader(http.StatusOK) // return 200 to Dota regardless
		return
	}

	if err := h.db.InsertSnapshot(r.Context(),
		matchID, player.ID,
		p.Map.ClockTime,
		p.Player.Kills, p.Player.Deaths, p.Player.Assists,
		p.Player.Gold, p.Player.GPM, p.Player.XPM,
		p.Player.LastHits, p.Player.Denies,
		p.Hero.Level, p.Hero.Name, p.Player.TeamName,
	); err != nil {
		log.Printf("[gsi] insert snapshot for match %s player %d: %v", p.Map.MatchID, player.ID, err)
	}

	if p.Map.GameState == postGameState {
		if err := h.db.UpsertMatchPlayerStat(r.Context(),
			matchID, player.ID,
			p.Hero.Name, p.Player.TeamName,
			p.Player.Kills, p.Player.Deaths, p.Player.Assists,
			p.Player.GPM, p.Player.XPM,
			p.Player.LastHits, p.Player.Denies, p.Hero.Level,
		); err != nil {
			log.Printf("[gsi] upsert match_player_stat: %v", err)
		}
		if err := h.db.CompleteMatch(r.Context(), matchID,
			p.Map.RadiantScore, p.Map.DireScore, p.Map.GameTime); err != nil {
			log.Printf("[gsi] complete match %d: %v", matchID, err)
		}
	}

	w.WriteHeader(http.StatusOK)
}
