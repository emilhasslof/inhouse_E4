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
	d, err := Open(":memory:", "")
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

func TestFinalizeMatch_FillsMissingFromLiveStats(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	pWithPostGame := insertPlayer(t, d, "HasPostGame", "tok-a")
	pMissingPostGame := insertPlayer(t, d, "NoPostGame", "tok-b")

	matchID, err := d.UpsertMatch(ctx, "match-fill")
	require.NoError(t, err)

	// Both players have live stats during the match.
	require.NoError(t, d.UpsertLiveMatchStat(ctx, matchID, pWithPostGame,
		900, 10, 2, 8, 2500, 600, 550, 200, 4, 22, "npc_dota_hero_axe", "radiant"))
	require.NoError(t, d.UpsertLiveMatchStat(ctx, matchID, pMissingPostGame,
		900, 3, 6, 12, 1800, 400, 420, 90, 2, 19, "npc_dota_hero_lion", "dire"))

	// Only one of them sent a POST_GAME packet (different numbers so we can
	// verify POST_GAME wins over the live-stats fallback).
	require.NoError(t, d.UpsertMatchPlayerStat(ctx, matchID, pWithPostGame,
		"npc_dota_hero_axe", "radiant", 11, 2, 9, 610, 560, 205, 4, 23))

	require.NoError(t, d.CompleteMatch(ctx, matchID, 30, 25, "radiant", 1800))
	require.NoError(t, d.FinalizeMatch(ctx, "match-fill"))

	detail, err := d.GetMatchDetail(ctx, matchID)
	require.NoError(t, err)
	require.NotNil(t, detail)

	byName := map[string]PlayerStatRow{}
	for _, r := range detail.Radiant {
		byName[r.DisplayName] = r
	}
	for _, r := range detail.Dire {
		byName[r.DisplayName] = r
	}

	// POST_GAME row preserved (kills=11 from UpsertMatchPlayerStat, not 10 from live).
	assert.Equal(t, 11, byName["HasPostGame"].Kills)
	// Fallback row materialised from live_match_stats.
	assert.Equal(t, 3, byName["NoPostGame"].Kills)
	assert.Equal(t, "npc_dota_hero_lion", byName["NoPostGame"].HeroName)
	assert.Equal(t, "dire", byName["NoPostGame"].TeamName)
	assert.Equal(t, 19, byName["NoPostGame"].FinalLevel)
}

// ── ArchiveMatch ──────────────────────────────────────────────────────────────

func TestArchiveMatch_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	mainPath := dir + "/main.db"
	arcPath := dir + "/arc.db"

	d, err := Open(mainPath, arcPath)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	ctx := context.Background()
	pid := insertPlayer(t, d, "P1", "tok-1")
	matchID, err := d.UpsertMatch(ctx, "match-arc")
	require.NoError(t, err)

	require.NoError(t, d.InsertSnapshot(ctx, matchID, pid,
		60, 1, 0, 0, 500, 300, 200, 10, 0, 3, "npc_dota_hero_axe", "radiant"))
	require.NoError(t, d.UpsertLiveMatchStat(ctx, matchID, pid,
		120, 2, 1, 1, 800, 350, 220, 20, 1, 5, "npc_dota_hero_axe", "radiant"))
	require.NoError(t, d.UpsertMatchDraft(ctx, matchID, "radiant", true,
		[]DraftEntry{{Slot: 0, HeroID: 1, HeroName: "npc_dota_hero_axe"}}))

	require.NoError(t, d.ArchiveMatch(ctx, "match-arc"))

	// Main DB has no trace of the match.
	var mainCount int
	require.NoError(t, d.conn.QueryRow(
		`SELECT COUNT(*) FROM matches WHERE dota_match_id = ?`, "match-arc").Scan(&mainCount))
	assert.Equal(t, 0, mainCount, "match should be removed from main")

	// Archive DB has the parent and all child rows.
	for _, q := range []struct {
		name string
		sql  string
		want int
	}{
		{"matches", `SELECT COUNT(*) FROM arc.matches WHERE dota_match_id = 'match-arc'`, 1},
		{"snapshots", `SELECT COUNT(*) FROM arc.gsi_snapshots WHERE match_id = ?`, 1},
		{"live", `SELECT COUNT(*) FROM arc.live_match_stats WHERE match_id = ?`, 1},
		{"draft", `SELECT COUNT(*) FROM arc.match_draft WHERE match_id = ?`, 1},
	} {
		var got int
		var err error
		if q.name == "matches" {
			err = d.conn.QueryRow(q.sql).Scan(&got)
		} else {
			err = d.conn.QueryRow(q.sql, matchID).Scan(&got)
		}
		require.NoError(t, err, q.name)
		assert.Equal(t, q.want, got, "arc.%s row count", q.name)
	}
}

