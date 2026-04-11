package bot

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/emilh/inhouse-e4/internal/db"
	"github.com/emilh/inhouse-e4/internal/match"
)

// Manager owns the bot Service lifecycle and can restart it on demand.
// It implements web.LobbyCreator, so the web handler holds the Manager and
// never needs to be updated when the underlying Service is replaced.
type Manager struct {
	gate *match.Gate

	mu     sync.Mutex
	svc    *Service
	cancel context.CancelFunc
}

// NewManager creates a Service, starts it, and returns the Manager.
// Returns nil if the bot is not configured (same semantics as bot.New).
func NewManager(gate *match.Gate) *Manager {
	svc := New(gate)
	if svc == nil {
		return nil
	}
	m := &Manager{gate: gate}
	m.mu.Lock()
	m.launch(svc)
	m.mu.Unlock()
	return m
}

// launch starts svc and stores it. Must be called with m.mu held.
func (m *Manager) launch(svc *Service) {
	ctx, cancel := context.WithCancel(context.Background())
	m.svc = svc
	m.cancel = cancel
	go svc.Start(ctx)
}

// Reset tears down the current Service and starts a completely fresh one.
// Any active Dota lobby is abandoned and pending !start waiters are cancelled.
// Blocks for ~2s to allow the old Steam connection to close before reconnecting.
func (m *Manager) Reset() {
	log.Println("[bot] hard reset requested")

	m.mu.Lock()
	if m.svc != nil {
		// Unblock any waitForStart goroutine so it exits cleanly.
		m.svc.startMu.Lock()
		close(m.svc.resetCh)
		m.svc.startCh = nil
		m.svc.startMu.Unlock()
		// Tell the GC to destroy the lobby before we disconnect.
		m.svc.dota.AbandonLobby()
	}
	m.cancel()
	m.svc = nil
	m.mu.Unlock()

	// Give the old TCP connection time to close before opening a new one.
	// Not strictly required, but avoids Steam seeing two simultaneous logins.
	time.Sleep(2 * time.Second)

	svc := New(m.gate)
	m.mu.Lock()
	if svc != nil {
		m.launch(svc)
		log.Println("[bot] restarted successfully")
	} else {
		m.cancel = func() {}
		log.Println("[bot] restart skipped — credentials not configured")
	}
	m.mu.Unlock()
}

// CreateLobbyAndInvite forwards to the current Service.
// No-ops if the bot is offline (e.g. mid-restart).
func (m *Manager) CreateLobbyAndInvite(players []db.Player, gameMode string) {
	m.mu.Lock()
	svc := m.svc
	m.mu.Unlock()
	if svc != nil {
		svc.CreateLobbyAndInvite(players, gameMode)
	}
}
