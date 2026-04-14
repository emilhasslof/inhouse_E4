package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// RegisterPlayer inserts a new player. Returns an error if the steam_id is already registered.
func (db *DB) RegisterPlayer(ctx context.Context, steamID, displayName string) (*Player, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(b)
	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO players (steam_id, display_name, token) VALUES (?, ?, ?)`,
		steamID, displayName, token)
	if err != nil {
		return nil, fmt.Errorf("register player: %w", err)
	}
	return &Player{SteamID: steamID, DisplayName: displayName}, nil
}

// PlayersBySteamIDs returns all registered players whose steam_id is in the
// provided list. The caller can detect unmatched IDs by comparing the returned
// slice length (or the SteamID values) against the input list.
func (db *DB) PlayersBySteamIDs(ctx context.Context, steamIDs []string) ([]Player, error) {
	if len(steamIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(steamIDs))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma
	args := make([]any, len(steamIDs))
	for i, id := range steamIDs {
		args[i] = id
	}
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, steam_id, display_name FROM players WHERE steam_id IN (`+placeholders+`)`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var players []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.SteamID, &p.DisplayName); err != nil {
			return nil, err
		}
		players = append(players, p)
	}
	return players, rows.Err()
}

// UpsertMatchDraft writes picks and bans for one team. Uses INSERT OR IGNORE so
// it is safe to call from every POST_GAME packet — only the first call per
// (match, team, slot) is stored, which is fine because the draft never changes.
func (db *DB) UpsertMatchDraft(ctx context.Context, matchID int64, teamName string, isPick bool, entries []DraftEntry) error {
	isPickInt := 0
	if isPick {
		isPickInt = 1
	}
	for _, e := range entries {
		_, err := db.conn.ExecContext(ctx,
			`INSERT OR IGNORE INTO match_draft (match_id, team_name, is_pick, slot, hero_id, hero_name)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			matchID, teamName, isPickInt, e.Slot, e.HeroID, e.HeroName)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetMatchDraft returns all picks and bans for a match, grouped by team.
// Returns nil if no draft data exists for the match.
func (db *DB) GetMatchDraft(ctx context.Context, matchID int64) (*MatchDraftView, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT team_name, is_pick, slot, hero_id, hero_name
		 FROM match_draft WHERE match_id = ?
		 ORDER BY team_name, is_pick DESC, slot`,
		matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	view := &MatchDraftView{
		Radiant: DraftTeamView{Picks: []DraftEntry{}, Bans: []DraftEntry{}},
		Dire:    DraftTeamView{Picks: []DraftEntry{}, Bans: []DraftEntry{}},
	}
	found := false
	for rows.Next() {
		found = true
		var teamName string
		var isPick, slot, heroID int
		var heroName string
		if err := rows.Scan(&teamName, &isPick, &slot, &heroID, &heroName); err != nil {
			return nil, err
		}
		entry := DraftEntry{Slot: slot, HeroID: heroID, HeroName: heroName}
		team := &view.Radiant
		if teamName == "dire" {
			team = &view.Dire
		}
		if isPick == 1 {
			team.Picks = append(team.Picks, entry)
		} else {
			team.Bans = append(team.Bans, entry)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return view, nil
}

// ListRegisteredPlayers returns all players with their steam IDs and display names.
func (db *DB) ListRegisteredPlayers(ctx context.Context) ([]Player, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, steam_id, display_name FROM players ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var players []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.SteamID, &p.DisplayName); err != nil {
			return nil, err
		}
		players = append(players, p)
	}
	return players, rows.Err()
}

// PlayerBySteamID returns the player with the given Steam ID.
// Returns an error if not found.
func (db *DB) PlayerBySteamID(ctx context.Context, steamID string) (*Player, error) {
	row := db.conn.QueryRowContext(ctx,
		`SELECT id, steam_id, display_name FROM players WHERE steam_id = ?`, steamID)
	p := &Player{}
	err := row.Scan(&p.ID, &p.SteamID, &p.DisplayName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("unknown steam_id")
	}
	return p, err
}

// UpsertMatch creates a match row for the given Dota match ID if one does not
// already exist, then returns the match's internal database ID.
func (db *DB) UpsertMatch(ctx context.Context, dotaMatchID string) (int64, error) {
	_, err := db.conn.ExecContext(ctx,
		`INSERT OR IGNORE INTO matches (dota_match_id) VALUES (?)`, dotaMatchID)
	if err != nil {
		return 0, fmt.Errorf("upsert match: %w", err)
	}
	var id int64
	err = db.conn.QueryRowContext(ctx,
		`SELECT id FROM matches WHERE dota_match_id = ?`, dotaMatchID).Scan(&id)
	return id, err
}

// InsertSnapshot writes one GSI snapshot row. Duplicate clock_time values for
// the same match+player are silently ignored via the UNIQUE constraint.
func (db *DB) InsertSnapshot(ctx context.Context, matchID, playerID int64,
	clockTime, kills, deaths, assists, gold, gpm, xpm, lastHits, denies, heroLevel int,
	heroName, teamName string,
) error {
	_, err := db.conn.ExecContext(ctx, `
		INSERT OR IGNORE INTO gsi_snapshots
		  (match_id, player_id, clock_time, kills, deaths, assists, gold, gpm, xpm,
		   last_hits, denies, hero_name, hero_level, team_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		matchID, playerID, clockTime, kills, deaths, assists, gold, gpm, xpm,
		lastHits, denies, heroName, heroLevel, teamName)
	return err
}

// UpsertMatchPlayerStat writes (or overwrites) the end-of-match stats for one
// player. Called when game_state == DOTA_GAMERULES_STATE_POST_GAME.
func (db *DB) UpsertMatchPlayerStat(ctx context.Context, matchID, playerID int64,
	heroName, teamName string,
	kills, deaths, assists, gpm, xpm, lastHits, denies, finalLevel int,
) error {
	_, err := db.conn.ExecContext(ctx, `
		INSERT INTO match_player_stats
		  (match_id, player_id, hero_name, team_name, kills, deaths, assists,
		   gpm, xpm, last_hits, denies, final_level)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(match_id, player_id) DO UPDATE SET
		  hero_name   = excluded.hero_name,
		  team_name   = excluded.team_name,
		  kills       = excluded.kills,
		  deaths      = excluded.deaths,
		  assists     = excluded.assists,
		  gpm         = excluded.gpm,
		  xpm         = excluded.xpm,
		  last_hits   = excluded.last_hits,
		  denies      = excluded.denies,
		  final_level = excluded.final_level`,
		matchID, playerID, heroName, teamName,
		kills, deaths, assists, gpm, xpm, lastHits, denies, finalLevel)
	return err
}

// UpsertLiveMatchStat writes or updates the live stats for one player in an
// in-progress match. Only the latest snapshot per (match, player) is kept.
func (db *DB) UpsertLiveMatchStat(ctx context.Context, matchID, playerID int64,
	clockTime, kills, deaths, assists, gold, gpm, xpm, lastHits, denies, heroLevel int,
	heroName, teamName string,
) error {
	_, err := db.conn.ExecContext(ctx, `
		INSERT INTO live_match_stats
		  (match_id, player_id, clock_time, kills, deaths, assists, gold, gpm, xpm,
		   last_hits, denies, hero_name, hero_level, team_name, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch())
		ON CONFLICT(match_id, player_id) DO UPDATE SET
		  clock_time = excluded.clock_time,
		  kills      = excluded.kills,
		  deaths     = excluded.deaths,
		  assists    = excluded.assists,
		  gold       = excluded.gold,
		  gpm        = excluded.gpm,
		  xpm        = excluded.xpm,
		  last_hits  = excluded.last_hits,
		  denies     = excluded.denies,
		  hero_name  = excluded.hero_name,
		  hero_level = excluded.hero_level,
		  team_name  = excluded.team_name,
		  updated_at = excluded.updated_at`,
		matchID, playerID, clockTime, kills, deaths, assists, gold, gpm, xpm,
		lastHits, denies, heroName, heroLevel, teamName)
	return err
}

// CompleteMatch marks a match as completed, records final scores, and clears live stats.
func (db *DB) CompleteMatch(ctx context.Context, matchID int64, radiantScore, direScore int, winTeam string, durationSecs int) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE matches
		SET state = 'completed', radiant_score = ?, dire_score = ?,
		    win_team = ?, duration_secs = ?, ended_at = unixepoch()
		WHERE id = ? AND state != 'completed'`,
		radiantScore, direScore, winTeam, durationSecs, matchID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM live_match_stats WHERE match_id = ?`, matchID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteMatch removes a match and all its associated rows (snapshots, player
// stats, draft data) atomically. A no-op if the match does not exist.
// Used by the gate's abandon callback to clean up incomplete matches.
func (db *DB) DeleteMatch(ctx context.Context, dotaMatchID string) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM matches WHERE dota_match_id = ?`, dotaMatchID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // already gone
	}
	if err != nil {
		return fmt.Errorf("lookup match %s: %w", dotaMatchID, err)
	}

	for _, stmt := range []string{
		`DELETE FROM match_draft         WHERE match_id = ?`,
		`DELETE FROM match_player_stats  WHERE match_id = ?`,
		`DELETE FROM live_match_stats    WHERE match_id = ?`,
		`DELETE FROM gsi_snapshots       WHERE match_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return fmt.Errorf("delete child rows for match %d: %w", id, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM matches WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete match %d: %w", id, err)
	}
	return tx.Commit()
}

// DeleteInProgressMatches removes every match that is still in state
// 'in_progress', along with all associated rows. Returns the number of matches
// deleted. Called at startup to discard matches that were never completed
// because the server restarted while a game was running.
func (db *DB) DeleteInProgressMatches(ctx context.Context) (int, error) {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM matches WHERE state = 'in_progress'`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count in-progress matches: %w", err)
	}
	if count == 0 {
		return 0, nil
	}

	for _, stmt := range []string{
		`DELETE FROM match_draft        WHERE match_id IN (SELECT id FROM matches WHERE state = 'in_progress')`,
		`DELETE FROM match_player_stats WHERE match_id IN (SELECT id FROM matches WHERE state = 'in_progress')`,
		`DELETE FROM live_match_stats   WHERE match_id IN (SELECT id FROM matches WHERE state = 'in_progress')`,
		`DELETE FROM gsi_snapshots      WHERE match_id IN (SELECT id FROM matches WHERE state = 'in_progress')`,
		`DELETE FROM matches            WHERE state = 'in_progress'`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return 0, fmt.Errorf("cleanup in-progress matches: %w", err)
		}
	}

	return count, tx.Commit()
}

// ListMatches returns all matches ordered by most recently started, including
// comma-separated player name lists per team from match_player_stats.
func (db *DB) ListMatches(ctx context.Context) ([]MatchSummary, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT m.id, m.dota_match_id, m.state, m.win_team, m.radiant_score, m.dire_score, m.duration_secs, m.started_at,
		       GROUP_CONCAT(CASE WHEN mps.team_name = 'radiant' THEN p.display_name END) AS radiant_players,
		       GROUP_CONCAT(CASE WHEN mps.team_name = 'dire'    THEN p.display_name END) AS dire_players
		FROM matches m
		LEFT JOIN match_player_stats mps ON mps.match_id = m.id
		LEFT JOIN players p ON p.id = mps.player_id
		GROUP BY m.id
		ORDER BY m.started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []MatchSummary
	for rows.Next() {
		var m MatchSummary
		var radiant, dire sql.NullString
		if err := rows.Scan(
			&m.ID, &m.DotaMatchID, &m.State, &m.WinTeam, &m.RadiantScore, &m.DireScore, &m.DurationSecs, &m.StartedAt,
			&radiant, &dire,
		); err != nil {
			return nil, err
		}
		if radiant.Valid && radiant.String != "" {
			m.RadiantPlayers = strings.Split(radiant.String, ",")
		}
		if dire.Valid && dire.String != "" {
			m.DirePlayers = strings.Split(dire.String, ",")
		}
		matches = append(matches, m)
	}
	return matches, rows.Err()
}

// GetMatchDetail returns the match summary plus all player stats for a match.
func (db *DB) GetMatchDetail(ctx context.Context, matchID int64) (*MatchDetailView, error) {
	var m MatchSummary
	err := db.conn.QueryRowContext(ctx, `
		SELECT id, dota_match_id, state, win_team, radiant_score, dire_score, duration_secs, started_at
		FROM matches WHERE id = ?`, matchID).
		Scan(&m.ID, &m.DotaMatchID, &m.State, &m.WinTeam, &m.RadiantScore, &m.DireScore, &m.DurationSecs, &m.StartedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	view := &MatchDetailView{Match: m}

	if m.State == "in_progress" {
		rows, err := db.conn.QueryContext(ctx, `
			SELECT p.display_name, lms.hero_name, lms.team_name,
			       lms.kills, lms.deaths, lms.assists,
			       lms.gpm, lms.xpm, lms.last_hits, lms.denies, lms.hero_level,
			       lms.gold, lms.clock_time
			FROM live_match_stats lms
			JOIN players p ON p.id = lms.player_id
			WHERE lms.match_id = ?
			ORDER BY lms.team_name, lms.kills DESC`, matchID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var row PlayerStatRow
			if err := rows.Scan(
				&row.DisplayName, &row.HeroName, &row.TeamName,
				&row.Kills, &row.Deaths, &row.Assists,
				&row.GPM, &row.XPM, &row.LastHits, &row.Denies, &row.FinalLevel,
				&row.Gold, &row.ClockTime,
			); err != nil {
				return nil, err
			}
			if row.TeamName == "radiant" {
				view.Radiant = append(view.Radiant, row)
			} else {
				view.Dire = append(view.Dire, row)
			}
		}
		return view, rows.Err()
	}

	// Completed match — read from materialised end-of-game stats.
	rows, err := db.conn.QueryContext(ctx, `
		SELECT p.display_name, mps.hero_name, mps.team_name,
		       mps.kills, mps.deaths, mps.assists,
		       mps.gpm, mps.xpm, mps.last_hits, mps.denies, mps.final_level
		FROM match_player_stats mps
		JOIN players p ON p.id = mps.player_id
		WHERE mps.match_id = ?
		ORDER BY mps.team_name, mps.kills DESC`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row PlayerStatRow
		if err := rows.Scan(
			&row.DisplayName, &row.HeroName, &row.TeamName,
			&row.Kills, &row.Deaths, &row.Assists,
			&row.GPM, &row.XPM, &row.LastHits, &row.Denies, &row.FinalLevel,
		); err != nil {
			return nil, err
		}
		if row.TeamName == "radiant" {
			view.Radiant = append(view.Radiant, row)
		} else {
			view.Dire = append(view.Dire, row)
		}
	}
	return view, rows.Err()
}

// ListPlayers returns all players with aggregated career stats including wins and losses.
// Streak is computed in Go from a separate ordered query; call ListPlayerStreaks to get it.
func (db *DB) ListPlayers(ctx context.Context) ([]LeaderboardEntry, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT p.id, p.display_name,
		       COUNT(DISTINCT mps.match_id)  AS matches_played,
		       COALESCE(SUM(CASE WHEN m.state = 'completed' AND
		           mps.team_name = m.win_team
		         THEN 1 ELSE 0 END), 0)      AS wins,
		       COALESCE(SUM(CASE WHEN m.state = 'completed' AND m.win_team != '' AND
		           mps.team_name != m.win_team
		         THEN 1 ELSE 0 END), 0)      AS losses,
		       COALESCE(SUM(mps.kills), 0)   AS total_kills,
		       COALESCE(SUM(mps.deaths), 0)  AS total_deaths,
		       COALESCE(SUM(mps.assists), 0) AS total_assists,
		       COALESCE(AVG(mps.gpm), 0)     AS avg_gpm
		FROM players p
		LEFT JOIN match_player_stats mps ON mps.player_id = p.id
		LEFT JOIN matches m ON m.id = mps.match_id
		GROUP BY p.id
		ORDER BY avg_gpm DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LeaderboardEntry
	for rows.Next() {
		var e LeaderboardEntry
		if err := rows.Scan(&e.ID, &e.DisplayName, &e.MatchesPlayed,
			&e.Wins, &e.Losses,
			&e.TotalKills, &e.TotalDeaths, &e.TotalAssists, &e.AvgGPM); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ListPlayerStreaks returns a map of player_id → streak value.
// Positive = win streak, negative = loss streak (0 = no completed matches).
func (db *DB) ListPlayerStreaks(ctx context.Context) (map[int64]int, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT mps.player_id,
		       CASE WHEN mps.team_name = m.win_team THEN 1 ELSE 0 END AS won
		FROM match_player_stats mps
		JOIN matches m ON m.id = mps.match_id
		WHERE m.state = 'completed' AND m.win_team != ''
		ORDER BY mps.player_id, m.started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Accumulate ordered results per player, then compute streaks.
	type result struct{ won bool }
	playerResults := map[int64][]bool{}
	for rows.Next() {
		var playerID int64
		var won int
		if err := rows.Scan(&playerID, &won); err != nil {
			return nil, err
		}
		playerResults[playerID] = append(playerResults[playerID], won == 1)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	streaks := make(map[int64]int, len(playerResults))
	for playerID, results := range playerResults {
		if len(results) == 0 {
			continue
		}
		first := results[0]
		count := 0
		for _, r := range results {
			if r == first {
				count++
			} else {
				break
			}
		}
		if first {
			streaks[playerID] = count
		} else {
			streaks[playerID] = -count
		}
	}
	return streaks, nil
}

// HeroStats returns aggregated pick and win counts per hero across completed matches.
func (db *DB) HeroStats(ctx context.Context) ([]HeroStat, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT mps.hero_name,
		       COUNT(*) AS picks,
		       SUM(CASE WHEN mps.team_name = m.win_team THEN 1 ELSE 0 END) AS wins
		FROM match_player_stats mps
		JOIN matches m ON m.id = mps.match_id
		WHERE m.state = 'completed'
		GROUP BY mps.hero_name
		ORDER BY picks DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []HeroStat
	for rows.Next() {
		var s HeroStat
		if err := rows.Scan(&s.HeroName, &s.Picks, &s.Wins); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// GetLeagueOverview returns aggregate stats across all completed matches.
func (db *DB) GetLeagueOverview(ctx context.Context) (*LeagueOverview, error) {
	ov := &LeagueOverview{}

	// Main aggregates.
	err := db.conn.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(radiant_score + dire_score), 0),
		       COALESCE(AVG(duration_secs), 0),
		       COALESCE(MAX(duration_secs), 0),
		       COALESCE(MIN(CASE WHEN duration_secs > 0 THEN duration_secs END), 0)
		FROM matches WHERE state = 'completed'`).
		Scan(&ov.TotalMatches, &ov.TotalKills, &ov.AvgMatchDurationSecs,
			&ov.LongestMatchSecs, &ov.ShortestMatchSecs)
	if err != nil {
		return nil, fmt.Errorf("league overview aggregates: %w", err)
	}

	// Bloodiest match (most combined kills = highest radiant+dire score).
	_ = db.conn.QueryRowContext(ctx, `
		SELECT id, (radiant_score + dire_score)
		FROM matches WHERE state = 'completed'
		ORDER BY (radiant_score + dire_score) DESC LIMIT 1`).
		Scan(&ov.BloodyMatch.MatchID, &ov.BloodyMatch.Kills)
	ov.MostKillsInMatch = ov.BloodyMatch // same concept

	// Highest KDA player (kills + assists*0.5) / max(deaths, 1).
	_ = db.conn.QueryRowContext(ctx, `
		SELECT p.display_name,
		       (CAST(SUM(mps.kills) AS REAL) + CAST(SUM(mps.assists) AS REAL) * 0.5)
		       / MAX(SUM(mps.deaths), 1) AS kda
		FROM match_player_stats mps
		JOIN players p ON p.id = mps.player_id
		JOIN matches m ON m.id = mps.match_id
		WHERE m.state = 'completed'
		GROUP BY mps.player_id
		ORDER BY kda DESC LIMIT 1`).
		Scan(&ov.HighestKDAPlayer.Name, &ov.HighestKDAPlayer.KDA)

	return ov, nil
}
