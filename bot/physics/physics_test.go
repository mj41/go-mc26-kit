package physics

import (
	"math"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// testWorld is a flat world (grass at y -61, air above) with blocks set on top.
type testWorld map[world.BlockPos]block.StateID

func (w testWorld) BlockAt(p world.BlockPos) (block.StateID, bool) {
	if s, ok := w[p]; ok {
		return s, true
	}
	if p.Y <= -61 {
		return block.ToStateID[block.GrassBlock{}], true
	}
	return block.ToStateID[block.Air{}], true
}

func (w testWorld) fill(x0, y0, z0, x1, y1, z1 int, s block.StateID) {
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				w[world.BlockPos{X: x, Y: y, Z: z}] = s
			}
		}
	}
}

func run(p *Player, w Blocks, keys Keys, ticks int) {
	for range ticks {
		p.Tick(w, nil, keys, DefaultAttributes)
	}
}

func TestWalk(t *testing.T) {
	w := testWorld{}
	p := NewPlayer(Vec3{20.5, -60, 30.5})
	p.Yaw = -90
	run(p, w, Keys{}, 5)
	if !p.OnGround || p.Pos.Y != -60 {
		t.Fatalf("standing: %+v", p)
	}
	run(p, w, Keys{Forward: true}, 20)
	run(p, w, Keys{}, 20)
	if math.Abs(p.Pos.X-(20.5+4.3134)) > 0.001 {
		t.Errorf("walked to x %.4f, the server measured 24.8134", p.Pos.X)
	}
}

func TestSneakEdge(t *testing.T) {
	w := testWorld{}
	w.fill(24, -60, 42, 26, -58, 44, block.ToStateID[block.Stone{}])
	p := NewPlayer(Vec3{25.5, -57, 43.5})
	p.Yaw = -90
	run(p, w, Keys{}, 10)
	if !p.OnGround {
		t.Fatalf("not on the pillar: %+v", p.Pos)
	}
	run(p, w, Keys{Forward: true, Shift: true}, 40)
	if p.Pos.Y != -57 || p.Pos.X > 27.31 {
		t.Errorf("sneaking ended at %+v, want on the pillar at most to x 27.3", p.Pos)
	}
}

// TestCurrent: standing in a channel of flowing water (a source at x 0, the
// level dropping toward +x) the player is pushed down the flow; in still
// water it stays.
func TestCurrent(t *testing.T) {
	stone := block.ToStateID[block.Stone{}]
	w := testWorld{}
	w.fill(-1, -60, -1, 9, -60, -1, stone) // the channel's walls
	w.fill(-1, -60, 1, 9, -60, 1, stone)
	w[world.BlockPos{X: 0, Y: -60, Z: 0}] = block.ToStateID[block.Water{Level: 0}]
	for x := 1; x <= 7; x++ {
		w[world.BlockPos{X: x, Y: -60, Z: 0}] = block.ToStateID[block.Water{Level: block.Integer(x)}]
	}
	p := NewPlayer(Vec3{3.5, -60, 0.5})
	run(p, w, Keys{}, 40)
	if p.Pos.X < 3.6 {
		t.Errorf("40 ticks in the current moved the player to x %.4f, want down the flow past 3.6", p.Pos.X)
	}
	still := testWorld{}
	still.fill(-1, -60, -1, 9, -60, -1, stone)
	still.fill(-1, -60, 1, 9, -60, 1, stone)
	still.fill(0, -60, 0, 7, -60, 0, block.ToStateID[block.Water{Level: 0}])
	q := NewPlayer(Vec3{3.5, -60, 0.5})
	run(q, still, Keys{}, 40)
	if math.Abs(q.Pos.X-3.5) > 1e-6 {
		t.Errorf("still water moved the player to x %.4f", q.Pos.X)
	}
}
