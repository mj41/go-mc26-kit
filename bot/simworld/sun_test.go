package simworld

import (
	"testing"

	"github.com/mj41/go-mc26/data/entitydata"
	pk "github.com/mj41/go-mc26/net/packet"
)

// pen is a ring of stone two high round 20, 0, open above: a zombie in it stays where the sun is (it cannot
// walk off to the player); roof puts a stone roof, a block thick, over it.
func pen(roof bool) []string {
	lines := []string{"fill 18 64 -2 22 65 2 minecraft:stone", "fill 19 64 -1 21 65 1 minecraft:air"}
	if roof {
		lines = append(lines, "fill 18 66 -2 22 66 2 minecraft:stone")
	}
	return lines
}

// At noon under the open sky a zombie catches fire (one tick in 25 at full light), the bot told it burns, and
// it burns on till it dies — a point a second, its 20 health.
func TestZombieBurnsAtNoon(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3, Day: &Day{Time: 6000}, Setup: pen(false)})
		if err := g.SummonAdultNear("minecraft:zombie", 20, 0); err != nil {
			t.Fatal(err)
		}
		m := g.mobs[0]
		if !g.Until(200, func() bool { return g.Burning(m.id) }) {
			t.Fatalf("not on fire in 10 s at noon")
		}
		g.Step(2)
		if e, ok := g.B.Entities.Get(m.id); !ok || e.Data[entitydata.EntitySharedFlags].Value == nil || *e.Data[entitydata.EntitySharedFlags].Value.(*pk.Byte)&1 == 0 {
			t.Errorf("the bot was not told it burns")
		}
		lit := g.Tick
		if !g.Until(20*30, func() bool { return m.dying > 0 }) {
			h, _ := g.MobHealth(m.id)
			t.Fatalf("alive 30 s after it caught fire: health %.0f", h)
		}
		// a point a second, and one more when the sun sets it again just as a second's point came (its fire
		// back at 8 s, a point at once — as the game's): 20 health in 12 to 20 s
		if s := (g.Tick - lit) / 20; s < 12 || s > 21 {
			t.Errorf("it died %d s after it caught fire, want 12 to 20", s)
		}
	})
}

// Under a roof a block thick it does not burn at noon; at night in the open it does not either.
func TestZombieDoesNotBurnInShadeOrAtNight(t *testing.T) {
	Run(t, func(t *testing.T) {
		for _, c := range []struct {
			what string
			time int64
			roof bool
		}{{"in the shade at noon", 6000, true}, {"in the open at night", 18000, false}} {
			g := Start(t, Options{View: 3, Day: &Day{Time: c.time}, Setup: pen(c.roof)})
			if err := g.SummonAdultNear("minecraft:zombie", 20, 0); err != nil {
				t.Fatal(err)
			}
			m := g.mobs[0]
			m.y = 64 // (in the pen, under its roof: a summon puts it on the top block)
			if g.Until(400, func() bool { return g.Burning(m.id) }) {
				t.Errorf("%s: it caught fire", c.what)
			}
		}
	})
}

// Water puts a burning zombie out, and it does not catch fire standing in it.
func TestZombieInWaterDoesNotBurn(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3, Day: &Day{Time: 6000}, Setup: append(pen(false), "fill 19 64 -1 21 65 1 minecraft:water")})
		if err := g.SummonAdultNear("minecraft:zombie", 20, 0); err != nil {
			t.Fatal(err)
		}
		m := g.mobs[0]
		m.fire = 100
		g.Step(2)
		if g.Burning(m.id) {
			t.Errorf("still burning in water")
		}
		if g.Until(300, func() bool { return g.Burning(m.id) }) {
			t.Errorf("caught fire in water")
		}
	})
}

// A baby zombie walks half again as fast as an adult (the game's baby speed modifier) and, half the
// size, goes through a gap one block high that stops an adult.
func TestBabyZombieIsFasterAndFitsAGap(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3})
		if err := g.SummonBabyNear("minecraft:zombie", 30, -4); err != nil {
			t.Fatal(err)
		}
		if err := g.SummonAdultNear("minecraft:zombie", 30, 4); err != nil {
			t.Fatal(err)
		}
		b, a := g.mobs[0], g.mobs[1]
		g.Step(1)
		if !g.IsBaby(b.id) || g.IsBaby(a.id) {
			t.Fatalf("baby %v, adult %v", g.IsBaby(b.id), g.IsBaby(a.id))
		}
		if e, ok := g.B.Entities.Get(b.id); !ok || e.Data[entitydata.ZombieBaby].Value == nil || !bool(*e.Data[entitydata.ZombieBaby].Value.(*pk.Boolean)) {
			t.Errorf("the bot was not told it is a baby")
		}
		g.Step(40)
		bx, ax := b.x, a.x
		g.Step(20)
		vb, va := (bx-b.x)/20, (ax-a.x)/20
		if va < 0.115 || va >= 0.120 || vb < 1.45*va || vb > 1.55*va {
			t.Errorf("the baby walks %.4f a tick, the adult %.4f: want the adult 0.115–0.120, the baby half again", vb, va)
		}
		// the gap: a wall across the way, one block open at the ground
		gap := []string{"fill 6 64 -3 6 66 3 minecraft:stone", "setblock 6 64 0 minecraft:air"}
		for _, c := range []struct {
			baby bool
			want bool
		}{{true, true}, {false, false}} {
			g := Start(t, Options{View: 3, Setup: gap})
			summon := g.SummonAdultNear
			if c.baby {
				summon = g.SummonBabyNear
			}
			if err := summon("minecraft:zombie", 12, 0); err != nil {
				t.Fatal(err)
			}
			if got := g.Until(300, func() bool { return g.Health < 20 }); got != c.want {
				t.Errorf("baby %v: through the gap to the player %v, want %v (at %s)", c.baby, got, c.want, g.MobsAt())
			}
		}
	})
}
