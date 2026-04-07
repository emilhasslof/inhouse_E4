// Package web holds the embedded static assets and pre-parsed HTML templates.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed templates static
var files embed.FS

// StaticHandler serves files under /static/.
var StaticHandler http.Handler

// Pre-parsed templates — each page bundles layout + its own content block.
var (
	MatchesTemplate *template.Template
	MatchTemplate   *template.Template
	PlayersTemplate *template.Template
)

var funcMap = template.FuncMap{
	// heroName strips the "npc_dota_hero_" prefix and title-cases the result.
	"heroName": func(s string) string {
		s = strings.TrimPrefix(s, "npc_dota_hero_")
		parts := strings.Split(s, "_")
		for i, p := range parts {
			if len(p) > 0 {
				parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
			}
		}
		return strings.Join(parts, " ")
	},
	// fmtDuration formats seconds as "mm:ss". Returns "—" for zero.
	"fmtDuration": func(secs int) string {
		if secs <= 0 {
			return "—"
		}
		return fmt.Sprintf("%d:%02d", secs/60, secs%60)
	},
	// formatTime converts a Unix epoch to a human-readable date+time string.
	"formatTime": func(epoch int64) string {
		if epoch == 0 {
			return "—"
		}
		return time.Unix(epoch, 0).Format("Jan 2, 2006  15:04")
	},
	// fmtFloat formats a float64 with zero decimal places.
	"fmtFloat": func(f float64) string {
		return fmt.Sprintf("%.0f", f)
	},
	// inc adds 1 to an int (used for 1-based rank display).
	"inc": func(i int) int { return i + 1 },
	// not negates a boolean (template helper for {{if not .}}).
	"not": func(v any) bool {
		if v == nil {
			return true
		}
		return false
	},
}

func init() {
	staticSub, err := fs.Sub(files, "static")
	if err != nil {
		panic("web: cannot sub static: " + err.Error())
	}
	StaticHandler = http.FileServer(http.FS(staticSub))

	MatchesTemplate = mustParse("templates/layout.html", "templates/matches.html")
	MatchTemplate   = mustParse("templates/layout.html", "templates/match.html")
	PlayersTemplate = mustParse("templates/layout.html", "templates/players.html")
}

func mustParse(paths ...string) *template.Template {
	t := template.New("").Funcs(funcMap)
	return template.Must(t.ParseFS(files, paths...))
}
