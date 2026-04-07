package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PlayerByToken returns the player with the given GSI auth token.
// Returns an error if not found.
func (db *DB) PlayerByToken(ctx context.Context, token string) (*Player, error) {
	row := db.conn.QueryRowContext(ctx,
		`SELECT id, steam_id, display_name, token FROM players WHERE token = ?`, token)
	p := &Player{}
	err := row.Scan(&p.ID, &p.SteamID, &p.DisplayName, &p.Token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("unknown token")
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

// CompleteMatch marks a match as completed and records final scores and duration.
func (db *DB) CompleteMatch(ctx context.Context, matchID int64, radiantScore, direScore, durationSecs int) error {
	_, err := db.conn.ExecContext(ctx, `
		UPDATE matches
		SET state = 'completed', radiant_score = ?, dire_score = ?,
		    duration_secs = ?, ended_at = unixepoch()
		WHERE id = ? AND state != 'completed'`,
		radiantScore, direScore, durationSecs, matchID)
	return err
}

// ListMatches returns all matches ordered by most recently started.
func (db *DB) ListMatches(ctx context.Context) ([]MatchSummary, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT id, dota_match_id, state, radiant_score, dire_score, duration_secs, started_at
		FROM matches
		ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []MatchSummary
	for rows.Next() {
		var m MatchSummary
		if err := rows.Scan(&m.ID, &m.DotaMatchID, &m.State, &m.RadiantScore, &m.DireScore, &m.DurationSecs, &m.StartedAt); err != nil {
			return nil, err
		}
		matches = append(matches, m)
	}
	return matches, rows.Err()
}

// GetMatchDetail returns the match summary plus all player stats for a match.
func (db *DB) GetMatchDetail(ctx context.Context, matchID int64) (*MatchDetailView, error) {
	var m MatchSummary
	err := db.conn.QueryRowContext(ctx, `
		SELECT id, dota_match_id, state, radiant_score, dire_score, duration_secs, started_at
		FROM matches WHERE id = ?`, matchID).
		Scan(&m.ID, &m.DotaMatchID, &m.State, &m.RadiantScore, &m.DireScore, &m.DurationSecs, &m.StartedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

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

	view := &MatchDetailView{Match: m}
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

// ListPlayers returns all players with aggregated career stats.
func (db *DB) ListPlayers(ctx context.Context) ([]LeaderboardEntry, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT p.id, p.display_name,
		       COUNT(DISTINCT mps.match_id)  AS matches_played,
		       COALESCE(SUM(mps.kills), 0)   AS total_kills,
		       COALESCE(SUM(mps.deaths), 0)  AS total_deaths,
		       COALESCE(SUM(mps.assists), 0) AS total_assists,
		       COALESCE(AVG(mps.gpm), 0)     AS avg_gpm
		FROM players p
		LEFT JOIN match_player_stats mps ON mps.player_id = p.id
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
			&e.TotalKills, &e.TotalDeaths, &e.TotalAssists, &e.AvgGPM); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
