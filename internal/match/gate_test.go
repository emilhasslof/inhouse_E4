package match

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// callbackRecorder collects match IDs delivered to the gate's onFinalize and
// onAbandon callbacks. The callbacks fire in goroutines, so reads must wait for
// them to land — tests use waitFor to poll briefly before asserting.
type callbackRecorder struct {
	mu        sync.Mutex
	finalized []string
	abandoned []string
}

func (r *callbackRecorder) onFinalize(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finalized = append(r.finalized, id)
}

func (r *callbackRecorder) onAbandon(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.abandoned = append(r.abandoned, id)
}

func (r *callbackRecorder) snapshot() (finalized, abandoned []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.finalized...), append([]string(nil), r.abandoned...)
}

// waitFor polls cond every 5 ms until it returns true or 200 ms elapses.
// Used to give the gate's callback goroutines time to land before asserting.
func waitFor(cond func() bool) bool {
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func TestLockedMatchID(t *testing.T) {
	g := New(2)
	assert.Equal(t, "", g.LockedMatchID(), "closed")

	g.Open()
	assert.Equal(t, "", g.LockedMatchID(), "open/unconfirmed")

	g.Accept("match-xyz", "player-a")
	assert.Equal(t, "", g.LockedMatchID(), "one vote — still unconfirmed")

	g.Accept("match-xyz", "player-b")
	assert.Equal(t, "match-xyz", g.LockedMatchID(), "locked after threshold")

	g.Close()
	assert.Equal(t, "", g.LockedMatchID(), "closed again")
}

// TestClose_RoutesToOnFinalizeAfterMarkCompleted is the regression test for
// the production incident where a completed match was deleted by the abandon
// callback. After MarkCompleted, every gate-close path must fire onFinalize
// (which preserves the match) instead of onAbandon (which archives it).
func TestClose_RoutesToOnFinalizeAfterMarkCompleted(t *testing.T) {
	t.Run("Close after MarkCompleted fires onFinalize", func(t *testing.T) {
		g := New(1)
		var rec callbackRecorder
		g.SetOnFinalize(rec.onFinalize)
		g.SetOnAbandon(rec.onAbandon)

		g.Open()
		assert.True(t, g.Accept("match-A", "player-1"))
		g.MarkCompleted()
		g.Close()

		assert.True(t, waitFor(func() bool {
			f, _ := rec.snapshot()
			return len(f) == 1
		}))
		f, a := rec.snapshot()
		assert.Equal(t, []string{"match-A"}, f)
		assert.Empty(t, a, "abandon must not fire for a completed match")
	})

	t.Run("Open while still locked-and-completed fires onFinalize", func(t *testing.T) {
		g := New(1)
		var rec callbackRecorder
		g.SetOnFinalize(rec.onFinalize)
		g.SetOnAbandon(rec.onAbandon)

		g.Open()
		assert.True(t, g.Accept("match-B", "player-1"))
		g.MarkCompleted()
		// Simulate a new lobby being opened before the match's gate closes —
		// this is the exact scenario from the production logs.
		g.Open()

		assert.True(t, waitFor(func() bool {
			f, _ := rec.snapshot()
			return len(f) == 1
		}))
		f, a := rec.snapshot()
		assert.Equal(t, []string{"match-B"}, f)
		assert.Empty(t, a, "abandon must not fire for a completed match")
	})

	t.Run("Close without MarkCompleted fires onAbandon", func(t *testing.T) {
		g := New(1)
		var rec callbackRecorder
		g.SetOnFinalize(rec.onFinalize)
		g.SetOnAbandon(rec.onAbandon)

		g.Open()
		assert.True(t, g.Accept("match-C", "player-1"))
		g.Close()

		assert.True(t, waitFor(func() bool {
			_, a := rec.snapshot()
			return len(a) == 1
		}))
		f, a := rec.snapshot()
		assert.Equal(t, []string{"match-C"}, a)
		assert.Empty(t, f, "finalize must not fire when no POST_GAME ever arrived")
	})
}
