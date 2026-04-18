package gsi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emilh/inhouse-e4/internal/db"
	"github.com/emilh/inhouse-e4/internal/gsi"
	"github.com/emilh/inhouse-e4/internal/match"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

// openGate returns a match gate that is already open, for use in tests that
// exercise GSI processing directly without going through the bot flow.
func openGate() *match.Gate {
	g := match.New(1)
	g.Open()
	return g
}

func newSeededDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	require.NoError(t, d.Seed())
	return d
}

// sendGSI posts a payload to the GSI handler and returns the response.
func sendGSI(t *testing.T, h *gsi.Handler, payload any) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/gsi", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Receive(w, req)
	return w.Result()
}

// inProgressPayload builds a standard in-progress GSI payload for the given steamID.
func inProgressPayload(steamID, matchID string) map[string]any {
	return map[string]any{
		"map": map[string]any{
			"matchid":       matchID,
			"clock_time":    120,
			"game_time":     120,
			"game_state":    "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS",
			"radiant_score": 3,
			"dire_score":    2,
		},
		"player": map[string]any{
			"steamid":   steamID,
			"team_name": "radiant",
			"kills":     2,
			"deaths":    1,
			"assists":   3,
			"gold":      2400,
			"gpm":       400,
			"xpm":       380,
			"last_hits": 45,
			"denies":    2,
		},
		"hero": map[string]any{
			"name":  "npc_dota_hero_anti_mage",
			"level": 8,
		},
	}
}

// postGamePayload builds a POST_GAME GSI payload for the given token.
func postGamePayload(token, matchID string) map[string]any {
	p := inProgressPayload(token, matchID)
	p["map"].(map[string]any)["game_state"] = "DOTA_GAMERULES_STATE_POST_GAME"
	p["map"].(map[string]any)["clock_time"] = 2340
	p["map"].(map[string]any)["game_time"] = 2340
	p["map"].(map[string]any)["radiant_score"] = 30
	p["map"].(map[string]any)["dire_score"] = 20
	p["player"].(map[string]any)["kills"] = 10
	p["player"].(map[string]any)["deaths"] = 2
	p["player"].(map[string]any)["gpm"] = 550
	p["hero"].(map[string]any)["level"] = 25
	return p
}

