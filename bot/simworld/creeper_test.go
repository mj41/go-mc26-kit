package simworld

import (
	"math"
	"testing"

	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
)

// A creeper put six blocks off walks to the player, is lit within three blocks (the bot told its fuse's
// direction in the entity's data) and stands; thirty ticks on it blows up: gone, the ground round it broken,
// the player hurt by how near it stood and pushed away.
func TestCreeperLightsNearAndBlows(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3})
		if err := g.SummonNear("minecraft:creeper", 6, 0); err != nil {
			t.Fatal(err)
		}
		m := g.mobs[0]
		if !g.Until(200, func() bool { _, lit := g.Fuse(m.id); return lit }) {
			t.Fatalf("not lit in 10 s: %s", g.MobsAt())
		}
		litAt := g.Tick
		at := [3]float64{m.x, m.y, m.z}
		if d := math.Sqrt(sq(g.pos[0]-m.x) + sq(g.pos[1]-m.y) + sq(g.pos[2]-m.z)); d >= 3 || d < 2.5 {
			t.Errorf("lit %.2f blocks off, want just within 3", d)
		}
		g.Step(2)
		if e, ok := g.B.Entities.Get(m.id); !ok || e.Data[entitydata.CreeperSwellDir].Value == nil || *e.Data[entitydata.CreeperSwellDir].Value.(*pk.VarInt) != 1 {
			t.Errorf("the bot was not told the fuse is lit: %+v", e.Data)
		}
		before := g.B.Player.Position()
		if !g.Until(40, func() bool { return g.Blasts() == 1 }) {
			t.Fatalf("no blast 40 ticks after it was lit")
		}
		if fuse := g.Tick - litAt; fuse < 30 || fuse > 32 {
			t.Errorf("it blew %d ticks after it was lit, want 30 (and the tick it is told)", fuse)
		}
		if g.Mobs() != 0 {
			t.Errorf("%d mobs after the blast, want the creeper gone", g.Mobs())
		}
		if g.S.World.At(int(math.Floor(at[0])), 63, int(math.Floor(at[2]))) != 0 {
			t.Errorf("the ground under the creeper is %s, want broken", block.StateList[g.S.World.At(int(math.Floor(at[0])), 63, int(math.Floor(at[2])))].ID())
		}
		// at about 2.9 blocks, seen whole: i = 1 − 2.9/6 ≈ 0.52, (i² + i)/2 × 7 × 6 + 1 ≈ 17.5
		if g.Health > 4 || g.Health < 1 {
			t.Errorf("health %.1f after the blast at about 3 blocks, want about 2.5", g.Health)
		}
		g.Step(5)
		after := g.B.Player.Position()
		if away := math.Hypot(after.X-at[0], after.Z-at[2]) - math.Hypot(before.X-at[0], before.Z-at[2]); away < 0.3 {
			t.Errorf("the player was pushed %.2f blocks away in 5 ticks, want more", away)
		}
	})
}

// A creeper lit puts its fuse out when the player goes beyond seven blocks: the fuse burns back to nothing,
// no blast, and it walks after the player again.
func TestCreeperFuseGoesOutWhenThePlayerLeaves(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3})
		if err := g.SummonNear("minecraft:creeper", 2, 0); err != nil {
			t.Fatal(err)
		}
		m := g.mobs[0]
		if !g.Until(20, func() bool { _, lit := g.Fuse(m.id); return lit }) {
			t.Fatalf("not lit at two blocks")
		}
		g.Step(10)
		g.Teleport(g.pos[0]-10, g.pos[1], g.pos[2])
		g.Step(3)
		if swell, lit := g.Fuse(m.id); lit || swell == 0 {
			t.Errorf("the fuse after the player left: %d, lit %v; want going out", swell, lit)
		}
		x0 := m.x
		if !g.Until(30, func() bool { swell, _ := g.Fuse(m.id); return swell == 0 }) {
			t.Errorf("the fuse did not burn back down")
		}
		g.Step(40)
		if g.Blasts() != 0 || g.Mobs() != 1 {
			t.Errorf("%d blasts, %d mobs: want none, the creeper", g.Blasts(), g.Mobs())
		}
		if m.x > x0-1 {
			t.Errorf("the creeper did not walk after the player (x %.1f to %.1f)", x0, m.x)
		}
	})
}

