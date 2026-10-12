package control

import (
	"sync/atomic"
	"testing"
	"time"
)

// The stepped clock fires its ticker once a period, its timers at their time,
// and waits for each tick to be finished before the next.
func TestSteppedTicker(t *testing.T) {
	clk := NewStepped(time.Unix(1000, 0))
	tk := clk.NewTicker(50 * time.Millisecond)
	defer tk.Stop()
	var ticks, inTick atomic.Int32
	go func() {
		for range tk.C() {
			if inTick.Add(1) != 1 {
				t.Error("a tick began before the last was finished")
			}
			ticks.Add(1)
			inTick.Add(-1)
			tk.Done()
		}
	}()
	late := clk.After(120 * time.Millisecond)
	if err := clk.Step(500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if n := ticks.Load(); n != 10 {
		t.Errorf("%d ticks in 500 ms of 50 ms ticks, want 10", n)
	}
	select {
	case at := <-late:
		if want := time.Unix(1000, 0).Add(120 * time.Millisecond); !at.Equal(want) {
			t.Errorf("the timer fired at %v, want %v", at, want)
		}
	default:
		t.Error("the timer did not fire")
	}
	if got := clk.Now(); !got.Equal(time.Unix(1000, 0).Add(500 * time.Millisecond)) {
		t.Errorf("now %v", got)
	}
}
