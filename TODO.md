# TODO

Anyone (human or agent) can add items here at any time. No ceremony required.
Mark items done with `[x]` when complete, or remove them.

---

## Up next

## Backlog

- [ ] **Live match view** — add a polling endpoint (e.g. `GET /api/match/live`) returning current state for an in-progress match: scoreline, player K/D/A/gold, hero names, and building status per team. Building visibility is limited to own-team buildings in GSI, so enemy tower/barracks state would need to be inferred by combining feeds from both teams. This is a different UX from finished match pages — needs a polling interval, live scoreboard, and no final duration yet. Design the minimum viable payload before touching the DB.
- [ ] **Add a persistent Railway volume** before real matches begin — the database currently lives in ephemeral container storage and resets on every redeploy.
- [ ] **Gold-over-time graph** on the match detail page — the data is already in `gsi_snapshots`, just needs a query and a frontend chart.
- [ ] **Kill timeline** — `player.kill_list` in GSI maps victim slot to kill count within the current streak. Kill events can be reconstructed by diffing consecutive snapshots in `gsi_snapshots`.
- [ ] **Nemesis streaks** — for every ordered player pair (A, B), track how many consecutive times A has killed B without B killing A back. Streaks accumulate across matches and reset when the victim gets a kill back. Expose the current top streaks at `GET /api/stats/nemesis`. Requires kill event detection from snapshot deltas and a new `player_pair_killstreak` table storing current streak and all-time peak per pair.

## Done

- [x] Draft tracking — `match_draft` table + `GET /api/matches/{id}/draft`. Populated from POST_GAME GSI packets (Captain's Mode only — All Pick has no draft block). team3=radiant, team2=dire per Dota 2 internal numbering.
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
