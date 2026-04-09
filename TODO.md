# TODO

Anyone (human or agent) can add items here at any time. No ceremony required.
Mark items done with `[x]` when complete, or remove them.

---

## Up next

- [ ] Recover Steam bot credentials (`.env` was accidentally deleted and scrubbed from git history) — recreate `.env` with `STEAM_ACCOUNT_NAME`, `STEAM_PASSWORD`, `STEAM_TOTP_SECRET`, then push to Railway with `railway variable set`

- [ ] Run a real match with GSI active and inspect the `allplayers` block — does it include enemy stats for a non-spectator?
- [x] Player onboarding — `register.bat` (repo root) reads Steam ID automatically, calls `POST /api/register`, and writes the GSI config. Run once → stat collection works forever.

## Backlog

- [ ] Add persistent Railway volume before real matches start (DB currently resets on redeploy)
- [ ] Remove `APP_ENV=development` from Railway once real players are registered
- [ ] Gold-over-time graph on the match detail page (data is already in `gsi_snapshots`)

## Done

- [x] Prove GSI data can be received and parsed locally (`gsi/main.go`)
- [x] Scaffold Go server with SQLite and GSI ingest pipeline
- [x] Dev datagen tool for testing the pipeline without a real match
- [x] Rewritten to backend-only JSON API (removed HTMX frontend)
- [x] Deploy to Railway — live at https://inhousee4-production.up.railway.app
- [x] 23-test suite covering db/gsi/web layers
- [x] Dev seed data (3 fake matches) so frontend can develop against real API responses
