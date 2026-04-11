package db

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func newTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(":memory:")
	require.NoError(t, err, "open in-memory db")
	t.Cleanup(func() { d.Close() })
	return d
}

// insertPlayer inserts a player directly and returns its ID.
func insertPlayer(t *testing.T, d *DB, name, token string) int64 {
	t.Helper()
	res, err := d.conn.Exec(
		`INSERT INTO players (steam_id, display_name, token) VALUES (?, ?, ?)`,
		"steam-"+token, name, token,
	)
	require.NoError(t, err)
	id, _ := res.LastInsertId()
	return id
}

// buildCompletedMatch creates a match, inserts player stat rows, and marks it
// completed. startedAt controls ordering for streak tests.
func buildCompletedMatch(
	t *testing.T, d *DB,
	dotaMatchID string,
	radiantScore, direScore, durationSecs int,
	radiantPlayerIDs, direPlayerIDs []int64,
	startedAt int64,
) int64 {
	t.Helper()
	ctx := context.Background()

	matchID, err := d.UpsertMatch(ctx, dotaMatchID)
	require.NoError(t, err)

	_, err = d.conn.Exec(`UPDATE matches SET started_at = ? WHERE id = ?`, startedAt, matchID)
	require.NoError(t, err)

	for _, pid := range radiantPlayerIDs {
		err = d.UpsertMatchPlayerStat(ctx, matchID, pid,
			"npc_dota_hero_axe", "radiant", 5, 2, 3, 450, 400, 100, 5, 20)
		require.NoError(t, err)
	}
	for _, pid := range direPlayerIDs {
		err = d.UpsertMatchPlayerStat(ctx, matchID, pid,
			"npc_dota_hero_invoker", "dire", 4, 3, 5, 380, 360, 90, 3, 18)
		require.NoError(t, err)
	}

	winTeam := "radiant"
	if direScore > radiantScore {
		winTeam = "dire"
	}
	err = d.CompleteMatch(ctx, matchID, radiantScore, direScore, winTeam, durationSecs)
	require.NoError(t, err)

	return matchID
}

// ── ListPlayers ───────────────────────────────────────────────────────────────

func TestListPlayers_WinsLosses(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	radID := insertPlayer(t, d, "Radiant Player", "tok-r")
	dirID := insertPlayer(t, d, "Dire Player", "tok-d")

	buildCompletedMatch(t, d, "match-001", 30, 20, 2000, []int64{radID}, []int64{dirID}, 100)

	entries, err := d.ListPlayers(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	byName := map[string]LeaderboardEntry{}
	for _, e := range entries {
		byName[e.DisplayName] = e
	}

	assert.Equal(t, 1, byName["Radiant Player"].Wins)
	assert.Equal(t, 0, byName["Radiant Player"].Losses)
	assert.Equal(t, 0, byName["Dire Player"].Wins)
	assert.Equal(t, 1, byName["Dire Player"].Losses)
}

func TestListPlayers_InProgressDoesNotCountAsWinOrLoss(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	pid := insertPlayer(t, d, "Player", "tok-p")
	matchID, err := d.UpsertMatch(ctx, "match-inprog")
	require.NoError(t, err)
	// Stats exist but match stays in_progress — CompleteMatch not called
	err = d.UpsertMatchPlayerStat(ctx, matchID, pid,
		"npc_dota_hero_axe", "radiant", 5, 2, 3, 450, 400, 100, 5, 20)
	require.NoError(t, err)

	entries, err := d.ListPlayers(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, 0, entries[0].Wins)
	assert.Equal(t, 0, entries[0].Losses)
	assert.Equal(t, 1, entries[0].MatchesPlayed)
}

// ── ListPlayerStreaks ─────────────────────────────────────────────────────────

func TestListPlayerStreaks_WinStreak(t *testing.T) {
	d := newTestDB(t)
	pid := insertPlayer(t, d, "Streak Player", "tok-s")

	// 3 wins in sequence (most recent = startedAt 300)
	buildCompletedMatch(t, d, "m1", 30, 10, 1800, []int64{pid}, nil, 100)
	buildCompletedMatch(t, d, "m2", 25, 15, 2000, []int64{pid}, nil, 200)
	buildCompletedMatch(t, d, "m3", 40, 5, 2200, []int64{pid}, nil, 300)

	streaks, err := d.ListPlayerStreaks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, streaks[pid])
}

func TestListPlayerStreaks_LossStreak(t *testing.T) {
	d := newTestDB(t)
	pid := insertPlayer(t, d, "Loser", "tok-l")

	// 2 losses (radiant_score < dire_score)
	buildCompletedMatch(t, d, "m1", 10, 30, 1800, []int64{pid}, nil, 100)
	buildCompletedMatch(t, d, "m2", 5, 40, 2000, []int64{pid}, nil, 200)

	streaks, err := d.ListPlayerStreaks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, -2, streaks[pid])
}

func TestListPlayerStreaks_Mixed(t *testing.T) {
	d := newTestDB(t)
	pid := insertPlayer(t, d, "Mixed", "tok-m")

	// Ordered by startedAt ASC: W(100), L(200), W(300), W(400)
	// DESC (most-recent first):  W(400), W(300), L(200), W(100) → streak = +2
	buildCompletedMatch(t, d, "m1", 30, 10, 1800, []int64{pid}, nil, 100) // W
	buildCompletedMatch(t, d, "m2", 5, 40, 2000, []int64{pid}, nil, 200)  // L
	buildCompletedMatch(t, d, "m3", 25, 15, 2200, []int64{pid}, nil, 300) // W
	buildCompletedMatch(t, d, "m4", 35, 20, 2400, []int64{pid}, nil, 400) // W

	streaks, err := d.ListPlayerStreaks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, streaks[pid])
}

