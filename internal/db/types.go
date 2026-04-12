package db

// Player is a registered inhouse league player.
type Player struct {
	ID          int64
	SteamID     string
	DisplayName string
	Token       string
}

// Match represents a single Dota 2 match recorded via GSI.
type Match struct {
	ID           int64
	DotaMatchID  string
	State        string // "in_progress" | "completed"
	RadiantScore int
	DireScore    int
	DurationSecs int
	StartedAt    int64 // unix epoch
	EndedAt      int64 // unix epoch, 0 if not ended
}

// MatchPlayerStat holds materialized end-of-match stats for one player.
type MatchPlayerStat struct {
	ID         int64
	MatchID    int64
	PlayerID   int64
	HeroName   string
	TeamName   string
	Kills      int
	Deaths     int
	Assists    int
	GPM        int
	XPM        int
	LastHits   int
	Denies     int
	FinalLevel int
}

// MatchSummary is the view model for the match list / detail pages.
type MatchSummary struct {
	ID             int64    `json:"id"`
	DotaMatchID    string   `json:"dota_match_id"`
	State          string   `json:"state"`
	WinTeam        string   `json:"win_team"`
	RadiantScore   int      `json:"radiant_score"`
	DireScore      int      `json:"dire_score"`
	DurationSecs   int      `json:"duration_secs"`
	StartedAt      int64    `json:"started_at"`
	RadiantPlayers []string `json:"radiant_players,omitempty"`
	DirePlayers    []string `json:"dire_players,omitempty"`
}

// PlayerStatRow is one row in the match scoreboard.
// Gold and ClockTime are only present for in-progress matches (omitted for completed ones).
type PlayerStatRow struct {
	DisplayName string `json:"display_name"`
	HeroName    string `json:"hero_name"`
	TeamName    string `json:"team_name"`
	Kills       int    `json:"kills"`
	Deaths      int    `json:"deaths"`
	Assists     int    `json:"assists"`
	GPM         int    `json:"gpm"`
	XPM         int    `json:"xpm"`
	LastHits    int    `json:"last_hits"`
	Denies      int    `json:"denies"`
	FinalLevel  int    `json:"final_level"`
	Gold        int    `json:"gold,omitempty"`
	ClockTime   int    `json:"clock_time,omitempty"`
}

// MatchDetailView is the full data for the match scoreboard page.
type MatchDetailView struct {
	Match   MatchSummary    `json:"match"`
	Radiant []PlayerStatRow `json:"radiant"`
	Dire    []PlayerStatRow `json:"dire"`
}

// LeaderboardEntry is one row in the player leaderboard.
type LeaderboardEntry struct {
	ID            int64   `json:"id"`
	DisplayName   string  `json:"display_name"`
	MatchesPlayed int     `json:"matches_played"`
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	TotalKills    int     `json:"total_kills"`
	TotalDeaths   int     `json:"total_deaths"`
	TotalAssists  int     `json:"total_assists"`
	AvgGPM        float64 `json:"avg_gpm"`
	Streak        int     `json:"streak"`
}

// HeroStat holds aggregated pick/win data for a single hero.
// Bans are not tracked so that field is always 0.
type HeroStat struct {
	HeroName string `json:"hero_name"`
	Picks    int    `json:"picks"`
	Wins     int    `json:"wins"`
	Bans     int    `json:"bans"`
}

// KillsRef is a (match_id, kills) pair used in LeagueOverview.
type KillsRef struct {
	MatchID int `json:"match_id"`
	Kills   int `json:"kills"`
}

// KDARef is a (name, kda) pair used in LeagueOverview.
type KDARef struct {
	Name string  `json:"name"`
	KDA  float64 `json:"kda"`
}

// DraftEntry is one pick or ban slot in a draft.
type DraftEntry struct {
	Slot     int    `json:"slot"`
	HeroID   int    `json:"hero_id"`
	HeroName string `json:"hero_name"`
}

// DraftTeamView holds the ordered picks and bans for one team.
type DraftTeamView struct {
	Picks []DraftEntry `json:"picks"`
	Bans  []DraftEntry `json:"bans"`
}

// MatchDraftView is the full draft for a match.
type MatchDraftView struct {
	Radiant DraftTeamView `json:"radiant"`
	Dire    DraftTeamView `json:"dire"`
}

// LeagueOverview contains aggregate stats across all completed matches.
type LeagueOverview struct {
	TotalMatches         int     `json:"total_matches"`
	TotalKills           int     `json:"total_kills"`
	AvgMatchDurationSecs float64 `json:"avg_match_duration_secs"`
	LongestMatchSecs     int     `json:"longest_match_secs"`
	ShortestMatchSecs    int     `json:"shortest_match_secs"`
	MostKillsInMatch     KillsRef `json:"most_kills_in_match"`
	HighestKDAPlayer     KDARef   `json:"highest_kda_player"`
	BloodyMatch          KillsRef `json:"bloodiest_match"`
}
