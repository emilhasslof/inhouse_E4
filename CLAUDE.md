# inhouse-E4 — Project State

> **Keep this file current.** Update it at the end of any session where something significant changed — architecture decisions, findings, dead ends, or shift in direction. Future Claude instances read this to get up to speed without replaying the full conversation history.

## Goal

Dota 2 inhouse league stats website for a small group (~10-20 players). Collect match data via Game State Integration (GSI), store it in SQLite, and serve a leaderboard/scoreboard web UI.

## Scope

**We are working on the backend only.** The frontend (`/home/emilh/inhouse-E6/frontend`) is present for reference but should not be modified. Do not suggest or make changes to frontend files.

## Architecture

Two-part system: a Go backend API + a separate React/TypeScript frontend (in `/home/emilh/inhouse-E6/frontend`, built with Lovable/Vite).

The Go backend is a single binary (`cmd/server`) that handles GSI ingest and serves a JSON REST API. SQLite on a railway persistent volume.

```
Player's Dota client → POST /gsi → Go HTTP server → SQLite (data/inhouse.db)
                                                            ↓
                                                     JSON REST API (/api/*)
                                                            ↓
                                             React frontend (separate deployment)
```

**API endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| GET | /healthz | Health check — returns `{"status":"ok"}` |
| GET | /api | Endpoint spec — lists all routes and response shapes |
| POST | /gsi | GSI payload ingest from Dota clients |
| POST | /api/register | Register a new player — takes `{steam_id, display_name}`, returns `{token}`. Open, no auth required. 409 if Steam ID already registered. |
| GET | /api/matches | List matches with team player names |
| GET | /api/matches/{id} | Match detail + scoreboard |
| GET | /api/players | Player leaderboard (wins/losses/streak/GPM) |
| GET | /api/stats/heroes | Hero pick/win counts |
| GET | /api/stats/overview | League-wide aggregate stats |
| GET | /api/registered-players | All registered players (display_name, steam_id) |
| POST | /api/lobby/create | Create lobby + invite players — takes `{steam_ids: string[], game_mode?: "captains_mode"\|"all_pick"}` (default: `"captains_mode"`). Cheats disabled. 400 if any ID unregistered. Match gate locks after 2 players confirm the same match ID. |
| POST | /api/lobby/reset | Hard-reset the bot (abandon lobby, kill connection, reconnect). 503 if bot not configured. |

CORS is open (`*`) so the frontend can call from any origin.

**Live URL:** `https://inhousee4-production.up.railway.app`

**Frontend connection:** `API_BASE` in `frontend/src/lib/api.ts` — set to the origin only (no trailing `/api`). The fetch calls append `/api/...` themselves.

**Key dependencies:**
- `github.com/go-chi/chi/v5` — HTTP routing
- `modernc.org/sqlite` — pure-Go SQLite (no CGO, clean Docker builds)
- `github.com/paralin/go-steam` + `go-dota2` — Steam bot (lobby creation only)

## How Data Collection Works

Each player installs a GSI config file that sends live match data to the server every ~1 second. The server authenticates payloads by a **pre-shared per-player token** embedded in the config. Since each player only sees their own `player` block, stats are aggregated by receiving from all 10 players independently.

GSI config location: `~/.local/share/Steam/steamapps/common/dota 2 beta/game/dota/cfg/gamestate_integration/gamestate_integration_inhouse.cfg`

Post-game detection: when `map.game_state == "DOTA_GAMERULES_STATE_POST_GAME"`, the server materialises final stats into `match_player_stats` and marks the match completed.

## Database Schema (SQLite)

| Table | Purpose |
|---|---|
| `players` | Registered players — display name + unique GSI auth token |
| `matches` | One row per match — state, scores, duration |
| `gsi_snapshots` | Raw 1-per-second stream per player — source for gold graphs, kill timelines, kill events |
| `match_player_stats` | Materialised end-of-match K/D/A/GPM/XPM — what the web pages read |
| `player_pair_killstreak` | **Planned** — current and all-time peak killstreak for each ordered (killer, victim) player pair, accumulated across matches |

## File Map

| Path | Purpose |
|---|---|
| `cmd/server/main.go` | Server entry point — opens DB, wires handlers, listens |
| `cmd/bot/main.go` | Steam bot — creates lobbies, self-kicks, waits for `!start` |
| `internal/bot/manager.go` | `Manager` wraps `Service` and owns its lifecycle — used by the web handler for lobby create and hard reset |
| `cmd/datagen/main.go` | **Dev only** — fake GSI generator for 10 simulated players |
| `internal/db/` | SQLite layer: schema, queries, types (all types have JSON tags) |
| `internal/gsi/handler.go` | `POST /gsi` — auth, snapshot insert, post-game detection |
| `internal/web/handlers.go` | JSON API handlers for all `/api/*` endpoints |
| `internal/web/routes.go` | Chi router — GSI ingest + API routes + CORS middleware |
| `data/` | SQLite database files — gitignored |
| `.env` | Steam credentials — gitignored |
| `Dockerfile` | Builds `cmd/server` only (datagen is never included) |
| `railway.toml` | Railway config — health check path `/healthz`, timeout 30s |
| `fly.toml` | Old Fly.io config — kept for reference but not used |