// A blast hurts by distance and by how much of the player it sees: (i² + i)/2 × 7 × 6 + 1 for a radius of 3,
// i one less the distance over 6, times the share seen; nothing beyond 6 blocks; less behind a wall.
func TestBlastHurtsByDistance(t *testing.T) {
	Run(t, func(t *testing.T) {
		src := &mob{id: 999, kind: "minecraft:creeper"}
		hurt := func(dx float64, setup ...string) float32 {
			g := Start(t, Options{View: 3, Setup: setup})
			if err := g.explode([3]float64{g.pos[0] + dx, g.pos[1], g.pos[2]}, 3, src); err != nil {
				t.Fatal(err)
			}
			return 20 - g.Health
		}
		formula := func(d float64) float32 { i := 1 - d/6; return float32((i*i+i)/2*7*6 + 1) }
		near, mid, far := hurt(3), hurt(4), hurt(6.5)
		if math.Abs(float64(near-formula(3))) > 0.5 || math.Abs(float64(mid-formula(4))) > 0.5 {
			t.Errorf("damage %.2f at 3 blocks and %.2f at 4, want %.2f and %.2f", near, mid, formula(3), formula(4))
		}
		if far != 0 {
			t.Errorf("damage %.2f at 6.5 blocks, want none", far)
		}
		walled := hurt(4, "fill 2 64 -2 2 66 2 minecraft:stone")
		if walled >= mid {
			t.Errorf("behind a wall %.2f, in the open %.2f: want less behind it", walled, mid)
		}
		t.Logf("damage at 3: %.2f, at 4: %.2f, behind a wall at 4: %.2f, at 6.5: %.2f", near, mid, walled, far)
	})
}

// The blocks a blast breaks drop as their loot tables say for a blast of radius 3: a block's drop survives
// one time in three — the ground's grass and dirt give dirt (the stone under them cobblestone) for about a
// third of the blocks broken. (In stone a creeper's blast breaks little: each step of a ray in a block costs
// its resistance again, stone's 6.)
func TestBlastDropsAsTheLootTablesSay(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 4})
		solid := func() int {
			n := 0
			for x := 2; x <= 36; x++ {
				for y := 55; y <= 64; y++ {
					for z := -7; z <= 7; z++ {
						if g.S.World.At(x, y, z) != 0 {
							n++
						}
					}
				}
			}
			return n
		}
		src := &mob{id: 999, kind: "minecraft:creeper"}
		before := solid()
		for _, x := range []float64{10.5, 20.5, 30.5} { // (three blasts, ten apart: one does not burn the last's drops)
			if err := g.explode([3]float64{x, 64, 0.5}, 3, src); err != nil {
				t.Fatal(err)
			}
		}
		broken := before - solid()
		total := 0
		for _, it := range g.items {
			if it.what.item != "minecraft:dirt" && it.what.item != "minecraft:cobblestone" {
				t.Errorf("a blast in the ground dropped %s", it.what.item)
			}
			total += it.what.n
		}
		if broken < 60 {
			t.Fatalf("three blasts broke %d blocks", broken)
		}
		if share := float64(total) / float64(broken); share < 0.2 || share > 0.47 {
			t.Errorf("%d items of %d blocks broken (%.2f), want about a third", total, broken, share)
		}
		t.Logf("%d blocks broken, %d items dropped", broken, total)
		if g.Health != 20 {
			t.Errorf("the player 10 blocks off was hurt: %.1f", g.Health)
		}
	})
}
