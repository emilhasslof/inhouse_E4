// Package match holds shared state between the bot and the GSI ingest pipeline.
package match

import (
	"log"
	"sync"
	"time"
)

const (
	// openTTL is the maximum time the gate will wait in the confirmation phase
	// before giving up. Acts as a last resort if the match never starts.
	openTTL = 4 * time.Hour

	// idleTimeout is how long the gate stays locked with no incoming packets
	// before closing. Handles the case where players stop sending without
	// every player reporting POST_GAME (e.g. someone without GSI configured).
	// Set generously: GSI pauses emitting while the game is paused, and long
	// pauses (tech issues, breaks) are common.
	idleTimeout = 30 * time.Minute

	// postCompletionIdleTimeout is the shorter idle timeout used once the match
	// is marked completed (first POST_GAME packet received). Stragglers' POST_GAMEs
	// arrive within seconds, so we don't need to wait the full idle window —
	// freeing the gate quickly lets the next !start go through.
	postCompletionIdleTimeout = 3 * time.Minute
)

// Gate controls GSI ingest through three states:
//
//	closed → open   (bot calls Open when !start is received)
//	open   → locked (threshold players send the same match ID)
//	locked → closed (all seen players report POST_GAME, idle timeout, or
//	                 forced reset; idle timeout drops to 3 min after first POST_GAME)
//
// Before the gate is locked, packets are not written to the database.
// Once locked, only packets for the confirmed match ID are accepted.
type Gate struct {
	mu        sync.Mutex
	threshold int

	open      bool
	expiresAt time.Time // deadline for the confirmation (open) phase only

	lockedMatchID string
	candidates    map[string]map[string]struct{} // matchID → set of playerSteamIDs

	// Populated once the gate is locked to a match.
	seenPlayers     map[string]struct{} // all players that sent a packet while locked
	postGamePlayers map[string]struct{} // players that reported POST_GAME
	idleTimer       *time.Timer
	completed       bool // set by MarkCompleted on the first POST_GAME packet

	// onFinalize is called (in a goroutine) whenever a *completed* match's gate
	// closes — whether via all-POST_GAME, idle timeout, forced reset, or a new
	// Open() call while still locked. The DB row already says state='completed';
	// the callback should promote any leftover live_match_stats into final stats
	// and clear them. Never deletes the match.
	onFinalize func(dotaMatchID string)

	// onAbandon is called (in a goroutine) whenever a locked match closes
	// *without* having been marked completed — i.e. no POST_GAME packet ever
	// arrived. The match should be moved to the archive DB so we never lose
	// data, even forensically.
	onAbandon func(dotaMatchID string)
}

// New creates a Gate with the given confirmation threshold — the number of
// unique players that must report the same match ID before the gate locks.
func New(threshold int) *Gate {
	if threshold < 1 {
		threshold = 1
	}
	return &Gate{threshold: threshold}
}

// SetOnAbandon registers the callback for never-completed matches (no POST_GAME
// ever arrived). Must be called before the gate is opened for the first time.
func (g *Gate) SetOnAbandon(fn func(dotaMatchID string)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onAbandon = fn
}

// SetOnFinalize registers the callback fired when a completed match's gate
// closes. Must be called before the gate is opened for the first time.
func (g *Gate) SetOnFinalize(fn func(dotaMatchID string)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onFinalize = fn
}

// MarkCompleted records that the locked match has reached POST_GAME at least
// once (the DB row is now state='completed'). Switches the idle timer to the
// shorter post-completion timeout and ensures any subsequent gate close fires
// onFinalize instead of onAbandon. Safe to call multiple times.
func (g *Gate) MarkCompleted() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.open || g.lockedMatchID == "" || g.completed {
		return
	}
	g.completed = true
	g.resetIdleTimerLocked()
	log.Printf("[gate] match %s marked completed — idle timeout now %s", g.lockedMatchID, postCompletionIdleTimeout)
}

// Open marks the gate as open and resets any prior state.
// Called by the bot when the lobby is launched via !start.
// If the gate is currently locked to a match, that match is abandoned and the
// onAbandon callback fires — Open() must not be called mid-match without the
// match having been properly completed or cleaned up first.
func (g *Gate) Open() {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Discard any match that was locked but never closed cleanly.
	if g.lockedMatchID != "" {
		if g.completed {
			log.Printf("[gate] finalizing locked match %s — gate re-opened", g.lockedMatchID)
			g.fireFinalizeLocked(g.lockedMatchID)
		} else {
			log.Printf("[gate] abandoning locked match %s — gate re-opened", g.lockedMatchID)
			g.fireAbandonLocked(g.lockedMatchID)
		}
	}

	g.stopIdleTimerLocked()
	g.open = true
	g.expiresAt = time.Now().Add(openTTL)
	g.lockedMatchID = ""
	g.candidates = make(map[string]map[string]struct{})
	g.seenPlayers = nil
	g.postGamePlayers = nil
	g.completed = false
	log.Printf("[gate] open — waiting for %d player(s) to confirm match ID", g.threshold)
}

// Close marks the gate as closed. Used by the reset handler to force-close.
func (g *Gate) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.open {
		log.Println("[gate] closed — forced reset")
	}
	g.closeLocked()
}

// closeLocked tears down all gate state. Must be called with g.mu held.
//
// If a match was locked, the post-close callback is chosen by the gate's
// internal `completed` flag: completed → onFinalize, never-completed → onAbandon.
func (g *Gate) closeLocked() {
	mid := g.lockedMatchID
	wasCompleted := g.completed

	g.stopIdleTimerLocked()
	g.open = false
	g.lockedMatchID = ""
	g.candidates = nil
	g.seenPlayers = nil
	g.postGamePlayers = nil
	g.completed = false

	if mid == "" {
		return
	}
	if wasCompleted {
		g.fireFinalizeLocked(mid)
	} else {
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

// fireFinalizeLocked calls onFinalize in a goroutine. Must be called with g.mu held.
func (g *Gate) fireFinalizeLocked(dotaMatchID string) {
	if g.onFinalize == nil {
		return
	}
	fn := g.onFinalize
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
// Uses the short post-completion timeout once the gate has been MarkCompleted.
func (g *Gate) resetIdleTimerLocked() {
	g.stopIdleTimerLocked()
	d := idleTimeout
	if g.completed {
		d = postCompletionIdleTimeout
	}
	g.idleTimer = time.AfterFunc(d, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.open && g.lockedMatchID != "" {
			log.Printf("[gate] closed — no packets for %s", d)
			g.closeLocked()
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

// LockedMatchID returns the match ID the gate is currently locked to, or the
// empty string if the gate is closed or in the open/confirmation phase.
func (g *Gate) LockedMatchID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lockedMatchID
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
//     g.threshold unique players agree on the same match ID, then locks.
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
		g.closeLocked() // lockedMatchID is "" here, so no abandon callback fires
		return false
	}

	// Record this player's vote for this match ID.
	if g.candidates[matchID] == nil {
		g.candidates[matchID] = make(map[string]struct{})
	}
	g.candidates[matchID][playerSteamID] = struct{}{}
	count := len(g.candidates[matchID])
	log.Printf("[gate] match %s seen by %d/%d players", matchID, count, g.threshold)

	if count >= g.threshold {
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
		g.closeLocked() // routes through onFinalize since g.completed is set
	}
}
