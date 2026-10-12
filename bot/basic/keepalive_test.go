package basic

import (
	"testing"
	"time"
)

// A player's keep-alive deadline: the package's timeout by default, its own when set, none when negative (a game on a
// stepped clock has no wall time to keep).
func TestKeepAliveDeadline(t *testing.T) {
	now := time.Unix(1000, 0)
	p := &Player{}
	if got := p.keepAliveDeadline(now); !got.Equal(now.Add(KeepAliveTimeout)) {
		t.Errorf("default: %v", got)
	}
	p.KeepAliveTimeout = 3 * time.Second
	if got := p.keepAliveDeadline(now); !got.Equal(now.Add(3 * time.Second)) {
		t.Errorf("own: %v", got)
	}
	p.KeepAliveTimeout = -1
	if got := p.keepAliveDeadline(now); !got.IsZero() {
		t.Errorf("none: %v", got)
	}
}
