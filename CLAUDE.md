# inhouse-E4 — Project State

> **Keep this file current.** Update it at the end of any session where something significant changed — architecture decisions, findings, dead ends, or shift in direction. Future Claude instances read this to get up to speed without replaying the full conversation history.

## Goal

Dota 2 inhouse league stats website for a small group (~10-20 players). Collect match data via Game State Integration (GSI), store it in SQLite, and serve a leaderboard/scoreboard web UI.

## Architecture

Two-part system: a Go backend API + a separate React/TypeScript frontend (in `/home/emilh/inhouse-E6/frontend`, built with Lovable/Vite).

The Go backend is a single binary (`cmd/server`) that handles GSI ingest and serves a JSON REST API. SQLite on a Fly.io persistent volume.

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
| GET | /api/matches | List matches with team player names |
| GET | /api/matches/{id} | Match detail + scoreboard |
| GET | /api/players | Player leaderboard (wins/losses/streak/GPM) |
| GET | /api/stats/heroes | Hero pick/win counts |
| GET | /api/stats/overview | League-wide aggregate stats |

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
| `gsi_snapshots` | Raw 1-per-second stream per player (future: gold graphs, kill timelines) |
| `match_player_stats` | Materialised end-of-match K/D/A/GPM/XPM — what the web pages read |

## File Map

| Path | Purpose |
|---|---|
| `cmd/server/main.go` | Server entry point — opens DB, wires handlers, listens |
| `cmd/bot/main.go` | Steam bot — creates lobbies, self-kicks, waits for `!start` |
| `cmd/datagen/main.go` | **Dev only** — fake GSI generator for 10 simulated players |
| `internal/db/` | SQLite layer: schema, queries, types (all types have JSON tags) |
| `internal/gsi/handler.go` | `POST /gsi` — auth, snapshot insert, post-game detection |
| `internal/web/handlers.go` | JSON API handlers for all 5 `/api/*` endpoints |
| `internal/web/routes.go` | Chi router — GSI ingest + API routes + CORS middleware |
| `gsi/main.go` | Original local GSI debug receiver (reference only, not used in prod) |
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

**Never commit directly to `main`.** Always work on a branch and open a PR.

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

Hosted on Railway, auto-deploys on push to `main` via GitHub Actions.

```bash
git push   # triggers a Railway build and deploy automatically
```

Railway project: `inhouse-e4` (emilhasslof's workspace)
Live URL: `https://inhousee4-production.up.railway.app`

**Environment variables on Railway:**
- `APP_ENV=development` — seeds 10 players + 3 fake matches on boot (remove when going live with real players)
- `DB_PATH=/data/inhouse.db` — set this when a persistent volume is attached

**No persistent volume yet** — DB lives in ephemeral container storage and resets on each deploy. Add a Railway volume mounted at `/data` before running real matches.

**Simulating matches against production:**
```bash
go run ./cmd/datagen -target https://inhousee4-production.up.railway.app
```

## Adding Real Players

Insert directly into SQLite (generate tokens with `openssl rand -hex 16`):

```sql
INSERT INTO players (steam_id, display_name, token)
VALUES ('76561197990491029', 'PlayerName', 'abc123...');
```

Distribute a personalised GSI config to each player with their token in the `auth` block.

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

## Open Questions

- Does `allplayers` in GSI include enemy stats for a regular player (not spectator)? If yes, one player's feed is sufficient per match instead of needing all 10. Needs a real match test.
- Is the Steam bot still needed once GSI is proven? Could players self-host lobbies, or do we keep the bot for consistency?
