package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/emilh/inhouse-e4/internal/gsi"
)

// NewRouter wires all routes and returns the root http.Handler.
func NewRouter(gsiH *gsi.Handler, webH *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Health check — used by Railway and other orchestrators
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// GSI ingest — receives POST payloads from Dota 2 clients
	r.Post("/gsi", gsiH.Receive)

	// JSON API
	r.Post("/api/register", webH.Register)
	r.Get("/api", webH.Spec)
	r.Get("/api/matches", webH.Matches)
	r.Get("/api/matches/{id}", webH.Match)
	r.Get("/api/players", webH.Players)
	r.Get("/api/stats/heroes", webH.HeroStats)
	r.Get("/api/stats/overview", webH.LeagueOverview)
	r.Post("/api/lobby/create", webH.CreateLobby)

	return r
}

// corsMiddleware adds permissive CORS headers so the frontend can call the API
// from a different origin during development and production.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
