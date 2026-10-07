package physics

import (
	"testing"

	"github.com/mj41/go-mc26/level/block"
)

// A step up one block under a ceiling three over the feet: the jump is cut
// at the ceiling but still lands on the step, as in the game.
func TestStepUnderCeiling(t *testing.T) {
	stone := block.ToStateID[block.Stone{}]
	w := testWorld{}
	w.fill(1, -60, -1, 20, -60, 1, stone)  // the step
	w.fill(-1, -57, -1, 20, -57, 1, stone) // the ceiling: feet -60, ceiling at -57
	p := NewPlayer(Vec3{0.5, -60, 0.5})
	p.Yaw = -90 // east
	run(p, w, Keys{}, 5)
	for i := 0; i < 12; i++ {
		run(p, w, Keys{Forward: true, Jump: p.OnGround && p.Pos.Y < -59.5}, 1)
	}
	run(p, w, Keys{}, 20)
	if p.Pos.Y != -59 || p.Pos.X < 1.2 {
		t.Errorf("at %.3f %.3f: not up on the step", p.Pos.X, p.Pos.Y)
	}
}

// The same from against the step's face, sprinting, as a walker does.
func TestStepUnderCeilingFromFace(t *testing.T) {
	stone := block.ToStateID[block.Stone{}]
	for _, sprint := range []bool{false, true} {
		w := testWorld{}
		w.fill(1, -60, -1, 20, -60, 1, stone)
		w.fill(-1, -57, -1, 20, -57, 1, stone)
		p := NewPlayer(Vec3{0.7, -60, 0.49})
		p.Yaw = -90
		run(p, w, Keys{}, 5)
		for i := 0; i < 30 && p.Pos.Y < -59.01; i++ {
			run(p, w, Keys{Forward: true, Sprint: sprint, Jump: p.OnGround}, 1)
		}
		run(p, w, Keys{Forward: true}, 5)
		if p.Pos.Y != -59 {
			t.Errorf("sprint %v: at %.3f %.3f: not up on the step", sprint, p.Pos.X, p.Pos.Y)
		}
	}
}
