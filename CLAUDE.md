# inhouse-E6 — Project State

> **Keep this file current.** Update it at the end of any session where something significant changed — architecture decisions, findings, dead ends, or shift in direction. Future Claude instances read this to get up to speed without replaying the full conversation history.

## Goal

Automate private Dota 2 inhouse lobby creation and collect post-match statistics for an amateur inhouse league website.

## Architecture

Single Go binary (`main.go`) using:
- `github.com/paralin/go-steam` — Steam client connection and auth
- `github.com/paralin/go-dota2` — Dota 2 Game Coordinator (GC) protocol
- TOTP auth via `.env` (`STEAM_TOTP_SECRET`)

Supporting tool:
- `gsi/main.go` — standalone HTTP server that receives Dota 2 GSI payloads on `:1337`

## Bot Lifecycle

1. Connect to Steam with fresh TOTP code (generated at `ConnectedEvent`, not startup — fixes ~80% auth failure rate)
2. Connect to Dota 2 GC
3. Create lobby (AP mode, Europe West, no cheats, spectating allowed)
4. Read lobby ID from SOCache
5. Kick self (`KickLobbyMemberFromTeam`) from team slot → moves bot to unassigned pool
6. Wait for `start` command (stdin) or `!start` in lobby chat
7. Launch game
8. Poll `RequestMatchDetails` every 30s until result=1 (or 4h timeout)
9. Dump full `CMsgGCMatchDetailsResponse` JSON

## Key Findings / Dead Ends

### Slot management
- `BROADCASTER`, `SPECTATOR`, `NOTEAM` via `JoinLobbyTeam` all fail — bot still appears as a connecting game client and blocks launch
- **Solution that works:** `KickLobbyMemberFromTeam` on the bot's own account ID — moves it to unassigned pool, doesn't block game launch, host status retained

### Match stats access
- `RequestMatchDetails` returns result=2 (in progress) during game, result=15 (AccessDenied) after
- Private practice lobbies are **not recorded** in the GC match history — this is a Valve policy decision, not an auth problem
- `FindTopSourceTVGames` with lobby ID returns empty — private lobbies don't appear in SourceTV listings
- SOCache lobby subscription stops receiving updates once the game starts (bot is not a game client)

### Auth
- TOTP code must be generated immediately before `LogOn()`, not at startup — a code generated near end of 30s window expires before TCP handshake completes
- On `EResult_TwoFactorCodeMismatch`, bot waits for the next window and retries automatically

## Current Direction: GSI

Since the GC API is a dead end for private lobbies, pivoting to **Dota 2 Game State Integration**:
- Config installed at: `~/.local/share/Steam/steamapps/common/dota 2 beta/game/dota/cfg/gamestate_integration/gamestate_integration_inhouse.cfg`
- Sends to `http://localhost:1337/gsi`
- `gsi/main.go` receives payloads, prints sections, saves timestamped JSON files
- Next step: run a match with GSI active and inspect what data is available, particularly `allplayers` and whether enemy stats are included

## Open Questions

- Does `allplayers` in GSI include enemy gold/networth for a regular player, or only for spectators?
- If enemy data is restricted, can we aggregate from all 10 players' individual feeds?
- Is the bot still useful alongside GSI for lobby creation/management, or can players self-host?

## File Map

| File | Purpose |
|------|---------|
| `main.go` | Bot: Steam auth, lobby creation, self-kick, game launch, match detail polling |
| `gsi/main.go` | GSI receiver: HTTP server, payload logging, JSON dumps |
| `run.sh` | `go run .` with protobuf conflict suppression |
| `.env` | Steam credentials (gitignored) |
| `gamestate_integration_inhouse.cfg` | Dota GSI config (in Steam install, not this repo) |
