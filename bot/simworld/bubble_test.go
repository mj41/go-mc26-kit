package simworld

import "testing"

// A game in a bubble joins over memory, steps and settles by the runtime (synctest.Wait).
func TestGameInABubble(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3, Setup: []string{"give Builder minecraft:stone_sword"}})
		if g.S.Dialer == nil {
			t.Fatal("the game is on a loopback socket in a bubble")
		}
		if err := g.SummonNear("minecraft:zombie", 6, 0); err != nil {
			t.Fatal(err)
		}
		if !g.Until(100, func() bool { return g.Health < 20 }) {
			t.Fatalf("the zombie never hit: %s", g.MobsAt())
		}
		hit := g.Tick
		if hit < 30 || hit > 60 {
			t.Errorf("it hit at tick %d, want about 40", hit)
		}
		t.Logf("hit at tick %d", hit)
	})
}
