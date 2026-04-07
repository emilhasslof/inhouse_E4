// Package web contains HTTP handlers for the stats website pages.
package web

import (
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/emilh/inhouse-e6/internal/db"
	webpkg "github.com/emilh/inhouse-e6/web"
)

// Handler serves the website pages backed by the database.
type Handler struct {
	db *db.DB
}

// New creates a page handler.
func New(database *db.DB) *Handler {
	return &Handler{db: database}
}

// Matches renders the match list (home page).
func (h *Handler) Matches(w http.ResponseWriter, r *http.Request) {
	matches, err := h.db.ListMatches(r.Context())
	if err != nil {
		log.Printf("[web] list matches: %v", err)
		matches = nil
	}
	if err := webpkg.MatchesTemplate.ExecuteTemplate(w, "layout", matches); err != nil {
		log.Printf("[web] render matches: %v", err)
	}
}

// Match renders the scoreboard for a single match.
func (h *Handler) Match(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	detail, err := h.db.GetMatchDetail(r.Context(), id)
	if err != nil {
		log.Printf("[web] get match detail %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if detail == nil {
		http.NotFound(w, r)
		return
	}

	if err := webpkg.MatchTemplate.ExecuteTemplate(w, "layout", detail); err != nil {
		log.Printf("[web] render match: %v", err)
	}
}

// Players renders the player leaderboard.
func (h *Handler) Players(w http.ResponseWriter, r *http.Request) {
	players, err := h.db.ListPlayers(r.Context())
	if err != nil {
		log.Printf("[web] list players: %v", err)
		players = nil
	}
	if err := webpkg.PlayersTemplate.ExecuteTemplate(w, "layout", players); err != nil {
		log.Printf("[web] render players: %v", err)
	}
}
