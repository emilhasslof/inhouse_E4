# TODO

Anyone (human or agent) can add items here at any time. No ceremony required.
Mark items done with `[x]` when complete, or remove them.

---

## Up next


## Backlog

- [ ] **Live match view** — frontend needs to be able to poll an in-progress match. Likely a dedicated endpoint (e.g. `GET /api/match/live`) that returns current state: scoreline, player K/D/A/gold, hero names, and building state (towers/barracks standing per team — data comes from own-team building visibility in GSI, noting enemy buildings are not visible so we'd need to infer from both teams' feeds combined). Different UX from finished match pages — polling interval, live scoreboard, no duration yet. Consider what the minimum viable payload looks like before designing the DB query.
- [ ] Add persistent Railway volume before real matches start (DB currently resets on redeploy)
- [ ] Remove `APP_ENV=development` from Railway once real players are registered
- [ ] Gold-over-time graph on the match detail page (data is already in `gsi_snapshots`)
- [ ] Kill timeline — `player.kill_list` maps victim slot → kill count per streak. Could reconstruct a kill event log from `gsi_snapshots`.
- [ ] **Nemesis streaks** — track, for every ordered player pair (A, B), how many consecutive times A has killed B without B ever killing A back. Streaks accumulate across matches. When B kills A, A's streak on B resets to 0 and B's streak on A starts. Expose the current top streaks via `GET /api/stats/nemesis`. Requires: (1) kill event detection from `gsi_snapshot` deltas (`kill_list` changes between ticks), (2) new `player_pair_killstreak` table storing current streak count + all-time peak per pair, updated at match end (or live during ingest).

## Done

- [x] Draft tracking — `match_draft` table + `GET /api/matches/{id}/draft`. Populated from POST_GAME GSI packets (Captain's Mode only — All Pick has no draft block). team3=radiant, team2=dire per Dota 2 internal numbering.
- [x] `POST /api/lobby/reset` — hard-resets the bot (abandon lobby, cancel waiters, kill Steam connection, reconnect fresh). Implemented via `bot.Manager` which owns the `Service` lifecycle.

- [x] Match gating — GSI ingest only accepts packets when a bot lobby is active. Bot kicks itself from its team slot after lobby creation (retaining host status in unassigned pool), then listens for `!start` in lobby chat. On `!start`, gate opens and bot calls `LaunchLobby()`. Gate closes automatically on POST_GAME. Dev mode pre-opens the gate so datagen works without a bot.
- [x] `allplayers` investigation — confirmed **never present** for regular players, only for observers. Valve intentionally gates it. All 10 GSI clients are required for complete stats unless we add an observer bot.
- [x] Bot auto-accepts incoming Steam friend requests — register.bat opens bot profile so players can add it in one click
- [x] Prove GSI data can be received and parsed locally (`gsi/main.go`)
- [x] Scaffold Go server with SQLite and GSI ingest pipeline
- [x] Dev datagen tool for testing the pipeline without a real match
- [x] Rewritten to backend-only JSON API (removed HTMX frontend)
- [x] Deploy to Railway — live at https://inhousee4-production.up.railway.app
- [x] 23-test suite covering db/gsi/web layers
- [x] Dev seed data (3 fake matches) so frontend can develop against real API responses
- [x] Player onboarding — `register.bat` reads Steam ID automatically, calls `POST /api/register`, writes GSI config
- [x] Registered players endpoint — `GET /api/registered-players`
- [x] Bot integration — `POST /api/lobby/create` creates lobby and sends invites
- [x] Recover Steam bot credentials — `inhouse_e6` account with TOTP via steamguard-cli
