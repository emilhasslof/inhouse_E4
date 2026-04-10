# Dota 2 GSI Payload Schema

Captured from single-player practice lobbies, 2026-04-10.
Config requested: `map`, `player`, `hero`, `abilities`, `items`, `draft`, `allplayers`, `buildings`, `minimap`, `wearables`.

Two capture sessions:
- `samples/0*` — All Pick lobby (used for all blocks except `draft`)
- `samples/03_hero_selection_cm.json` — Captain's Mode lobby (draft block populated)

---

## Game state sequence

```
(menu)
  → WAIT_FOR_PLAYERS_TO_LOAD   match ID appears, loading screen
  → HERO_SELECTION              hero picking
  → STRATEGY_TIME               strategy / banning phase
  → PRE_GAME                    countdown before creeps spawn (clock runs negative)
  → GAME_IN_PROGRESS            clock ≥ 0, game live
  → POST_GAME                   ancient destroyed, scoreboard
```

---

## Structural schemas

There are three distinct field schemas across the game lifecycle. The schema
changes at phase transitions, not between individual packets.

### Schema A — `WAIT_FOR_PLAYERS_TO_LOAD` + `HERO_SELECTION`

Hero block contains only `id: 0` (no hero selected yet).
Abilities, items, minimap, wearables, and buildings are all present but empty.

### Schema B — `STRATEGY_TIME` + early `PRE_GAME` (before hero spawns)

Hero block gains `facet`, `id`, `name` once a hero is locked in.
Minimap and wearables become populated. Buildings remain empty until spawn.

### Schema C — `PRE_GAME` (after spawn) + `GAME_IN_PROGRESS` + `POST_GAME`

Hero block gains the full set of live fields (position, health, mana, level, xp,
status flags, talents, aghs). Buildings populate with all towers and barracks.
This schema is fully stable for the remainder of the game — field set does not
change between in-game and post-game, only values differ.

---

## Top-level blocks

| Block        | Menu | A | B | C | Notes |
|--------------|------|---|---|---|-------|
| `map`        | —    | ✓ | ✓ | ✓ | Absent in menu |
| `player`     | `{}` | ✓ | ✓ | ✓ | Empty object in menu |
| `hero`       | —    | ✓ | ✓ | ✓ | Varies per schema (see below) |
| `abilities`  | —    | ✓ | ✓ | ✓ | Empty until hero spawns |
| `items`      | —    | ✓ | ✓ | ✓ | Empty until hero spawns |
| `buildings`  | —    | ✓ | ✓ | ✓ | Empty until hero spawns (Schema C) |
| `minimap`    | —    | ✓ | ✓ | ✓ | Empty in Schema A, populated from B |
| `wearables`  | —    | ✓ | ✓ | ✓ | Empty in Schema A, populated from B |
| `draft`      | `{}` | ✓ | ✓ | ✓ | Empty in All Pick. Populated in Captain's Mode from `WAIT_FOR_PLAYERS_TO_LOAD` onwards |
| `previously` | —    | ✓ | ✓ | ✓ | Delta: previous values of changed fields |
| `added`      | —    | ✓ | ✓ | ✓ | Delta: keys that appeared for the first time |
| `auth`       | ✓    | ✓ | ✓ | ✓ | Always present |
| `allplayers` | —    | — | — | — | **Never present** for a regular player (observer-only) |

---

## Block field reference

### `map`

| Field                  | Type    | Notes |
|------------------------|---------|-------|
| `name`                 | string  | Always `"start"` on the standard map |
| `matchid`              | string  | Dota match ID. Empty string in menu |
| `game_state`           | string  | See game state sequence above |
| `game_time`            | int     | Seconds since match was loaded (monotonic) |
| `clock_time`           | int     | In-game clock. Negative during PRE_GAME countdown, 0 at creep spawn |
| `daytime`              | bool    | True during day cycle |
| `nightstalker_night`   | bool    | True during Night Stalker ultimate night |
| `radiant_score`        | int     | Radiant kills |
| `dire_score`           | int     | Dire kills |
| `paused`               | bool    | |
| `win_team`             | string  | `"none"` during game, `"radiant"` or `"dire"` in POST_GAME |
| `customgamename`       | string  | **Always empty string** even in practice lobbies |
| `ward_purchase_cooldown` | int   | Seconds until next free ward |

**Absent fields:** `lobby_type`, `game_mode`, `matchmaking_mode` — there is no
signal in the map block to distinguish a practice lobby from a ranked game.

---

### `player`

