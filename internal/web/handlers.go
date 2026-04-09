// Package web contains JSON API handlers for the stats frontend.
package web

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/emilh/inhouse-e4/internal/db"
)

// Handler serves the JSON API backed by the database.
type Handler struct {
	db *db.DB
}

// New creates an API handler.
func New(database *db.DB) *Handler {
	return &Handler{db: database}
}

var specJSON = []byte(`{
  "endpoints": [
    {
      "method": "GET",
      "path": "/api/matches",
      "description": "All matches, newest first.",
      "returns": "{ id, dota_match_id, state, radiant_score, dire_score, duration_secs, started_at, radiant_players?, dire_players? }[]"
    },
    {
      "method": "GET",
      "path": "/api/matches/:id",
      "description": "Full scoreboard for a single match.",
      "returns": "{ match: MatchSummary, radiant: PlayerStat[], dire: PlayerStat[] }"
    },
    {
      "method": "GET",
      "path": "/api/players",
      "description": "Player leaderboard sorted by avg GPM.",
      "returns": "{ id, display_name, matches_played, wins, losses, total_kills, total_deaths, total_assists, avg_gpm, streak }[]"
    },
    {
      "method": "GET",
      "path": "/api/stats/heroes",
      "description": "Hero pick and win counts across all completed matches.",
      "returns": "{ hero_name, picks, wins, bans }[]"
    },
    {
      "method": "GET",
      "path": "/api/stats/overview",
      "description": "League-wide aggregate stats.",
      "returns": "{ total_matches, total_kills, avg_match_duration_secs, longest_match_secs, shortest_match_secs, most_kills_in_match, highest_kda_player, bloodiest_match }"
    }
  ]
}`)

// Spec handles GET /api — returns a JSON listing of all available endpoints.
func (h *Handler) Spec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(specJSON)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[api] encode response: %v", err)
	}
}

// Matches handles GET /api/matches
func (h *Handler) Matches(w http.ResponseWriter, r *http.Request) {
	matches, err := h.db.ListMatches(r.Context())
	if err != nil {
		log.Printf("[api] list matches: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if matches == nil {
		matches = []db.MatchSummary{}
	}
	writeJSON(w, http.StatusOK, matches)
}

// Match handles GET /api/matches/{id}
func (h *Handler) Match(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	detail, err := h.db.GetMatchDetail(r.Context(), id)
	if err != nil {
		log.Printf("[api] get match detail %d: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if detail == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	// Ensure nil slices serialize as [] not null.
	if detail.Radiant == nil {
		detail.Radiant = []db.PlayerStatRow{}
	}
	if detail.Dire == nil {
		detail.Dire = []db.PlayerStatRow{}
	}
	writeJSON(w, http.StatusOK, detail)
}

// Players handles GET /api/players
func (h *Handler) Players(w http.ResponseWriter, r *http.Request) {
	entries, err := h.db.ListPlayers(r.Context())
	if err != nil {
		log.Printf("[api] list players: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	streaks, err := h.db.ListPlayerStreaks(r.Context())
	if err != nil {
		log.Printf("[api] list player streaks: %v", err)
		// Non-fatal — proceed with streak = 0
	}
	for i := range entries {
		if s, ok := streaks[entries[i].ID]; ok {
			entries[i].Streak = s
		}
	}

	if entries == nil {
		entries = []db.LeaderboardEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// HeroStats handles GET /api/stats/heroes
func (h *Handler) HeroStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.db.HeroStats(r.Context())
	if err != nil {
		log.Printf("[api] hero stats: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if stats == nil {
		stats = []db.HeroStat{}
	}
	writeJSON(w, http.StatusOK, stats)
}

// Register handles POST /api/register
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SteamID     string `json:"steam_id"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if body.SteamID == "" || body.DisplayName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "steam_id and display_name are required"})
		return
	}

	player, err := h.db.RegisterPlayer(r.Context(), body.SteamID, body.DisplayName)
	if err != nil {
		log.Printf("[api] register player %s: %v", body.SteamID, err)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "player already registered"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"token": player.Token})
}

// LeagueOverview handles GET /api/stats/overview
func (h *Handler) LeagueOverview(w http.ResponseWriter, r *http.Request) {
	ov, err := h.db.GetLeagueOverview(r.Context())
	if err != nil {
		log.Printf("[api] league overview: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, ov)
}