// confirmMatch sends minimal in-progress packets from registered players so the
// gate locks to matchID. Requires a seeded DB (datagen-steam-r1/r2/r3 must exist).
func confirmMatch(t *testing.T, h *gsi.Handler, matchID string) {
	t.Helper()
	for _, steamID := range []string{"datagen-steam-r1", "datagen-steam-r2", "datagen-steam-r3"} {
		sendGSI(t, h, map[string]any{
			"map": map[string]any{
				"matchid":    matchID,
				"clock_time": 30,
				"game_time":  30,
				"game_state": "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS",
			},
			"player": map[string]any{"steamid": steamID},
			"hero":   map[string]any{},
		})
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestReceive_UnregisteredSteamID_NoLockedMatch(t *testing.T) {
	// When the gate is open but not yet locked, an unregistered packet is
	// dropped (no orphan row written) because we have no locked match ID to
	// attribute it to. Response is 200 so Dota keeps sending.
	d := newSeededDB(t)
	h := gsi.New(d, openGate())

	resp := sendGSI(t, h, map[string]any{
		"map":    map[string]any{"matchid": "match-xxx", "game_state": "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS"},
		"player": map[string]any{"steamid": "not-a-real-steam-id"},
	})
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	orphans, err := d.ListOrphans(context.Background(), 10)
	require.NoError(t, err)
	assert.Empty(t, orphans)
}

func TestReceive_UnregisteredSteamID_LockedMatch_CapturesOrphan(t *testing.T) {
	d := newSeededDB(t)
	h := gsi.New(d, openGate())
	confirmMatch(t, h, "match-locked-orphan")

	resp := sendGSI(t, h, map[string]any{
		"map": map[string]any{
			"matchid":    "match-locked-orphan",
			"clock_time": 142,
			"game_state": "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS",
		},
		"player": map[string]any{
			"steamid": "76561199999999999",
			"kills":   4,
		},
	})
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	orphans, err := d.ListOrphans(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, orphans, 1)
	o := orphans[0]
	assert.Equal(t, "match-locked-orphan", o.DotaMatchID)
	assert.Equal(t, "76561199999999999", o.SteamID)
	assert.Equal(t, 142, o.ClockTime)
	assert.Equal(t, "unregistered", o.DropReason)
	assert.Contains(t, o.Payload, "76561199999999999")
}

func TestReceive_NoMatchID(t *testing.T) {
	d := newSeededDB(t)
	h := gsi.New(d, openGate())

	resp := sendGSI(t, h, map[string]any{
		"map":    map[string]any{"matchid": ""},
		"player": map[string]any{"steamid": "datagen-steam-r1"},
	})
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	matches, err := d.ListMatches(context.Background())
	require.NoError(t, err)
	assert.Empty(t, matches)
}

func TestReceive_InProgress(t *testing.T) {
	d := newSeededDB(t)
	h := gsi.New(d, openGate())
	confirmMatch(t, h, "match-live-001")

	resp := sendGSI(t, h, inProgressPayload("datagen-steam-r1", "match-live-001"))
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	matches, err := d.ListMatches(context.Background())
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "in_progress", matches[0].State)

	// Live stats are populated from live_match_stats for in-progress matches.
	// confirmMatch sends r1/r2/r3 with no team_name (empty string → Dire bucket).
	// The subsequent inProgressPayload from r1 upserts Spinelli into Radiant.
	detail, err := d.GetMatchDetail(context.Background(), matches[0].ID)
	require.NoError(t, err)
	require.Len(t, detail.Radiant, 1)
	assert.Equal(t, "Spinelli", detail.Radiant[0].DisplayName)
	assert.Equal(t, 2, detail.Radiant[0].Kills)
	assert.Equal(t, "npc_dota_hero_anti_mage", detail.Radiant[0].HeroName)
	assert.Equal(t, 120, detail.Radiant[0].ClockTime)
	assert.Len(t, detail.Dire, 2) // Sku + Jockwe Lamotte from confirmMatch (no team_name)
}

func TestReceive_PostGame(t *testing.T) {
	d := newSeededDB(t)
	h := gsi.New(d, openGate())
	ctx := context.Background()
	confirmMatch(t, h, "match-pg-001")

	resp := sendGSI(t, h, postGamePayload("datagen-steam-r1", "match-pg-001"))
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	matches, err := d.ListMatches(ctx)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "completed", matches[0].State)
	assert.Equal(t, 30, matches[0].RadiantScore)
	assert.Equal(t, 20, matches[0].DireScore)

	detail, err := d.GetMatchDetail(ctx, matches[0].ID)
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.Len(t, detail.Radiant, 1)
	assert.Equal(t, "Spinelli", detail.Radiant[0].DisplayName)
	assert.Equal(t, 10, detail.Radiant[0].Kills)
}

func TestReceive_PostGame_Idempotent(t *testing.T) {
	d := newSeededDB(t)
	h := gsi.New(d, openGate())
	ctx := context.Background()
	confirmMatch(t, h, "match-idem-001")

	payload := postGamePayload("datagen-steam-r1", "match-idem-001")
	sendGSI(t, h, payload)
	sendGSI(t, h, payload) // send again

	matches, err := d.ListMatches(ctx)
	require.NoError(t, err)
	require.Len(t, matches, 1)

	detail, err := d.GetMatchDetail(ctx, matches[0].ID)
	require.NoError(t, err)
	// Still only one stats row for Spinelli, not two
	assert.Len(t, detail.Radiant, 1)
	// Scores unchanged (CompleteMatch guards against re-completion)
	assert.Equal(t, 30, matches[0].RadiantScore)
}

func TestReceive_InvalidJSON(t *testing.T) {
	d := newSeededDB(t)
	h := gsi.New(d, openGate())

	req := httptest.NewRequest(http.MethodPost, "/gsi", bytes.NewBufferString("not json {{{"))
	w := httptest.NewRecorder()
	h.Receive(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
