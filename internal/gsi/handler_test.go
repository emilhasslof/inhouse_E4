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
)

// ── Helpers ───────────────────────────────────────────────────────────────────

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

// inProgressPayload builds a standard in-progress GSI payload for the given token.
func inProgressPayload(token, matchID string) map[string]any {
	return map[string]any{
		"auth": map[string]any{"token": token},
		"map": map[string]any{
			"matchid":       matchID,
			"clock_time":    120,
			"game_time":     120,
			"game_state":    "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS",
			"radiant_score": 3,
			"dire_score":    2,
		},
		"player": map[string]any{
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

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestReceive_UnknownToken(t *testing.T) {
	d := newSeededDB(t)
	h := gsi.New(d)

	resp := sendGSI(t, h, map[string]any{
		"auth": map[string]any{"token": "not-a-real-token"},
	})
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestReceive_NoMatchID(t *testing.T) {
	d := newSeededDB(t)
	h := gsi.New(d)

	resp := sendGSI(t, h, map[string]any{
		"auth": map[string]any{"token": "datagen-radiant-1"},
		"map":  map[string]any{"matchid": ""},
	})
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	matches, err := d.ListMatches(context.Background())
	require.NoError(t, err)
	assert.Empty(t, matches)
}

func TestReceive_InProgress(t *testing.T) {
	d := newSeededDB(t)
	h := gsi.New(d)

	resp := sendGSI(t, h, inProgressPayload("datagen-radiant-1", "match-live-001"))
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	matches, err := d.ListMatches(context.Background())
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "in_progress", matches[0].State)

	// No player stats materialised for in-progress match
	detail, err := d.GetMatchDetail(context.Background(), matches[0].ID)
	require.NoError(t, err)
	assert.Empty(t, detail.Radiant)
	assert.Empty(t, detail.Dire)
}

func TestReceive_PostGame(t *testing.T) {
	d := newSeededDB(t)
	h := gsi.New(d)
	ctx := context.Background()

	resp := sendGSI(t, h, postGamePayload("datagen-radiant-1", "match-pg-001"))
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
	h := gsi.New(d)
	ctx := context.Background()

	payload := postGamePayload("datagen-radiant-1", "match-idem-001")
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
	h := gsi.New(d)

	req := httptest.NewRequest(http.MethodPost, "/gsi", bytes.NewBufferString("not json {{{"))
	w := httptest.NewRecorder()
	h.Receive(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
