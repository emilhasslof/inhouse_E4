-- Recover match 8778443197 from GSI POST_GAME spectator packet captured by
-- watching the replay (packets/postgame/000079_DOTA_GAMERULES_STATE_POST_GAME.json).
-- This match was auto-deleted from the production DB by the old startup cleanup.
--
-- Kill sums verified against map.radiant_score/dire_score (47 / 37). Duration is
-- map.clock_time at POST_GAME = 3538s. Winner is map.win_team = "radiant".
--
-- Run (once) against the production DB:
--   railway run sqlite3 /data/inhouse.db < scripts/recover_match_8778443197.sql
--
-- If any of the 10 steam_ids isn't registered in the production `players`
-- table, the corresponding row's player_id subquery returns NULL and the
-- INSERT will fail the NOT NULL constraint — at which point the transaction
-- rolls back cleanly and nothing is committed.

BEGIN TRANSACTION;

-- 1) Match row. Adjust started_at/ended_at to the real wall-clock time if you
--    care about ordering in the match list — the packet only carries in-game
--    clock_time, not a real timestamp. Leaving started_at ~1 day ago as a
--    placeholder; override as needed.
INSERT INTO matches
  (dota_match_id, state, win_team, radiant_score, dire_score, duration_secs, started_at, ended_at)
VALUES
  ('8778443197', 'completed', 'radiant', 47, 37, 3538,
   unixepoch('now', '-1 day'),
   unixepoch('now', '-1 day') + 3538);

-- 2) Per-player end-of-match stats (10 rows).
INSERT INTO match_player_stats
  (match_id, player_id, hero_name, team_name, kills, deaths, assists, gpm, xpm, last_hits, denies, final_level)
VALUES
  -- Radiant
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'),
   (SELECT id FROM players WHERE steam_id='76561198013559780'),
   'npc_dota_hero_treant',             'radiant',  0, 14, 24, 306,  596,  70,  0, 25),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'),
   (SELECT id FROM players WHERE steam_id='76561197988477204'),
   'npc_dota_hero_zuus',               'radiant',  8,  9, 34, 394,  745, 116,  3, 27),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'),
   (SELECT id FROM players WHERE steam_id='76561197990491029'),
   'npc_dota_hero_juggernaut',         'radiant', 17,  5, 14, 799, 1092, 678, 21, 30),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'),
   (SELECT id FROM players WHERE steam_id='76561198060209627'),
   'npc_dota_hero_abyssal_underlord',  'radiant',  7,  6, 28, 506,  794, 342,  6, 27),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'),
   (SELECT id FROM players WHERE steam_id='76561197996059799'),
   'npc_dota_hero_lina',               'radiant', 15,  5, 18, 636, 1029, 481,  9, 29),
  -- Dire
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'),
   (SELECT id FROM players WHERE steam_id='76561198091381493'),
   'npc_dota_hero_storm_spirit',       'dire',    12,  9, 16, 615,  945, 501,  4, 28),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'),
   (SELECT id FROM players WHERE steam_id='76561198052817545'),
   'npc_dota_hero_shredder',           'dire',    13,  6, 17, 662,  959, 491,  5, 29),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'),
   (SELECT id FROM players WHERE steam_id='76561197970785234'),
   'npc_dota_hero_venomancer',         'dire',     2, 14, 20, 357,  578, 167,  3, 24),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'),
   (SELECT id FROM players WHERE steam_id='76561197963289811'),
   'npc_dota_hero_monkey_king',        'dire',     9,  7, 11, 608,  829, 528,  7, 27),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'),
   (SELECT id FROM players WHERE steam_id='76561198007300249'),
   'npc_dota_hero_rattletrap',         'dire',     1, 11, 19, 344,  603, 118,  3, 25);

-- 3) Captain's Mode draft (5 picks + 7 bans per team).
--    team2 = radiant, team3 = dire (verified via player.teamN.playerM.team_name in the packet).
INSERT INTO match_draft
  (match_id, team_name, is_pick, slot, hero_id, hero_name)
VALUES
  -- Radiant picks
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'radiant', 1, 0,  22, 'npc_dota_hero_zuus'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'radiant', 1, 1,  83, 'npc_dota_hero_treant'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'radiant', 1, 2, 108, 'npc_dota_hero_abyssal_underlord'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'radiant', 1, 3,  25, 'npc_dota_hero_lina'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'radiant', 1, 4,   8, 'npc_dota_hero_juggernaut'),
  -- Radiant bans
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'radiant', 0, 0,  53, 'npc_dota_hero_furion'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'radiant', 0, 1, 123, 'npc_dota_hero_hoodwink'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'radiant', 0, 2,  71, 'npc_dota_hero_spirit_breaker'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'radiant', 0, 3,  80, 'npc_dota_hero_lone_druid'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'radiant', 0, 4,  60, 'npc_dota_hero_night_stalker'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'radiant', 0, 5,  29, 'npc_dota_hero_tidehunter'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'radiant', 0, 6, 135, 'npc_dota_hero_dawnbreaker'),
  -- Dire picks
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'dire',    1, 0,  51, 'npc_dota_hero_rattletrap'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'dire',    1, 1,  40, 'npc_dota_hero_venomancer'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'dire',    1, 2, 114, 'npc_dota_hero_monkey_king'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'dire',    1, 3,  17, 'npc_dota_hero_storm_spirit'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'dire',    1, 4,  98, 'npc_dota_hero_shredder'),
  -- Dire bans
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'dire',    0, 0,  33, 'npc_dota_hero_enigma'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'dire',    0, 1, 120, 'npc_dota_hero_pangolier'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'dire',    0, 2, 129, 'npc_dota_hero_mars'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'dire',    0, 3,  86, 'npc_dota_hero_rubick'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'dire',    0, 4, 110, 'npc_dota_hero_phoenix'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'dire',    0, 5,  54, 'npc_dota_hero_life_stealer'),
  ((SELECT id FROM matches WHERE dota_match_id='8778443197'), 'dire',    0, 6,  18, 'npc_dota_hero_sven');

COMMIT;

-- Sanity checks (run these separately after COMMIT to verify):
-- SELECT id, dota_match_id, state, win_team, radiant_score, dire_score, duration_secs FROM matches WHERE dota_match_id='8778443197';
-- SELECT p.display_name, mps.hero_name, mps.team_name, mps.kills, mps.deaths, mps.assists, mps.gpm FROM match_player_stats mps JOIN players p ON p.id=mps.player_id JOIN matches m ON m.id=mps.match_id WHERE m.dota_match_id='8778443197' ORDER BY mps.team_name, mps.kills DESC;
-- SELECT team_name, is_pick, slot, hero_name FROM match_draft WHERE match_id=(SELECT id FROM matches WHERE dota_match_id='8778443197') ORDER BY team_name, is_pick DESC, slot;