| Field                  | Type    | Notes |
|------------------------|---------|-------|
| `steamid`              | string  | 64-bit Steam ID |
| `accountid`            | string  | 32-bit Steam account ID |
| `name`                 | string  | Steam display name |
| `activity`             | string  | `"playing"` during a match |
| `team_name`            | string  | `"radiant"` or `"dire"` |
| `player_slot`          | int     | 0–9 across both teams |
| `team_slot`            | int     | 0–4 within team |
| `kills`                | int     | |
| `deaths`               | int     | |
| `assists`              | int     | |
| `last_hits`            | int     | |
| `denies`               | int     | |
| `kill_streak`          | int     | Current kill streak |
| `kill_list`            | object  | Map of victim slot → kill count this streak |
| `commands_issued`      | int     | Total commands sent to the server |
| `gold`                 | int     | Total gold (reliable + unreliable) |
| `gold_reliable`        | int     | Reliable gold (from kills, objectives) |
| `gold_unreliable`      | int     | Unreliable gold (from income, creeps) |
| `gold_from_hero_kills` | int     | Cumulative gold from hero kills |
| `gold_from_creep_kills`| int     | Cumulative gold from creep kills |
| `gold_from_income`     | int     | Cumulative gold from passive income |
| `gold_from_shared`     | int     | Cumulative gold from shared kill bounty |
| `gpm`                  | int     | Gold per minute. **Unreliable for first ~5 seconds** of game (wildly inflated, normalises quickly) |
| `xpm`                  | int     | Experience per minute. Same caveat as gpm |

---

### `hero` — Schema A (no hero picked)

| Field | Type | Notes |
|-------|------|-------|
| `id`  | int  | `0` |

---

### `hero` — Schema B (hero locked in, not yet spawned)

| Field    | Type   | Notes |
|----------|--------|-------|
| `id`     | int    | Hero internal ID |
| `name`   | string | e.g. `"npc_dota_hero_leshrac"` |
| `facet`  | int    | Selected hero facet index |

---

### `hero` — Schema C (hero spawned, in-game and post-game)

All Schema B fields, plus:

| Field               | Type    | Notes |
|---------------------|---------|-------|
| `xpos`              | int     | World X coordinate |
| `ypos`              | int     | World Y coordinate |
| `level`             | int     | 1–30 |
| `xp`                | int     | Total experience points |
| `alive`             | bool    | False while dead |
| `respawn_seconds`   | int     | Seconds until respawn (0 if alive) |
| `buyback_cost`      | int     | Current buyback gold cost |
| `buyback_cooldown`  | int     | Seconds until buyback available |
| `health`            | int     | Current HP |
| `max_health`        | int     | Max HP |
| `health_percent`    | int     | 0–100 |
| `mana`              | int     | Current mana |
| `max_mana`          | int     | Max mana |
| `mana_percent`      | int     | 0–100 |
| `silenced`          | bool    | |
| `stunned`           | bool    | |
| `disarmed`          | bool    | |
| `magicimmune`       | bool    | |
| `hexed`             | bool    | |
| `muted`             | bool    | |
| `break`             | bool    | Passive abilities broken |
| `smoked`            | bool    | |
| `has_debuff`        | bool    | |
| `aghanims_scepter`  | bool    | |
| `aghanims_shard`    | bool    | |
| `permanent_buffs`   | object  | Map of active permanent buff names → stack count |
| `talent_1`–`talent_8` | bool | Whether each talent is selected (1=bottom-left, 8=top-right) |
| `attributes_level`  | int     | Attribute points spent |

---

### `abilities`

Keyed `ability0`, `ability1`, … (including talents once selected). Dota+ abilities (`plus_high_five`, `plus_guild_banner`) also appear here.

Each entry:

| Field            | Type   | Notes |
|------------------|--------|-------|
| `name`           | string | Internal ability name |
| `level`          | int    | Current skill level (0 = not skilled) |
| `can_cast`       | bool   | |
| `passive`        | bool   | |
| `ability_active` | bool   | False if ability is disabled |
| `cooldown`       | int    | Remaining cooldown seconds |
| `max_cooldown`   | int    | |
| `ultimate`       | bool   | |

---

### `items`

Keyed by slot name. Inventory slots: `slot0`–`slot8`. Stash: `stash0`–`stash5`.
Special slots: `teleport0`, `neutral0`, `neutral1`, `preserved_neutral6`–`preserved_neutral10`.

Empty slots: `{"name": "empty"}`.
Occupied slots (active item):

