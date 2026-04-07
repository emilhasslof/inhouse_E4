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

// MatchSummary is the view model for the match list page.
type MatchSummary struct {
	ID           int64
	DotaMatchID  string
	State        string
	RadiantScore int
	DireScore    int
	DurationSecs int
	StartedAt    int64
}

// PlayerStatRow is one row in the match scoreboard.
type PlayerStatRow struct {
	DisplayName string
	HeroName    string
	TeamName    string
	Kills       int
	Deaths      int
	Assists     int
	GPM         int
	XPM         int
	LastHits    int
	Denies      int
	FinalLevel  int
}

// MatchDetailView is the full data for the match scoreboard page.
type MatchDetailView struct {
	Match   MatchSummary
	Radiant []PlayerStatRow
	Dire    []PlayerStatRow
}

// LeaderboardEntry is one row in the player leaderboard.
type LeaderboardEntry struct {
	ID           int64
	DisplayName  string
	MatchesPlayed int
	TotalKills   int
	TotalDeaths  int
	TotalAssists int
	AvgGPM       float64
}
