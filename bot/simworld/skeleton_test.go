package simworld

import (
	"math"
	"testing"
)

// A skeleton ten blocks off a player in the open draws its bow and shoots: the arrow flies (an entity the bot
// is told of), hits for its speed times its base damage, rounded up (3 to 5), and pushes the player. It keeps
// its distance meanwhile, strafing: it neither comes to the player nor stands still.
func TestSkeletonShootsThePlayerInTheOpen(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3})
		if err := g.SummonNear("minecraft:skeleton", 10, 0); err != nil {
			t.Fatal(err)
		}
		m := g.mobs[0]
		sawArrow := false
		if !g.Until(100, func() bool {
			for _, a := range g.arrows {
				if e, ok := g.B.Entities.Get(a.id); ok && e.Type == "minecraft:arrow" {
					sawArrow = true
				}
			}
			return g.Health < 20
		}) {
			t.Fatalf("not hit in 5 s (%d arrows): %s", g.Arrows(), g.MobsAt())
		}
		if !sawArrow {
			t.Errorf("the bot was never told of an arrow")
		}
		// the first shot: the bow drawn 20 ticks, the arrow 9 blocks at about 1.6 a tick
		if g.Tick < 25 || g.Tick > 45 {
			t.Errorf("hit at tick %d, want about 30", g.Tick)
		}
		if d := 20 - g.Health; d < 3 || d > 5 {
			t.Errorf("an arrow did %.1f, want 3 to 5", d)
		}
		p0 := g.B.Player.Position()
		g.Step(4)
		if p := g.B.Player.Position(); p.X > p0.X-0.2 {
			t.Errorf("the player was not pushed along the arrow's flight (x %.2f to %.2f)", p0.X, p.X)
		}
		z0 := m.z
		hits := 1
		health := g.Health
		for range 200 { // ten seconds: more shots, one every three seconds or so (40 ticks between, 20 drawn)
			g.Step(1)
			if g.Health < health {
				hits++
			}
			health = g.Health
		}
		if hits < 3 || hits > 5 {
			t.Errorf("%d hits in 10.5 s, want one about every 3 s", hits)
		}
		if d := math.Hypot(m.x-g.pos[0], m.z-g.pos[2]); d < 6 || d > 15 {
			t.Errorf("the skeleton is %.1f blocks off, want its distance kept", d)
		}
		if math.Abs(m.z-z0) < 0.5 && math.Abs(m.x-g.pos[0]-10) < 0.5 {
			t.Errorf("the skeleton stood still")
		}
		t.Logf("%d hits, health %.1f; the skeleton at %.1f %.1f", hits, g.Health, m.x-g.pos[0], m.z-g.pos[2])
	})
}

// Behind a wall the skeleton does not see the player: it does not shoot, nothing hits.
func TestSkeletonBehindAWallDoesNotHit(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3, Setup: []string{"fill 4 64 -4 4 67 4 minecraft:stone"}})
		if err := g.SummonNear("minecraft:skeleton", 10, 0); err != nil {
			t.Fatal(err)
		}
		g.Step(200)
		if g.Health != 20 || g.Arrows() != 0 {
			t.Errorf("behind the wall: health %.1f, %d arrows; want no shot", g.Health, g.Arrows())
		}
	})
}

// An arrow that meets a block stops in it and lies there.
func TestArrowStopsInABlock(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3, Setup: []string{"fill 3 64 -2 3 66 2 minecraft:stone"}})
		src := &mob{id: 999, kind: "minecraft:skeleton", k: mobKinds["minecraft:skeleton"], x: g.pos[0] + 8, y: g.pos[1], z: g.pos[2]}
		if err := g.shoot(src, 1); err != nil {
			t.Fatal(err)
		}
		g.Step(20)
		if len(g.arrows) != 1 || !g.arrows[0].inGround {
			t.Fatalf("the arrow: %+v", g.arrows)
		}
		if a := g.arrows[0]; a.x < 3.9 || a.x > 4.1 {
			t.Errorf("it lies at x %.2f, want at the wall's face (4)", a.x)
		}
		if g.Health != 20 {
			t.Errorf("the player behind the wall was hit")
		}
	})
}
