package control_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
)

// A bot on a stepped clock stands still till the test steps it, then ticks as
// many times as it is stepped — its level loaded on the virtual time, not the wall's.
func TestControllerOnSteppedClock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		b := fakeserver.JoinStepped(t, s, "Tester")
		time.Sleep(50 * time.Millisecond) // real time passes: no tick comes
		if n := b.Ctl.State().Ticks; n != 0 {
			t.Fatalf("%d ticks on a held clock", n)
		}
		if err := b.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		before := b.Ctl.State().Ticks
		b.Step(100)
		if got := b.Ctl.State().Ticks - before; got != 100 {
			t.Errorf("%d ticks in 100 steps", got)
		}
		// a wait of the controller's runs on the same time
		done := make(chan error, 1)
		go func() { done <- b.Ctl.WaitTicks(t.Context(), 30) }()
		b.Step(40)
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("WaitTicks(30) did not return after 40 ticks")
		}
		_ = control.TickInterval
	})
}
