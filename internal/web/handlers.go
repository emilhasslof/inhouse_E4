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
