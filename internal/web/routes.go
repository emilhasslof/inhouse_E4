package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/emilh/inhouse-e6/internal/gsi"
	webpkg "github.com/emilh/inhouse-e6/web"
)

// NewRouter wires all routes and returns the root http.Handler.
func NewRouter(gsiH *gsi.Handler, webH *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// GSI ingest — receives POST payloads from Dota 2 clients
	r.Post("/gsi", gsiH.Receive)

	// Website pages
	r.Get("/", webH.Matches)
	r.Get("/matches/{id}", webH.Match)
	r.Get("/players", webH.Players)

	// Static assets (CSS, JS)
	r.Handle("/static/*", http.StripPrefix("/static/", webpkg.StaticHandler))

	return r
}
