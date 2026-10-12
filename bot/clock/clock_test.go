package clock_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/protocol/play"
)

// TestGameTime: the game time is what the server said, run on between its
// updates but never past the next one (a server lagging behind the wall's
// time is not counted ahead of it).
func TestGameTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		b := fakeserver.Join(t, s, "Tester")
		if _, ok := b.Clock.GameTime(); ok {
			t.Fatal("a game time before the server said one")
		}
		if err := s.Send(play.SetTime{GameTime: 1000}); err != nil {
			t.Fatal(err)
		}
		gameTime := func() int64 {
			t.Helper()
			for t0 := time.Now(); time.Since(t0) < 2*time.Second; time.Sleep(5 * time.Millisecond) {
				if g, ok := b.Clock.GameTime(); ok {
					return g
				}
			}
			t.Fatal("the game time never came")
			return 0
		}
		if g := gameTime(); g < 1000 || g > 1019 {
			t.Fatalf("game time %d just after the server said 1000", g)
		}
		time.Sleep(1200 * time.Millisecond) // the server's next update late: lagging
		if g := gameTime(); g != 1019 {
			t.Fatalf("game time %d a second and more after 1000, the server silent: want 1019, no further", g)
		}
		if err := s.Send(play.SetTime{GameTime: 1020}); err != nil {
			t.Fatal(err)
		}
		for t0 := time.Now(); ; time.Sleep(5 * time.Millisecond) {
			if g := gameTime(); g >= 1020 {
				break
			}
			if time.Since(t0) > 2*time.Second {
				t.Fatal("the server's 1020 never counted")
			}
		}
	})
}
