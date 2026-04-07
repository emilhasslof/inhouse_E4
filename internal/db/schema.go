package db

const schemaSQL = `
CREATE TABLE IF NOT EXISTS players (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  steam_id     TEXT    NOT NULL UNIQUE,
  display_name TEXT    NOT NULL,
  token        TEXT    NOT NULL UNIQUE,
  created_at   INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS matches (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  dota_match_id   TEXT    NOT NULL UNIQUE,
  state           TEXT    NOT NULL DEFAULT 'in_progress',
  radiant_score   INTEGER NOT NULL DEFAULT 0,
  dire_score      INTEGER NOT NULL DEFAULT 0,
  duration_secs   INTEGER NOT NULL DEFAULT 0,
  started_at      INTEGER NOT NULL DEFAULT (unixepoch()),
  ended_at        INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS gsi_snapshots (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  match_id    INTEGER NOT NULL REFERENCES matches(id),
  player_id   INTEGER NOT NULL REFERENCES players(id),
  clock_time  INTEGER NOT NULL,
  kills       INTEGER NOT NULL DEFAULT 0,
  deaths      INTEGER NOT NULL DEFAULT 0,
  assists     INTEGER NOT NULL DEFAULT 0,
  gold        INTEGER NOT NULL DEFAULT 0,
  gpm         INTEGER NOT NULL DEFAULT 0,
  xpm         INTEGER NOT NULL DEFAULT 0,
  last_hits   INTEGER NOT NULL DEFAULT 0,
  denies      INTEGER NOT NULL DEFAULT 0,
  hero_name   TEXT    NOT NULL DEFAULT '',
  hero_level  INTEGER NOT NULL DEFAULT 1,
  team_name   TEXT    NOT NULL DEFAULT '',
  recorded_at INTEGER NOT NULL DEFAULT (unixepoch()),
  UNIQUE(match_id, player_id, clock_time)
);

CREATE TABLE IF NOT EXISTS match_player_stats (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  match_id    INTEGER NOT NULL REFERENCES matches(id),
  player_id   INTEGER NOT NULL REFERENCES players(id),
  hero_name   TEXT    NOT NULL DEFAULT '',
  team_name   TEXT    NOT NULL DEFAULT '',
  kills       INTEGER NOT NULL DEFAULT 0,
  deaths      INTEGER NOT NULL DEFAULT 0,
  assists     INTEGER NOT NULL DEFAULT 0,
  gpm         INTEGER NOT NULL DEFAULT 0,
  xpm         INTEGER NOT NULL DEFAULT 0,
  last_hits   INTEGER NOT NULL DEFAULT 0,
  denies      INTEGER NOT NULL DEFAULT 0,
  final_level INTEGER NOT NULL DEFAULT 1,
  UNIQUE(match_id, player_id)
);

CREATE INDEX IF NOT EXISTS idx_snapshots_match_player ON gsi_snapshots(match_id, player_id);
CREATE INDEX IF NOT EXISTS idx_stats_match ON match_player_stats(match_id);
CREATE INDEX IF NOT EXISTS idx_stats_player ON match_player_stats(player_id);
`

// seedSQL inserts dev-only fake players used by the datagen tool.
// Tokens match the hardcoded tokens in cmd/datagen/main.go.
const seedSQL = `
INSERT OR IGNORE INTO players (steam_id, display_name, token) VALUES
  ('datagen-steam-r1', 'Arteezy',   'datagen-radiant-1'),
  ('datagen-steam-r2', 'Miracle',   'datagen-radiant-2'),
  ('datagen-steam-r3', 'w33',       'datagen-radiant-3'),
  ('datagen-steam-r4', 'Ana',       'datagen-radiant-4'),
  ('datagen-steam-r5', 'Puppey',    'datagen-radiant-5'),
  ('datagen-steam-d1', 'N0tail',    'datagen-dire-1'),
  ('datagen-steam-d2', 'Ceb',       'datagen-dire-2'),
  ('datagen-steam-d3', 'Jerax',     'datagen-dire-3'),
  ('datagen-steam-d4', 'Topson',    'datagen-dire-4'),
  ('datagen-steam-d5', 'Sumail',    'datagen-dire-5');
`