| Field          | Type   | Notes |
|----------------|--------|-------|
| `name`         | string | Internal item name e.g. `"item_blink"` |
| `purchaser`    | int    | `team_slot` of the player who bought it |
| `item_level`   | int    | |
| `passive`      | bool   | |
| `can_cast`     | bool   | Only on active items |
| `cooldown`     | int    | Only on active items |
| `max_cooldown` | int    | Only on active items |
| `item_charges` | int    | Only on charged items |
| `charges`      | int    | Only on charged items |

---

### `buildings`

Only the reporting player's team is visible (fog of war applies — enemy buildings are absent).
Keyed by building name: `dota_goodguys_tower1_top`, `good_rax_melee_mid`, etc.
Empty `{}` until Schema C (hero spawns).

Each entry:

| Field        | Type | Notes |
|--------------|------|-------|
| `health`     | int  | Current HP |
| `max_health` | int  | |

---

### `minimap`

Keyed `o0`, `o1`, … — entities visible to the player on the minimap.
Content varies by game state and what is in vision range.

Each entry:

| Field         | Type   | Notes |
|---------------|--------|-------|
| `xpos`        | int    | World X |
| `ypos`        | int    | World Y |
| `image`       | string | Minimap icon name |
| `team`        | int    | 2 = Radiant, 3 = Dire |
| `yaw`         | int    | Facing direction |
| `unitname`    | string | Internal unit name |
| `visionrange` | int    | Vision radius of this unit |

---

### `draft` — Captain's Mode only

Present from `WAIT_FOR_PLAYERS_TO_LOAD` onwards in CM. All slots initialise to
`id: 0, class: ""` and fill in as picks and bans are made. Empty `{}` in All Pick.

Top-level fields:

| Field                    | Type   | Notes |
|--------------------------|--------|-------|
| `activeteam`             | int    | Team ID currently acting: `2` = Radiant, `3` = Dire, `0` = not active (between picks/bans or draft finished) |
| `pick`                   | bool   | `true` = active team is picking, `false` = active team is banning |
| `activeteam_time_remaining` | int | Seconds remaining for the current pick or ban |
| `radiant_bonus_time`     | int    | Radiant's remaining bonus time pool (seconds) |
| `dire_bonus_time`        | int    | Dire's remaining bonus time pool (seconds) |
| `team2`                  | object | Radiant draft data (Dota internal team ID 2) |
| `team3`                  | object | Dire draft data (Dota internal team ID 3) |

Each team object (`team2` / `team3`):

| Field          | Type   | Notes |
|----------------|--------|-------|
| `home_team`    | bool   | `true` for the team that created the lobby |
| `pick0_class`–`pick4_class` | string | Short hero class name e.g. `"leshrac"` (not the full `npc_dota_hero_` prefix). Empty string until picked. |
| `pick0_id`–`pick4_id`     | int    | Hero internal ID. `0` until picked. |
| `ban0_class`–`ban6_class` | string | Same format as picks. 7 bans per team in CM. |
| `ban0_id`–`ban6_id`      | int    | Hero internal ID. `0` until banned. |

---

### `wearables`

Cosmetic item IDs for the reporting player's hero. Keyed `wearable0`, `wearable1`, …
(non-consecutive — some slots unused).
Values are integer item definition indexes. Irrelevant for stats tracking.

---

### `previously` and `added`

Delta blocks present on most packets. Not useful for stats processing but helpful for
optimising change detection if needed in future.

- `previously`: contains the prior value of any field that changed this tick
- `added`: contains `true` for every key that appeared for the first time this tick

---

## Key findings and known absences

| Question | Answer |
|---|---|
| Is `lobby_type` present? | No. The map block has no lobby type, game mode, or matchmaking signal. |
| Is `allplayers` available? | Only for observers with full vision. Regular players never receive it. Valve intentionally gate it to prevent stat-scraping exploits. |
| Is draft data available? | Yes, but only in Captain's Mode. The `draft` block is `{}` in All Pick. In CM it is populated from the first packet, with all slots starting at `id: 0` and filling in as the draft progresses. Contains full picks (5 per team) and bans (7 per team). |
| Are enemy buildings visible? | No. Only the reporting player's team buildings appear. |
| Can GPM/XPM be trusted immediately? | No. Values are wildly inflated for the first ~5 seconds (`clock_time` ≈ 0). Use after `clock_time > 10` or so. |
| Does the schema change mid-game? | No. Once Schema C is established (hero spawns in PRE_GAME), the field set is stable for the rest of the match. |
