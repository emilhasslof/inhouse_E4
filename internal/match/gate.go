// Package match holds shared state between the bot and the GSI ingest pipeline.
package match

import (
	"log"
	"sync"
	"time"
)

const (
	ttl              = 4 * time.Hour
	confirmThreshold = 2
)

// Gate controls GSI ingest through three states:
//
//	closed → open  (bot calls Open when lobby is launched via !start)
//	open   → locked (3 registered players send packets with the same match ID)
//	locked → closed (GSI handler calls Close on POST_GAME)
//
// In the open/unconfirmed state packets are tracked but not stored — data is
// only written once the match is confirmed. Once locked, only packets with the
// confirmed match ID are accepted, blocking any concurrent matchmaking games.
type Gate struct {
	mu        sync.Mutex
	open      bool
	expiresAt time.Time

	lockedMatchID string
	candidates    map[string]map[string]struct{} // matchID → set of playerSteamIDs
}

// Open marks the gate as open and resets any prior confirmation state.
// Called by the bot when the lobby is launched.
func (g *Gate) Open() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.open = true
	g.expiresAt = time.Now().Add(ttl)
	g.lockedMatchID = ""
	g.candidates = make(map[string]map[string]struct{})
	log.Println("[gate] open — waiting for match confirmation from 2 players")
}

// Close marks the gate as closed. Called by the GSI handler on POST_GAME.
func (g *Gate) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.open = false
	g.lockedMatchID = ""
	g.candidates = nil
}

// IsOpen reports whether the gate is open (confirmed or not) and not expired.
// Used as a fast pre-auth check in the GSI handler before hitting the database.
func (g *Gate) IsOpen() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.open && time.Now().Before(g.expiresAt)
}

// State returns a short human-readable description of the current gate state.
func (g *Gate) State() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.open {
		return "closed"
	}
	if time.Now().After(g.expiresAt) {
		return "expired"
	}
	if g.lockedMatchID != "" {
		return "locked(" + g.lockedMatchID + ")"
	}
	return "open"
}

// Accept reports whether a packet for matchID from playerSteamID should be
// stored. It drives the confirmation state machine:
//
//   - Locked: accepts only packets matching the confirmed match ID.
//   - Open/unconfirmed: records the (matchID, playerSteamID) pair. Returns
//     false until confirmThreshold unique players agree on the same match ID,
//     then locks and returns true for the confirming packet onwards.
//   - Closed or expired: always false.
func (g *Gate) Accept(matchID, playerSteamID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.open || time.Now().After(g.expiresAt) {
		return false
	}

	// Already locked — only pass packets for the confirmed match.
	if g.lockedMatchID != "" {
		return matchID == g.lockedMatchID
	}

	// Unconfirmed — record this player's vote for this match ID.
	if g.candidates[matchID] == nil {
		g.candidates[matchID] = make(map[string]struct{})
	}
	g.candidates[matchID][playerSteamID] = struct{}{}
	count := len(g.candidates[matchID])
	log.Printf("[gate] match %s seen by %d/%d players", matchID, count, confirmThreshold)

	if count >= confirmThreshold {
		g.lockedMatchID = matchID
		g.candidates = nil
		log.Printf("[gate] locked to match %s", matchID)
		return true
	}

	return false
}
