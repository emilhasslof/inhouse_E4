// Package web contains JSON API handlers for the stats frontend.
package web

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/emilh/inhouse-e4/internal/db"
)

// LobbyCreator is the subset of bot.Service used by the web handlers.
// Defined as an interface so the web package does not need to import bot,
// keeping go-steam out of test binaries.
type LobbyCreator interface {
	CreateLobbyAndInvite(players []db.Player, gameMode string)
	Reset()
}

// Handler serves the JSON API backed by the database.
type Handler struct {
	db  *db.DB
	bot LobbyCreator
}

// New creates an API handler. Pass nil for bot when the Steam bot is not
// configured — lobby creation will be silently skipped.
func New(database *db.DB, bot LobbyCreator) *Handler {
	return &Handler{db: database, bot: bot}
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
      "description": "Full scoreboard for a single match. Shape differs by match.state: completed matches include final stats; in-progress matches include live stats with gold and clock_time.",
      "returns": "{ match: { id, dota_match_id, state, win_team, radiant_score, dire_score, duration_secs, started_at }, radiant: PlayerStat[], dire: PlayerStat[] }",
      "notes": "PlayerStat: { display_name, hero_name, team_name, kills, deaths, assists, gpm, xpm, last_hits, denies, final_level, gold? (live only), clock_time? (live only) }"
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
    },
    {
      "method": "GET",
      "path": "/api/registered-players",
      "description": "All registered players with their display name and Steam ID.",
      "returns": "{ display_name, steam_id }[]"
    },
    {
      "method": "GET",
      "path": "/api/matches/:id/draft",
      "description": "Captain's Mode draft for a match — picks and bans in slot order per team. 404 if no draft data (All Pick matches have none).",
      "returns": "{ radiant: { picks: { slot, hero_id, hero_name }[], bans: { slot, hero_id, hero_name }[] }, dire: { ... } }"
    },
    {
      "method": "POST",
      "path": "/api/register",
      "description": "Register a new player. Returns a GSI auth token. 409 if the Steam ID is already registered.",
      "body": "{ steam_id: string, display_name: string }",
      "returns": "{ token: string }"
    },
    {
      "method": "POST",
      "path": "/api/lobby/create",
      "description": "Create a Dota 2 lobby and invite the given players. Cheats are always enabled. Returns immediately. 400 if any Steam ID is not registered.",
      "body": "{ steam_ids: string[], game_mode?: 'captains_mode' | 'all_pick' (default: 'captains_mode') }",
      "returns": "{ ok: true }"
    },
    {
      "method": "POST",
      "path": "/api/lobby/reset",
      "description": "Abandon the current lobby and cancel any pending !start waiter. No-op if no lobby is active. 503 if the bot is not configured.",
      "returns": "{ ok: true }"
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

// RegisteredPlayers handles GET /api/registered-players
func (h *Handler) RegisteredPlayers(w http.ResponseWriter, r *http.Request) {
	players, err := h.db.ListRegisteredPlayers(r.Context())
	if err != nil {
		log.Printf("[api] list registered players: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	type playerView struct {
		DisplayName string `json:"display_name"`
		SteamID     string `json:"steam_id"`
	}
	out := make([]playerView, len(players))
	for i, p := range players {
		out[i] = playerView{DisplayName: p.DisplayName, SteamID: p.SteamID}
	}
	writeJSON(w, http.StatusOK, out)
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
	writeJSON(w, http.StatusCreated, map[string]string{"display_name": player.DisplayName, "steam_id": player.SteamID})
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

// CreateLobby handles POST /api/lobby/create
// Resolves Steam IDs to DB records, then triggers the bot to create a Dota 2
// lobby and invite each player. Returns 400 if any Steam ID is not registered.
// game_mode accepts "captains_mode" or "all_pick"; defaults to "captains_mode".
func (h *Handler) CreateLobby(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SteamIDs []string `json:"steam_ids"`
		GameMode string   `json:"game_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.SteamIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "steam_ids list required"})
		return
	}
	if body.GameMode != "all_pick" {
		body.GameMode = "captains_mode"
	}

	players, err := h.db.PlayersBySteamIDs(r.Context(), body.SteamIDs)
	if err != nil {
		log.Printf("[api] lobby create — player lookup: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if len(players) != len(body.SteamIDs) {
		found := make(map[string]bool, len(players))
		for _, p := range players {
			found[p.SteamID] = true
		}
		var unmatched []string
		for _, id := range body.SteamIDs {
			if !found[id] {
				unmatched = append(unmatched, id)
			}
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     "unrecognised steam IDs",
			"unmatched": unmatched,
		})
		return
	}

	if h.bot != nil {
		go h.bot.CreateLobbyAndInvite(players, body.GameMode)
	} else {
		log.Println("[api] lobby create — bot not configured, skipping")
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// MatchDraft handles GET /api/matches/{id}/draft
func (h *Handler) MatchDraft(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	draft, err := h.db.GetMatchDraft(r.Context(), id)
	if err != nil {
		log.Printf("[api] get match draft %d: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if draft == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no draft data for this match"})
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

// ResetLobby handles POST /api/lobby/reset — restarts the entire process.
// Railway detects the exit and brings the container back up automatically.
func (h *Handler) ResetLobby(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	go func() {
		time.Sleep(100 * time.Millisecond) // let the response flush
		os.Exit(0)
	}()
}
