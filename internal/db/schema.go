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
  win_team        TEXT    NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS match_draft (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  match_id  INTEGER NOT NULL REFERENCES matches(id),
  team_name TEXT    NOT NULL,   -- 'radiant' or 'dire'
  is_pick   INTEGER NOT NULL,   -- 1 = pick, 0 = ban
  slot      INTEGER NOT NULL,   -- 0-based within (team, is_pick); preserves pick/ban order
  hero_id   INTEGER NOT NULL,
  hero_name TEXT    NOT NULL,
  UNIQUE(match_id, team_name, is_pick, slot)
);

CREATE INDEX IF NOT EXISTS idx_snapshots_match_player ON gsi_snapshots(match_id, player_id);
CREATE INDEX IF NOT EXISTS idx_stats_match ON match_player_stats(match_id);
CREATE INDEX IF NOT EXISTS idx_stats_player ON match_player_stats(player_id);
CREATE INDEX IF NOT EXISTS idx_draft_match ON match_draft(match_id);
`

// seedSQL inserts dev-only fake players used by the datagen tool.
// Tokens match the hardcoded tokens in cmd/datagen/main.go.
const seedSQL = `
INSERT OR IGNORE INTO players (steam_id, display_name, token) VALUES
  ('datagen-steam-r1', 'Spinelli',       'datagen-radiant-1'),
  ('datagen-steam-r2', 'Sku',            'datagen-radiant-2'),
  ('datagen-steam-r3', 'Jockwe Lamotte', 'datagen-radiant-3'),
  ('datagen-steam-r4', 'Ottosama',       'datagen-radiant-4'),
  ('76561197990491029', 'HACKERMAN',      'datagen-radiant-5'),
  ('datagen-steam-d1', 'Maddashåååtaaa', 'datagen-dire-1'),
  ('datagen-steam-d2', 'Harvey Specter', 'datagen-dire-2'),
  ('datagen-steam-d3', 'Deer',           'datagen-dire-3'),
  ('datagen-steam-d4', 'Jointzart',      'datagen-dire-4'),
  ('datagen-steam-d5', 'Lacko',          'datagen-dire-5');
`

// devMatchSQL inserts three completed fake matches with full player stats.
// Uses token-based subqueries so it doesn't depend on auto-assigned player IDs.
// IDs prefixed with 'dev-' never collide with real Dota match IDs (10-digit numbers).
// All statements use INSERT OR IGNORE so re-running Seed() is safe.
const devMatchSQL = `
INSERT OR IGNORE INTO matches (dota_match_id, state, radiant_score, dire_score, duration_secs, started_at) VALUES
  ('dev-match-001', 'completed', 32, 28, 2340, unixepoch('now', '-2 hours')),
  ('dev-match-002', 'completed', 18, 41, 1980, unixepoch('now', '-4 hours')),
  ('dev-match-003', 'completed', 45, 22, 2780, unixepoch('now', '-1 day'));

-- Match 1: Spinelli's team (radiant) wins 32-28
INSERT OR IGNORE INTO match_player_stats (match_id, player_id, hero_name, team_name, kills, deaths, assists, gpm, xpm, last_hits, denies, final_level) VALUES
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-001'), (SELECT id FROM players WHERE token='datagen-radiant-1'), 'npc_dota_hero_anti_mage',       'radiant', 12,3, 8, 720,650,380,12,25),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-001'), (SELECT id FROM players WHERE token='datagen-radiant-2'), 'npc_dota_hero_invoker',           'radiant',  9,5,14, 580,610,210, 8,23),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-001'), (SELECT id FROM players WHERE token='datagen-radiant-3'), 'npc_dota_hero_storm_spirit',      'radiant',  7,6,11, 490,520,190, 5,21),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-001'), (SELECT id FROM players WHERE token='datagen-radiant-4'), 'npc_dota_hero_spectre',           'radiant',  3,7,18, 410,380,150, 3,18),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-001'), (SELECT id FROM players WHERE token='datagen-radiant-5'), 'npc_dota_hero_chen',              'radiant',  1,7,22, 310,340, 60,15,16),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-001'), (SELECT id FROM players WHERE token='datagen-dire-1'),    'npc_dota_hero_phantom_assassin',  'dire',     10,5, 9, 640,590,340,10,24),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-001'), (SELECT id FROM players WHERE token='datagen-dire-2'),    'npc_dota_hero_axe',               'dire',      6,7,15, 420,460,120, 6,20),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-001'), (SELECT id FROM players WHERE token='datagen-dire-3'),    'npc_dota_hero_earth_spirit',      'dire',      4,8,18, 350,390, 45, 2,17),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-001'), (SELECT id FROM players WHERE token='datagen-dire-4'),    'npc_dota_hero_ember_spirit',      'dire',      5,6,12, 510,530,220, 7,22),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-001'), (SELECT id FROM players WHERE token='datagen-dire-5'),    'npc_dota_hero_shadow_shaman',     'dire',      3,6,16, 290,310, 55, 4,16);

