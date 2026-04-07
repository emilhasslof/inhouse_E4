# inhouse-E6 — Project State

> **Keep this file current.** Update it at the end of any session where something significant changed — architecture decisions, findings, dead ends, or shift in direction. Future Claude instances read this to get up to speed without replaying the full conversation history.

## Goal

Dota 2 inhouse league stats website for a small group (~10-20 players). Collect match data via Game State Integration (GSI), store it in SQLite, and serve a leaderboard/scoreboard web UI.

## Architecture

Single Go binary (`cmd/server`) serving everything: GSI ingest endpoint, web pages, and static assets. SQLite on a Fly.io persistent volume. No separate frontend build step — HTML templates and CSS are embedded directly in the binary via `go:embed`.

```
Player's Dota client → POST /gsi → Go HTTP server → SQLite (data/inhouse.db)
                                                            ↓
                                              HTMX + plain CSS web pages
```

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
| `internal/db/` | SQLite layer: schema, queries, types |
| `internal/gsi/handler.go` | `POST /gsi` — auth, snapshot insert, post-game detection |
| `internal/web/` | HTTP handlers + chi router for web pages |
| `web/templates/` | HTML templates (layout, matches list, scoreboard, leaderboard) |
| `web/static/style.css` | Dark Dota-themed CSS |
| `web/web.go` | `go:embed` declarations + pre-parsed templates |
| `gsi/main.go` | Original local GSI debug receiver (reference only, not used in prod) |
| `data/` | SQLite database files — gitignored |
| `.env` | Steam credentials — gitignored |
| `Dockerfile` | Builds `cmd/server` only (datagen is never included) |
| `fly.toml` | Fly.io config — `ams` region, volume mount at `/data` |

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

Open `http://localhost:8080`. After `stop`, the scoreboard page shows all 10 players' stats.

## Deploying

```bash
fly launch    # first time only
fly deploy
```

The Dockerfile builds only `cmd/server`. `DB_PATH` defaults to `/data/inhouse.db` which is the mounted Fly.io volume.

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
