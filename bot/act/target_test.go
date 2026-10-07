package act

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// TestTarget: the block of the action in progress, and none when idle or on
// something without one.
func TestTarget(t *testing.T) {
	p := world.BlockPos{X: 1, Y: 2, Z: 3}
	for _, c := range []struct {
		job  job
		what string
		ok   bool
	}{
		{nil, "", false},
		{&dig{pos: p}, "dig", true},
		{&use{target: p, place: true}, "place", true},
		{&use{target: p}, "use", true},
		{&eat{}, "", false},
	} {
		h := &Hands{job: c.job}
		pos, what, ok := h.Target()
		if ok != c.ok || what != c.what || ok && pos != p {
			t.Errorf("%T: Target() = %v %q %v, want %q %v", c.job, pos, what, ok, c.what, c.ok)
		}
	}
}
