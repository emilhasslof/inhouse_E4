// Package gsi handles incoming Dota 2 Game State Integration payloads.
package gsi

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/emilh/inhouse-e4/internal/db"
	"github.com/emilh/inhouse-e4/internal/match"
)

// Payload mirrors the JSON structure that Dota 2 sends via GSI.
// Fields not needed for the MVP are omitted.
type Payload struct {
	Auth   AuthBlock   `json:"auth"`
	Map    MapBlock    `json:"map"`
	Player PlayerBlock `json:"player"`
	Hero   HeroBlock   `json:"hero"`
	Draft  DraftBlock  `json:"draft"`
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

// DraftTeam holds the picks and bans for one team in a Captain's Mode draft.
// Slot indices (pick0..pick4, ban0..ban6) reflect the order within each category.
type DraftTeam struct {
	HomeTeam bool   `json:"home_team"`
	Pick0ID  int    `json:"pick0_id"`
	Pick0    string `json:"pick0_class"`
	Pick1ID  int    `json:"pick1_id"`
	Pick1    string `json:"pick1_class"`
	Pick2ID  int    `json:"pick2_id"`
	Pick2    string `json:"pick2_class"`
	Pick3ID  int    `json:"pick3_id"`
	Pick3    string `json:"pick3_class"`
	Pick4ID  int    `json:"pick4_id"`
	Pick4    string `json:"pick4_class"`
	Ban0ID   int    `json:"ban0_id"`
	Ban0     string `json:"ban0_class"`
	Ban1ID   int    `json:"ban1_id"`
	Ban1     string `json:"ban1_class"`
	Ban2ID   int    `json:"ban2_id"`
	Ban2     string `json:"ban2_class"`
	Ban3ID   int    `json:"ban3_id"`
	Ban3     string `json:"ban3_class"`
	Ban4ID   int    `json:"ban4_id"`
	Ban4     string `json:"ban4_class"`
	Ban5ID   int    `json:"ban5_id"`
	Ban5     string `json:"ban5_class"`
	Ban6ID   int    `json:"ban6_id"`
	Ban6     string `json:"ban6_class"`
}

// picks returns the non-zero pick entries in slot order.
func (t DraftTeam) picks() []db.DraftEntry {
	raw := []struct {
		id   int
		name string
	}{
		{t.Pick0ID, t.Pick0}, {t.Pick1ID, t.Pick1}, {t.Pick2ID, t.Pick2},
		{t.Pick3ID, t.Pick3}, {t.Pick4ID, t.Pick4},
	}
	var out []db.DraftEntry
	for i, p := range raw {
		if p.id != 0 {
			out = append(out, db.DraftEntry{Slot: i, HeroID: p.id, HeroName: p.name})
		}
	}
	return out
}

// bans returns the non-zero ban entries in slot order.
func (t DraftTeam) bans() []db.DraftEntry {
	raw := []struct {
		id   int
		name string
	}{
		{t.Ban0ID, t.Ban0}, {t.Ban1ID, t.Ban1}, {t.Ban2ID, t.Ban2}, {t.Ban3ID, t.Ban3},
		{t.Ban4ID, t.Ban4}, {t.Ban5ID, t.Ban5}, {t.Ban6ID, t.Ban6},
	}
	var out []db.DraftEntry
	for i, b := range raw {
		if b.id != 0 {
			out = append(out, db.DraftEntry{Slot: i, HeroID: b.id, HeroName: b.name})
		}
	}
	return out
}

// DraftBlock is the top-level draft object in a GSI payload.
// Team2 = dire, Team3 = radiant (Dota 2 internal team numbering).
// home_team=true on Team3 confirms the radiant mapping.
type DraftBlock struct {
	Team2 DraftTeam `json:"team2"`
	Team3 DraftTeam `json:"team3"`
}

const postGameState = "DOTA_GAMERULES_STATE_POST_GAME"

// Handler handles POST /gsi requests from Dota 2 clients.
type Handler struct {
	db   *db.DB
	gate *match.Gate
}

// New creates a new GSI handler backed by the given database and match gate.
func New(database *db.DB, gate *match.Gate) *Handler {
	return &Handler{db: database, gate: gate}
}

// Receive processes a single GSI payload from a player's client.
func (h *Handler) Receive(w http.ResponseWriter, r *http.Request) {
	// Reject all packets when no lobby is active. Return 200 so Dota doesn't
	// flag the endpoint as broken and stop sending.
	if !h.gate.IsOpen() {
		w.WriteHeader(http.StatusOK)
		return
	}

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

	// Confirmation check: drop packets until 3 registered players agree on the
	// same match ID. Once locked, only the confirmed match ID passes through,
	// which prevents concurrent matchmaking games from polluting the stats.
	if !h.gate.Accept(p.Map.MatchID, player.SteamID) {
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
		// Persist the Captain's Mode draft if the payload carries pick/ban data.
		// Team3 = radiant, Team2 = dire (Dota 2 internal team numbering).
		// INSERT OR IGNORE means only the first POST_GAME packet per slot wins,
		// which is fine — the draft is immutable by this point.
		radiant, dire := p.Draft.Team3, p.Draft.Team2
		if picks := radiant.picks(); len(picks) > 0 {
			if err := h.db.UpsertMatchDraft(r.Context(), matchID, "radiant", true, picks); err != nil {
				log.Printf("[gsi] upsert radiant picks: %v", err)
			}
		}
		if bans := radiant.bans(); len(bans) > 0 {
			if err := h.db.UpsertMatchDraft(r.Context(), matchID, "radiant", false, bans); err != nil {
				log.Printf("[gsi] upsert radiant bans: %v", err)
			}
		}
		if picks := dire.picks(); len(picks) > 0 {
			if err := h.db.UpsertMatchDraft(r.Context(), matchID, "dire", true, picks); err != nil {
				log.Printf("[gsi] upsert dire picks: %v", err)
			}
		}
		if bans := dire.bans(); len(bans) > 0 {
			if err := h.db.UpsertMatchDraft(r.Context(), matchID, "dire", false, bans); err != nil {
				log.Printf("[gsi] upsert dire bans: %v", err)
			}
		}

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
		// Match is over — close the gate so future packets (from e.g. datagen
		// or a second client that didn't get the post-game state yet) are dropped.
		h.gate.Close()
		log.Println("[gsi] match completed — gate closed")
	}

	w.WriteHeader(http.StatusOK)
}