// ── ListMatches ───────────────────────────────────────────────────────────────

func TestListMatches_PlayerNames(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	r1 := insertPlayer(t, d, "Alice", "tok-a")
	r2 := insertPlayer(t, d, "Bob", "tok-b")
	d1 := insertPlayer(t, d, "Carol", "tok-c")
	d2 := insertPlayer(t, d, "Dave", "tok-d")

	buildCompletedMatch(t, d, "match-names", 30, 20, 2000,
		[]int64{r1, r2}, []int64{d1, d2}, 100)

	matches, err := d.ListMatches(ctx)
	require.NoError(t, err)
	require.Len(t, matches, 1)

	sort.Strings(matches[0].RadiantPlayers)
	sort.Strings(matches[0].DirePlayers)
	assert.Equal(t, []string{"Alice", "Bob"}, matches[0].RadiantPlayers)
	assert.Equal(t, []string{"Carol", "Dave"}, matches[0].DirePlayers)
}

func TestListMatches_NoPlayers(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	_, err := d.UpsertMatch(ctx, "match-empty")
	require.NoError(t, err)

	matches, err := d.ListMatches(ctx)
	require.NoError(t, err)
	require.Len(t, matches, 1)

	// No player stats yet — player lists should be empty/nil, not a crash
	assert.Empty(t, matches[0].RadiantPlayers)
	assert.Empty(t, matches[0].DirePlayers)
}

// ── GetLeagueOverview ─────────────────────────────────────────────────────────

func TestGetLeagueOverview_Empty(t *testing.T) {
	d := newTestDB(t)

	ov, err := d.GetLeagueOverview(context.Background())
	require.NoError(t, err)
	require.NotNil(t, ov)

	assert.Equal(t, 0, ov.TotalMatches)
	assert.Equal(t, 0, ov.TotalKills)
	assert.Equal(t, float64(0), ov.AvgMatchDurationSecs)
}

func TestGetLeagueOverview_WithData(t *testing.T) {
	d := newTestDB(t)
	pid := insertPlayer(t, d, "Player", "tok-p")

	// Match 1: 30+20=50 kills, duration 2000
	buildCompletedMatch(t, d, "m1", 30, 20, 2000, []int64{pid}, nil, 100)
	// Match 2: 10+5=15 kills, duration 3000 (longest)
	buildCompletedMatch(t, d, "m2", 10, 5, 3000, []int64{pid}, nil, 200)

	ov, err := d.GetLeagueOverview(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, ov.TotalMatches)
	assert.Equal(t, 65, ov.TotalKills) // 50 + 15
	assert.Equal(t, 3000, ov.LongestMatchSecs)

	// Bloodiest match is m1 (50 kills > 15)
	// We verify it points to the match with 50 combined kills, not the longer one
	assert.Equal(t, 50, ov.BloodyMatch.Kills)
}

// ── HeroStats ────────────────────────────────────────────────────────────────

func TestHeroStats(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	r1 := insertPlayer(t, d, "P1", "tok-1")
	r2 := insertPlayer(t, d, "P2", "tok-2")

	// Match 1: both on radiant, radiant wins — hero gets a win
	m1 := buildCompletedMatch(t, d, "m1", 30, 10, 2000, []int64{r1, r2}, nil, 100)
	// Override hero for player 1 in match 1 to a specific hero
	_, err := d.conn.Exec(`UPDATE match_player_stats SET hero_name = 'npc_dota_hero_anti_mage'
		WHERE match_id = ? AND player_id = ?`, m1, r1)
	require.NoError(t, err)

	// Match 2: player 1 on radiant, radiant loses — hero gets a pick but no win
	m2 := buildCompletedMatch(t, d, "m2", 5, 40, 1800, []int64{r1}, nil, 200)
	_, err = d.conn.Exec(`UPDATE match_player_stats SET hero_name = 'npc_dota_hero_anti_mage'
		WHERE match_id = ? AND player_id = ?`, m2, r1)
	require.NoError(t, err)

	stats, err := d.HeroStats(ctx)
	require.NoError(t, err)

	heroMap := map[string]HeroStat{}
	for _, s := range stats {
		heroMap[s.HeroName] = s
	}

	am := heroMap["npc_dota_hero_anti_mage"]
	assert.Equal(t, 2, am.Picks)
	assert.Equal(t, 1, am.Wins)
}

// ── UpsertMatch ──────────────────────────────────────────────────────────────

func TestUpsertMatch_Idempotent(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	id1, err := d.UpsertMatch(ctx, "match-idem")
	require.NoError(t, err)

	id2, err := d.UpsertMatch(ctx, "match-idem")
	require.NoError(t, err)

	assert.Equal(t, id1, id2)

	var count int
	err = d.conn.QueryRow(`SELECT COUNT(*) FROM matches WHERE dota_match_id = 'match-idem'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// ── CompleteMatch ─────────────────────────────────────────────────────────────

func TestCompleteMatch_AlreadyCompleted(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	pid := insertPlayer(t, d, "P", "tok-p")
	matchID := buildCompletedMatch(t, d, "m1", 30, 20, 2000, []int64{pid}, nil, 100)

	// Try to complete again with different scores
	err := d.CompleteMatch(ctx, matchID, 50, 40, "radiant", 9999)
	require.NoError(t, err)

	// Scores must not have changed
	matches, err := d.ListMatches(ctx)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, 30, matches[0].RadiantScore)
	assert.Equal(t, 20, matches[0].DireScore)
}
