# TODO

Anyone (human or agent) can add items here at any time. No ceremony required.
Mark items done with `[x]` when complete, or remove them.

---

## Up next

- [ ] Run a real match with GSI active and inspect the `allplayers` block — does it include enemy stats for a non-spectator?
- [ ] Add `POST /api/bot/reset` endpoint to hard-reset the bot (disconnect + reconnect + re-establish GC) — for a frontend admin button
- [ ] Kick bot from player slot (via `KickLobbyMemberFromTeam`) instead of leaving lobby — bot retains host status and can still handle `!start` in chat

## Backlog

- [ ] Add persistent Railway volume before real matches start (DB currently resets on redeploy)
- [ ] Remove `APP_ENV=development` from Railway once real players are registered
- [ ] Gold-over-time graph on the match detail page (data is already in `gsi_snapshots`)

## Done

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
