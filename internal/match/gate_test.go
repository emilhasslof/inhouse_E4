package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