## TODO.md

`TODO.md` is the shared task board. Keep it up to date:
- If you complete something on the list, mark it `[x]` or move it to Done.
- If you notice something that should be done — a bug, a missing feature, a follow-up — add it.
- If a user mentions something they want but it's out of scope for the current branch, add it to the backlog instead of doing it now.

## Git Workflow

**Work on `dev`.** Commit to `dev` during development. Merge `dev` → `main` when ready to deploy (triggers Railway). Direct commits to `main` only for urgent hotfixes when explicitly asked.

**Branch naming:**
- `feature/short-description` — new functionality
- `bugfix/short-description` — fixing broken behaviour
- `enhancement/short-description` — improving existing functionality

**Merging:** anyone can merge once one other team member has approved the PR.

**Scope discipline:** branches should be short and focused. As work progresses, actively monitor whether the changes are staying on topic. If the work is starting to span multiple unrelated concerns — for example touching both the ingest pipeline and the UI for unrelated reasons, or pulling in extra features beyond what was asked — pause and say something like:

> "This is starting to touch a few different things. Would you like to keep the current branch focused on X and open a new branch for Y?"

Don't wait until the branch is already large. Raise it early, and raise it nicely.

## Running Locally

```bash
# Start server in dev mode (auto-seeds 10 fake datagen players)
APP_ENV=development go run ./cmd/server

# In another terminal: simulate a match
go run ./cmd/datagen
# Commands: start, stop, status, quit
```

After `stop`, hit `http://localhost:8080/api/players` or `/api/matches` to verify stats.
To see the UI: run the frontend (`cd ../frontend && npm run dev`). The frontend reads `VITE_API_BASE` from `frontend/.env.local` — set it to `http://localhost:8080` for local dev or the Railway URL for production.

## Deploying

Hosted on Railway, auto-deploys on push to `main` via Railway's GitHub integration (not GitHub Actions — there are no workflow files).

```bash
git push   # triggers a Railway build and deploy automatically
```

