package simworld

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/control"
)

// stepOff puts the bot on the top block of a lone pillar (feet at 74, ten over the ground) and has it walk off the
// edge toward the pond's side: what the robot's dropOff does after a cliff's last block (examples/robot/drop.go).
func stepOff(t *testing.T, pond bool) *Game {
	setup := []string{"fill 10 64 0 10 73 0 minecraft:stone"}
	if pond {
		setup = append(setup, "fill 11 63 0 12 63 0 minecraft:water") // the grass at y 63 under the flat ground's top
	}
	g := Start(t, Options{View: 3, Setup: setup})
	g.Teleport(10.5, 74, 0.5)
	if !g.Until(40, func() bool { return g.B.Ctl.State().OnGround && g.B.Player.Position().Y > 73.9 }) {
		t.Fatalf("not standing on the pillar: %+v", g.B.Player.Position())
	}
	act.LookAt(g.B.Player, false, 11.5, 74.5, 0.5)
	g.B.Ctl.SetInput(control.Input{Forward: true})
	left := false
	if !g.Until(200, func() bool {
		if !g.B.Ctl.State().OnGround && !left {
			left = true
			g.B.Ctl.SetInput(control.Input{}) // off the edge: the keys let go, the fall carries it a little way
		}
		return left && g.B.Ctl.State().OnGround
	}) {
		t.Fatalf("it did not step off and land: %+v", g.B.Player.Position())
	}
	g.B.Ctl.SetInput(control.Input{})
	g.Step(10)
	t.Logf("landed at %+v health %v", g.B.Player.Position(), g.B.Player.Status().Health)
	return g
}

// Off a lone block into a pond at the cliff's foot: no hurt, whatever the height.
func TestStepOffTenIntoWater(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := stepOff(t, true)
		if h := g.B.Player.Status().Health; h != 20 {
			t.Errorf("health %v after a fall into the pond, want 20", h)
		}
		if y := g.B.Player.Position().Y; y > 64.1 {
			t.Errorf("at y %.2f: it did not come down", y)
		}
	})
}

// Off the same block onto the ground: the blocks fallen less three (10 → 7).
func TestStepOffTenOntoStone(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := stepOff(t, false)
		// 7 hurt, a little of it healed in the ten ticks since (the saturation)
		if h := g.B.Player.Status().Health; h < 13 || h > 14 {
			t.Errorf("health %v after ten blocks, want 13 (7 hurt) and a little healed", h)
		}
	})
}
