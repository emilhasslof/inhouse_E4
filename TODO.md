# TODO

Anyone (human or agent) can add items here at any time. No ceremony required.
Mark items done with `[x]` when complete, or remove them.

---

## Up next

- [ ] **Item builds** — capture final item builds per player from POST_GAME packets. GSI provides `items` block with `slot0`–`slot8` (inventory), `stash0`–`stash5`, `teleport0`, `neutral0`. Needs a new table, slot parsing, and a new API endpoint (e.g. `GET /api/matches/{id}/items`).

## Backlog

- [x] **Match confirmation threshold now controlled by `CONFIRM_THRESHOLD` env var** — defaults to 3. Set to `1` on staging for solo testing.

- [x] **Live match view** — `GET /api/matches/:id` now returns live stats (K/D/A, gold, GPM, XPM, hero, clock_time) from `live_match_stats` when `match.state == "in_progress"`. Frontend can check `state` and poll. Building status not included (fog-of-war limitation).
- [x] **Persistent Railway volume** — volume `inhouse_e4-volume` mounted at `/data`, `DB_PATH=/data/inhouse.db` set in Railway env. DB survives redeploys.
- [ ] **Gold/XP over time** — `gsi_snapshots` already has per-second gold and XPM for every player. Add `GET /api/matches/{id}/timeline` returning time-series data per player. Frontend can render it as a graph on the match detail page.
- [ ] **Match history per player** — `GET /api/players/{id}/matches` returning each match they played: hero, team, K/D/A, GPM, win/loss. Needed for a player profile page.
- [ ] **Kill timeline** — `player.kill_list` in GSI maps victim slot to kill count within the current streak. Kill events can be reconstructed by diffing consecutive snapshots in `gsi_snapshots`.
- [ ] **Nemesis streaks** — for every ordered player pair (A, B), track how many consecutive times A has killed B without B killing A back. Streaks accumulate across matches and reset when the victim gets a kill back. Expose the current top streaks at `GET /api/stats/nemesis`. Requires kill event detection from snapshot deltas and a new `player_pair_killstreak` table storing current streak and all-time peak per pair.

## Done

- [x] Schema migration added for `win_team` column — `ALTER TABLE` runs on startup so existing DBs are upgraded without needing a full wipe.
- [x] Win/loss determination now uses `win_team` from GSI POST_GAME packets instead of kill score comparison.
- [x] Lobby cheats disabled; `POST /api/lobby/create` accepts `game_mode: "captains_mode" | "all_pick"` (default: captains_mode).
- [x] Match gate confirmation threshold set to 1 player (solo testing). Raise `confirmThreshold` in `internal/match/gate.go` before going live.
- [x] Register scripts use Steam persona name automatically — no manual name entry, fixes Å/Ä/Ö encoding issues.
- [x] Bot GC reconnect hardening — `DisconnectedEvent` now calls `connectWithRetry` (full retry loop) instead of a one-shot `Connect()`. `gcReady`/`gcAbort` channels are reset on each `LoggedOnEvent` so reconnects get a fresh GC session. `lobbyMu` is now held for the entire lobby lifetime (creation + `!start` wait) to prevent concurrent `LeaveCreateLobby` calls from multiple frontend POSTs. `!start` now also accepted via Steam direct message (independent of GC session state).
- [x] Draft tracking — `match_draft` table + `GET /api/matches/{id}/draft`. Populated from HERO_SELECTION GSI packets (Captain's Mode only — All Pick has no draft block). team3=radiant, team2=dire per Dota 2 internal numbering. Fixed bug where draft was only written on POST_GAME (block is empty by then).
- [x] Bot hard reset endpoint — `POST /api/lobby/reset` tears down the Steam connection and reconnects from scratch. Implemented via `bot.Manager` which owns the `Service` lifecycle.
- [x] Lobby invites now use Steam IDs instead of display names — `POST /api/lobby/create` accepts `{ steam_ids: string[] }` and returns 400 with the unmatched IDs if any are not registered.
- [x] Steam friend chat message sent to each player when a lobby is created, including the lobby password.
- [x] Match gating — GSI ingest only accepts packets when a bot lobby is active. Bot kicks itself from its team slot after lobby creation (retaining host status), then listens for `!start` in lobby chat. On `!start`, the gate opens and the lobby launches. Gate closes automatically on POST_GAME. Dev mode pre-opens the gate so datagen works without a bot.
- [x] Confirmed that `allplayers` is never present in regular player GSI feeds — Valve intentionally restricts it to observers. All 10 GSI clients are required for complete match stats.
- [x] Bot auto-accepts incoming Steam friend requests — `register.bat` opens the bot profile so players can add it in one click.
- [x] Scaffold Go server with SQLite and GSI ingest pipeline.
- [x] Dev datagen tool for testing the pipeline without a real match.
- [x] Rewritten to a backend-only JSON API (removed the HTMX frontend).
- [x] Deployed to Railway — live at https://inhousee4-production.up.railway.app
- [x] 23-test suite covering the db, gsi, and web layers.
- [x] Dev seed data (3 fake matches) so the frontend can develop against real API responses.
- [x] Player onboarding — `register.bat` reads the Steam ID automatically, calls `POST /api/register`, and writes the GSI config file.
- [x] `GET /api/registered-players` endpoint.
- [x] Steam bot integration — `POST /api/lobby/create` creates a lobby and sends invites.
- [x] Recovered Steam bot credentials — `inhouse_e6` account with TOTP via steamguard-cli.
