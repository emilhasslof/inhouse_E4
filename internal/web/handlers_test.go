package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emilh/inhouse-e4/internal/db"
	"github.com/emilh/inhouse-e4/internal/gsi"
	"github.com/emilh/inhouse-e4/internal/match"
	"github.com/emilh/inhouse-e4/internal/web"
)


// ── Helpers ───────────────────────────────────────────────────────────────────

func newTestServer(t *testing.T, d *db.DB) *httptest.Server {
	t.Helper()
	gate := new(match.Gate)
	srv := httptest.NewServer(web.NewRouter(gsi.New(d, gate), web.New(d, nil)))
	t.Cleanup(srv.Close)
	return srv
}

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func getJSON(t *testing.T, url string, dst any) *http.Response {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if dst != nil {
		require.NoError(t, json.Unmarshal(body, dst), "body: %s", body)
	}
	return resp
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestMatchesHandler_Empty(t *testing.T) {
	srv := newTestServer(t, newTestDB(t))

	var result json.RawMessage
	resp := getJSON(t, srv.URL+"/api/matches", &result)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	assert.Equal(t, "[]", string(result))
}

func TestMatchHandler_NotFound(t *testing.T) {
	srv := newTestServer(t, newTestDB(t))

	resp, err := http.Get(srv.URL + "/api/matches/99999")
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestMatchHandler_NilSlices(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	// Match exists but has no player stats yet
	_, err := d.UpsertMatch(ctx, "match-noslots")
	require.NoError(t, err)

	srv := newTestServer(t, d)

	var result map[string]json.RawMessage
	resp := getJSON(t, srv.URL+"/api/matches/1", &result)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "[]", string(result["radiant"]))
	assert.Equal(t, "[]", string(result["dire"]))
}

func TestPlayersHandler_Empty(t *testing.T) {
	srv := newTestServer(t, newTestDB(t))

	var result json.RawMessage
	resp := getJSON(t, srv.URL+"/api/players", &result)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "[]", string(result))
}

func TestPlayersHandler_StreakPopulated(t *testing.T) {
	d := newTestDB(t)
	require.NoError(t, d.Seed())
	ctx := context.Background()

	p, err := d.PlayerByToken(ctx, "datagen-radiant-1")
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		matchID, err := d.UpsertMatch(ctx, fmt.Sprintf("streak-match-%d", i))
		require.NoError(t, err)
		require.NoError(t, d.UpsertMatchPlayerStat(ctx, matchID, p.ID,
			"npc_dota_hero_axe", "radiant", 5, 2, 3, 450, 400, 100, 5, 20))
		require.NoError(t, d.CompleteMatch(ctx, matchID, 30, 10, "radiant", 2000))
	}

	srv := newTestServer(t, d)

	var players []map[string]json.RawMessage
	resp := getJSON(t, srv.URL+"/api/players", &players)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	for _, player := range players {
		if string(player["display_name"]) == `"Spinelli"` {
			var streak int
			require.NoError(t, json.Unmarshal(player["streak"], &streak))
			assert.Equal(t, 2, streak)
			return
		}
	}
	t.Fatal("Spinelli not found in /api/players response")
}

func TestLeagueOverviewHandler(t *testing.T) {
	srv := newTestServer(t, newTestDB(t))

	var result map[string]json.RawMessage
	resp := getJSON(t, srv.URL+"/api/stats/overview", &result)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	for _, key := range []string{
		"total_matches", "total_kills", "avg_match_duration_secs",
		"longest_match_secs", "shortest_match_secs",
		"most_kills_in_match", "highest_kda_player", "bloodiest_match",
	} {
		assert.Contains(t, result, key, "missing key %q", key)
	}
}