-- Match 2: Maddas's team (dire) wins 41-18
INSERT OR IGNORE INTO match_player_stats (match_id, player_id, hero_name, team_name, kills, deaths, assists, gpm, xpm, last_hits, denies, final_level) VALUES
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-002'), (SELECT id FROM players WHERE token='datagen-radiant-1'), 'npc_dota_hero_morphling',         'radiant',  5,8, 6, 480,440,280, 8,20),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-002'), (SELECT id FROM players WHERE token='datagen-radiant-2'), 'npc_dota_hero_tinker',            'radiant',  6,9, 5, 420,410,200, 4,19),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-002'), (SELECT id FROM players WHERE token='datagen-radiant-3'), 'npc_dota_hero_puck',              'radiant',  4,8, 7, 380,390,160, 3,18),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-002'), (SELECT id FROM players WHERE token='datagen-radiant-4'), 'npc_dota_hero_juggernaut',        'radiant',  2,8, 9, 350,330,190, 5,17),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-002'), (SELECT id FROM players WHERE token='datagen-radiant-5'), 'npc_dota_hero_dazzle',            'radiant',  1,8,11, 240,270, 40, 8,14),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-002'), (SELECT id FROM players WHERE token='datagen-dire-1'),    'npc_dota_hero_terrorblade',       'dire',     14,3,10, 780,710,420,14,25),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-002'), (SELECT id FROM players WHERE token='datagen-dire-2'),    'npc_dota_hero_mars',              'dire',      8,4,18, 480,500,130, 8,22),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-002'), (SELECT id FROM players WHERE token='datagen-dire-3'),    'npc_dota_hero_tusk',              'dire',      6,5,22, 370,400, 50, 3,19),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-002'), (SELECT id FROM players WHERE token='datagen-dire-4'),    'npc_dota_hero_void_spirit',       'dire',      9,3,14, 560,580,240, 6,23),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-002'), (SELECT id FROM players WHERE token='datagen-dire-5'),    'npc_dota_hero_oracle',            'dire',      4,3,20, 310,340, 35, 5,17);

-- Match 3: teams swapped — Maddas's group plays radiant, wins 45-22
INSERT OR IGNORE INTO match_player_stats (match_id, player_id, hero_name, team_name, kills, deaths, assists, gpm, xpm, last_hits, denies, final_level) VALUES
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-003'), (SELECT id FROM players WHERE token='datagen-dire-1'),    'npc_dota_hero_phantom_assassin',  'radiant', 16,2,11, 810,740,450,16,25),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-003'), (SELECT id FROM players WHERE token='datagen-dire-2'),    'npc_dota_hero_mars',              'radiant',  9,4,20, 510,540,140, 9,23),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-003'), (SELECT id FROM players WHERE token='datagen-dire-3'),    'npc_dota_hero_tusk',              'radiant',  7,5,25, 390,420, 55, 4,20),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-003'), (SELECT id FROM players WHERE token='datagen-dire-4'),    'npc_dota_hero_void_spirit',       'radiant', 10,3,16, 590,610,260, 7,24),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-003'), (SELECT id FROM players WHERE token='datagen-dire-5'),    'npc_dota_hero_oracle',            'radiant',  3,4,22, 330,360, 40, 6,18),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-003'), (SELECT id FROM players WHERE token='datagen-radiant-1'), 'npc_dota_hero_anti_mage',         'dire',      8,7, 5, 550,510,300,10,22),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-003'), (SELECT id FROM players WHERE token='datagen-radiant-2'), 'npc_dota_hero_invoker',           'dire',      7,8, 8, 490,470,180, 6,21),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-003'), (SELECT id FROM players WHERE token='datagen-radiant-3'), 'npc_dota_hero_storm_spirit',      'dire',      5,9, 9, 420,440,160, 4,19),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-003'), (SELECT id FROM players WHERE token='datagen-radiant-4'), 'npc_dota_hero_spectre',           'dire',      2,9,15, 370,340,130, 3,17),
  ((SELECT id FROM matches WHERE dota_match_id='dev-match-003'), (SELECT id FROM players WHERE token='datagen-radiant-5'), 'npc_dota_hero_chen',              'dire',      0,9,19, 270,300, 50,12,15);
`
