// Package match holds shared state between the bot and the GSI ingest pipeline.
package match

import (
	"log"
	"sync"
	"time"
)

const (
	// confirmThreshold is the number of players that must report the same match
	// ID before the gate locks. Set to 1 for solo testing; raise for production.
	confirmThreshold = 1

	// openTTL is the maximum time the gate will wait in the confirmation phase
	// before giving up. Acts as a last resort if the match never starts.
	openTTL = 4 * time.Hour

	// idleTimeout is how long the gate stays locked with no incoming packets
	// before closing. Handles the case where players stop sending without
	// every player reporting POST_GAME (e.g. someone without GSI configured).
	idleTimeout = 30 * time.Second
)

// Gate controls GSI ingest through three states:
//
//	closed → open   (bot calls Open when !start is received)
//	open   → locked (confirmThreshold players send the same match ID)
//	locked → closed (all seen players report POST_GAME, or idle for 30s)
//
// Before the gate is locked, packets are not written to the database.
// Once locked, only packets for the confirmed match ID are accepted.
type Gate struct {
	mu sync.Mutex

	open      bool
	expiresAt time.Time // deadline for the confirmation (open) phase only

	lockedMatchID string
	candidates    map[string]map[string]struct{} // matchID → set of playerSteamIDs

	// Populated once the gate is locked to a match.
	seenPlayers     map[string]struct{} // all players that sent a packet while locked
	postGamePlayers map[string]struct{} // players that reported POST_GAME
	idleTimer       *time.Timer

	// onAbandon is called (in a goroutine) whenever a locked match is discarded
	// without normal completion — idle timeout, forced reset, or a new Open() call
	// while still locked. The dota match ID string is passed so the caller can
	// delete the incomplete match from the database.
	onAbandon func(dotaMatchID string)
}

// SetOnAbandon registers a callback that is called (in a goroutine) whenever a
// locked match is discarded without normal completion. The dota match ID is
// passed so the caller can delete the incomplete match from the database.
// Must be called before the gate is opened for the first time.
func (g *Gate) SetOnAbandon(fn func(dotaMatchID string)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onAbandon = fn
}

// Open marks the gate as open and resets any prior state.
// Called by the bot when the lobby is launched via !start.
// If the gate is currently locked to a match, that match is abandoned and the
// onAbandon callback fires — Open() must not be called mid-match without the
// match having been properly completed or cleaned up first.
func (g *Gate) Open() {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Abandon any match that was locked but never completed.
	if g.lockedMatchID != "" {
		log.Printf("[gate] abandoning locked match %s — gate re-opened", g.lockedMatchID)
		g.fireAbandonLocked(g.lockedMatchID)
	}

	g.stopIdleTimerLocked()
	g.open = true
	g.expiresAt = time.Now().Add(openTTL)
	g.lockedMatchID = ""
	g.candidates = make(map[string]map[string]struct{})
	g.seenPlayers = nil
	g.postGamePlayers = nil
	log.Printf("[gate] open — waiting for %d player(s) to confirm match ID", confirmThreshold)
}

// Close marks the gate as closed. Used by the reset handler to force-close.
func (g *Gate) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.open {
		log.Println("[gate] closed — forced reset")
	}
	g.closeLocked(false)
}

// closeLocked tears down all gate state. Must be called with g.mu held.
// Pass completed=true when the match finished normally (all POST_GAME stats
// already written); pass false for every other path (idle timeout, forced
// reset, TTL expiry) so the abandon callback fires and cleans up the DB.
func (g *Gate) closeLocked(completed bool) {
	mid := g.lockedMatchID // capture before clearing
	g.stopIdleTimerLocked()
	g.open = false
	g.lockedMatchID = ""
	g.candidates = nil
	g.seenPlayers = nil
	g.postGamePlayers = nil
	if !completed && mid != "" {
		g.fireAbandonLocked(mid)
	}
}

// fireAbandonLocked calls onAbandon in a goroutine. Must be called with g.mu held.
func (g *Gate) fireAbandonLocked(dotaMatchID string) {
	if g.onAbandon == nil {
		return
	}
	fn := g.onAbandon
	go fn(dotaMatchID)
}

// stopIdleTimerLocked stops and nils the idle timer. Must be called with g.mu held.
func (g *Gate) stopIdleTimerLocked() {
	if g.idleTimer != nil {
		g.idleTimer.Stop()
		g.idleTimer = nil
	}
}

// resetIdleTimerLocked restarts the idle timer. Must be called with g.mu held.
func (g *Gate) resetIdleTimerLocked() {
	g.stopIdleTimerLocked()
	g.idleTimer = time.AfterFunc(idleTimeout, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.open && g.lockedMatchID != "" {
			log.Printf("[gate] closed — no packets for %s", idleTimeout)
			g.closeLocked(false)
		}
	})
}

// IsOpen reports whether the gate is currently open.
// Used as a fast pre-auth check in the GSI handler before hitting the database.
func (g *Gate) IsOpen() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.open
}

// State returns a short human-readable description of the current gate state.
func (g *Gate) State() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.open {
		return "closed"
	}
	if g.lockedMatchID != "" {
		return "locked(" + g.lockedMatchID + ")"
	}
	return "open"
}

// Accept reports whether a packet for matchID from playerSteamID should be stored.
//
//   - Locked: accepts only packets for the confirmed match ID, tracks the player,
//     and resets the idle timer.
//   - Open/unconfirmed: records the player's vote. Returns false until
//     confirmThreshold unique players agree on the same match ID, then locks.
//   - Closed, or confirmation phase past TTL: always false.
func (g *Gate) Accept(matchID, playerSteamID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.open {
		return false
	}

	// Already locked — only pass packets for the confirmed match.
	if g.lockedMatchID != "" {
		if matchID != g.lockedMatchID {
			return false
		}
		g.seenPlayers[playerSteamID] = struct{}{}
		g.resetIdleTimerLocked()
		return true
	}

	// Confirmation phase: enforce TTL so the gate doesn't stay open forever
	// if the match never actually starts.
	if time.Now().After(g.expiresAt) {
		log.Println("[gate] closed — confirmation TTL expired")
		g.closeLocked(false) // lockedMatchID is "" here, so no abandon callback fires
		return false
	}

	// Record this player's vote for this match ID.
	if g.candidates[matchID] == nil {
		g.candidates[matchID] = make(map[string]struct{})
	}
	g.candidates[matchID][playerSteamID] = struct{}{}
	count := len(g.candidates[matchID])
	log.Printf("[gate] match %s seen by %d/%d players", matchID, count, confirmThreshold)

	if count >= confirmThreshold {
		g.lockedMatchID = matchID
		g.candidates = nil
		g.seenPlayers = map[string]struct{}{playerSteamID: {}}
		g.postGamePlayers = make(map[string]struct{})
		g.resetIdleTimerLocked()
		log.Printf("[gate] locked to match %s", matchID)
		return true
	}

	return false
}

// PostGame records that a player has sent a POST_GAME packet.
// The gate closes once every seen player has reported post-game,
// indicating the match is fully collected.
func (g *Gate) PostGame(playerSteamID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.open || g.lockedMatchID == "" {
		return
	}

	g.postGamePlayers[playerSteamID] = struct{}{}
	log.Printf("[gate] post-game from %s (%d/%d players reported)",
		playerSteamID, len(g.postGamePlayers), len(g.seenPlayers))

	if len(g.postGamePlayers) >= len(g.seenPlayers) {
		log.Println("[gate] closed — all seen players reported POST_GAME")
		g.closeLocked(true) // match is complete — stats already written to DB
	}
}