func TestFinalizeMatch_MissingMatchIsNoOp(t *testing.T) {
	d := newTestDB(t)
	require.NoError(t, d.FinalizeMatch(context.Background(), "does-not-exist"))
}

func TestFinalizeMatch_NoLiveRowsLeavesStatsUntouched(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	pid := insertPlayer(t, d, "P", "tok-p")
	matchID, err := d.UpsertMatch(ctx, "match-no-live")
	require.NoError(t, err)
	require.NoError(t, d.UpsertMatchPlayerStat(ctx, matchID, pid,
		"npc_dota_hero_axe", "radiant", 7, 2, 4, 500, 480, 150, 5, 21))
	require.NoError(t, d.CompleteMatch(ctx, matchID, 30, 25, "radiant", 1800))

	require.NoError(t, d.FinalizeMatch(ctx, "match-no-live"))

	detail, err := d.GetMatchDetail(ctx, matchID)
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.Len(t, detail.Radiant, 1)
	assert.Equal(t, 7, detail.Radiant[0].Kills, "POST_GAME row preserved")
}

func TestArchiveInProgressMatches_OnlyMovesInProgress(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir+"/main.db", dir+"/arc.db")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	ctx := context.Background()
	pid := insertPlayer(t, d, "P", "tok-p")

	// Two in-progress matches (no CompleteMatch call), each with one live row.
	for _, dotaID := range []string{"match-ip-1", "match-ip-2"} {
		mid, err := d.UpsertMatch(ctx, dotaID)
		require.NoError(t, err)
		require.NoError(t, d.UpsertLiveMatchStat(ctx, mid, pid,
			60, 1, 0, 0, 500, 300, 200, 10, 0, 3, "npc_dota_hero_axe", "radiant"))
	}
	// One completed match — must be left alone.
	completedID, err := d.UpsertMatch(ctx, "match-done")
	require.NoError(t, err)
	require.NoError(t, d.UpsertMatchPlayerStat(ctx, completedID, pid,
		"npc_dota_hero_axe", "radiant", 5, 2, 3, 450, 400, 100, 5, 20))
	require.NoError(t, d.CompleteMatch(ctx, completedID, 30, 20, "radiant", 1800))

	n, err := d.ArchiveInProgressMatches(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// Main DB now holds only the completed match.
	var mainCount int
	require.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM matches`).Scan(&mainCount))
	assert.Equal(t, 1, mainCount)
	var completedStillThere int
	require.NoError(t, d.conn.QueryRow(
		`SELECT COUNT(*) FROM matches WHERE dota_match_id = 'match-done'`).Scan(&completedStillThere))
	assert.Equal(t, 1, completedStillThere)

	// Archive DB has both in-progress matches and not the completed one.
	var arcCount int
	require.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM arc.matches`).Scan(&arcCount))
	assert.Equal(t, 2, arcCount)
	var arcCompleted int
	require.NoError(t, d.conn.QueryRow(
		`SELECT COUNT(*) FROM arc.matches WHERE dota_match_id = 'match-done'`).Scan(&arcCompleted))
	assert.Equal(t, 0, arcCompleted)
}

func TestArchiveMatch_MissingMatchErrors(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir+"/main.db", dir+"/arc.db")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	err = d.ArchiveMatch(context.Background(), "does-not-exist")
	require.Error(t, err)
}

// ── InsertOrphan ──────────────────────────────────────────────────────────────

func TestInsertOrphan_RoundTrip(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	require.NoError(t, d.InsertOrphan(ctx,
		"7890123", "76561199999999999", 142, "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS",
		"unregistered", `{"player":{"steamid":"76561199999999999"}}`))

	orphans, err := d.ListOrphans(ctx, 10)
	require.NoError(t, err)
	require.Len(t, orphans, 1)
	o := orphans[0]
	assert.Equal(t, "7890123", o.DotaMatchID)
	assert.Equal(t, "76561199999999999", o.SteamID)
	assert.Equal(t, 142, o.ClockTime)
	assert.Equal(t, "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS", o.GameState)
	assert.Equal(t, "unregistered", o.DropReason)
	assert.Contains(t, o.Payload, "76561199999999999")
}