Railway project: `inhouse-e4` (emilhasslof's workspace)
Live URL: `https://inhousee4-production.up.railway.app`

**Environment variables on Railway:**
- `APP_ENV=development` — seeds 10 players + 3 fake matches on boot (remove when going live with real players)
- `DB_PATH=/data/inhouse.db` — set this when a persistent volume is attached

**Persistent volume configured** — volume `inhouse_e4-volume` is mounted at `/data`. `DB_PATH=/data/inhouse.db` is set in Railway env. DB survives redeploys.

**Simulating matches against production:**
```bash
go run ./cmd/datagen -target https://inhousee4-production.up.railway.app
```

## Adding Real Players

Players self-register via `register.bat` (lives at the repo root, outside the backend folder — not deployed). The script:
1. Reads the player's Steam ID from `loginusers.vdf` automatically
2. Prompts for a display name
3. POSTs to `POST /api/register` — backend generates a random token and inserts the player row
4. Writes the GSI config file to the correct Dota install folder

Registration is open — no admin secret required. If a Steam ID is already registered, the endpoint returns 409.

Manual fallback (if needed):
```sql
INSERT INTO players (steam_id, display_name, token)
VALUES ('76561197990491029', 'PlayerName', 'abc123...');
```

## GSI Data Notes

- Match gate confirmation threshold is **1 player** (for solo testing; raise `confirmThreshold` in `internal/match/gate.go` for production).
- `win_team` is captured from `map.win_team` in POST_GAME packets and stored in the `matches` table. All win/loss queries use `mps.team_name = m.win_team` — not kill score comparison.
- Lobby cheats are disabled (`AllowCheats: false`). Game mode defaults to Captain's Mode; pass `game_mode: "all_pick"` to override.
- Register scripts (`register.sh` / `register.bat`) use the Steam persona name from `loginusers.vdf` — no manual name entry. Already-registered players still get the bot friend prompt.

## Schema Migrations

`CREATE TABLE IF NOT EXISTS` in `schemaSQL` only creates tables on first run — it does not add new columns to existing tables. New columns must be added as `ALTER TABLE ... ADD COLUMN` statements in the `additiveMigrations` slice in `db.go:migrate()`. These are safe to re-run (SQLite's "duplicate column name" error is swallowed).

**Migrated columns so far:**
- `matches.win_team TEXT NOT NULL DEFAULT ''` — added after initial schema, captures winning team from GSI POST_GAME.

## Key Findings / Dead Ends

### Why not the GC API for match stats?
- `RequestMatchDetails` returns result=15 (AccessDenied) for private practice lobbies
- Valve does not record private lobby matches in the GC match history
- `FindTopSourceTVGames` returns empty for private lobbies
- **Solution:** GSI instead — the client pushes data to us during the match

### Steam bot slot management
- `JoinLobbyTeam` with `NOTEAM`/`BROADCASTER`/`SPECTATOR` all still block game launch (bot appears as a game client)
- **Solution:** `KickLobbyMemberFromTeam` on the bot's own account ID — moves it to unassigned pool, retains host status, doesn't block launch

### Steam auth
- TOTP code must be generated immediately before `LogOn()`, not at startup — near-expiry codes fail mid-TCP-handshake
- On `EResult_TwoFactorCodeMismatch`, bot waits for the next 30s window and retries automatically

### GSI `allplayers` is observer-only
- Confirmed from real captured payloads: `allplayers` is **never present** for a regular player, only for observers with full vision. Valve intentionally gates it.
- **Consequence:** all 10 players must have GSI configured and active to get complete match stats. There is no shortcut.
- An observer bot was considered and ruled out — too complex. GSI from players is the only data source.

### Steam bot GC connection on Railway
- Railway blocks some Steam CM ports (e.g. 27017 on certain IPs). The initial connect often succeeds then immediately gets EOF (CM redirect), and subsequent reconnects to specific IPs time out.
- **Fix:** `DisconnectedEvent` handler calls `go connectWithRetry(ctx)` instead of a one-shot `s.client.Connect()`. `connectWithRetry` retries indefinitely against different CMs until one responds.
- `gcReady`/`gcReadyOnce` must be reset on each `LoggedOnEvent` — if not, a reconnect's SayHello goroutine exits immediately (old channel already closed) and `CreateLobbyAndInvite` skips the GC wait, leading to undefined behaviour.
- `go-dota2` emits `unknown shared object type id: 2013` warnings when it can't parse a GC welcome cache message. This prevents `GCConnectionStatus_HAVE_SESSION` from being dispatched reliably. Lobby creation still works (GC responds to `LeaveCreateLobby`), but Dota lobby chat events (`events.ChatMessage`) may not arrive.
- **`!start` fallback:** also listen on `*steam.ChatMsgEvent` (Steam direct DM) so players can trigger lobby launch regardless of GC session state.
- `lobbyMu` must stay held for the full lobby lifetime (creation + `waitForStart`). Releasing it after launch allows a rapid second POST to call `LeaveCreateLobby` concurrently, stomping the active lobby.

### Match gate close logic
- The gate has three states: **closed** → **open** (on `!start`) → **locked** (once `confirmThreshold` players confirm the same match ID) → **closed**.
- **Open phase TTL** (4h): if the match never confirms, the gate self-closes. Checked in `Accept()` only — `IsOpen()` no longer checks TTL.
- **Locked phase**: TTL is intentionally NOT applied. Instead, a 30-second idle timer (reset on every accepted packet) closes the gate if packets stop flowing. This prevents the gate from closing mid-game due to a stale TTL from a previous session.
- **POST_GAME tracking**: the gate tracks `seenPlayers` (everyone who sent a packet while locked) and `postGamePlayers`. Once every seen player has sent a POST_GAME packet, the gate closes immediately without waiting for the idle timer. Call `gate.PostGame(steamID)` from the handler — do not call `gate.Close()` directly.
- **Bug found (and fixed):** a TTL check that existed in both `IsOpen()` and `Accept()` caused the gate to lock to a match ID from an expired session, then immediately close on the next `IsOpen()` call. Root cause: `Accept()` did not check TTL, so it could lock even when expired; `IsOpen()` then detected expiry and closed the gate.

### GSI data quality notes
- `gpm` and `xpm` are wildly inflated for the first ~10 seconds of `clock_time`. Always guard sampling with `clock_time > 10`.
- `player.kill_list` maps victim slot → kill count within the current kill streak. It resets to empty when the player dies. Useful for detecting kill events by diffing successive snapshots.
- Enemy buildings are not visible in any player's GSI feed (fog of war). Only own-team buildings appear.
- `draft` block is only populated in Captain's Mode. It is `{}` in All Pick.

## Open Questions

- Is the Steam bot still needed once GSI is proven? Could players self-host lobbies, or do we keep the bot for consistency?
