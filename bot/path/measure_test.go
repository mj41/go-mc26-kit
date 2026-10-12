package path

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// enclosed is a stone wall 3 high round x0..x1, z0..z1 so that the search has a finite world.
func enclosed(w testWorld, x0, z0, x1, z1 int) {
	stone := block.ToStateID[block.Stone{}]
	w.fill(x0, -60, z0, x1, -58, z0, stone)
	w.fill(x0, -60, z1, x1, -58, z1, stone)
	w.fill(x0, -60, z0, x0, -58, z1, stone)
	w.fill(x1, -60, z0, x1, -58, z1, stone)
}

type counted struct {
	w testWorld
	n *atomic.Int64
}

func (c counted) BlockAt(p world.BlockPos) (block.StateID, bool) {
	c.n.Add(1)
	return c.w.BlockAt(p)
}

// TestMeasureFailedSearch (a measure, not a test: MEASURE=1) is the cost of a search that finds no way — every
// place reachable searched, in a field, a lake, an ocean, a roofed flooded cave — as SEARCH.md "Cost" tells it:
// the places and labels, the world's reads (the work), the time (which varies with the machine's load).
func TestMeasureFailedSearch(t *testing.T) {
	if os.Getenv("MEASURE") == "" {
		t.Skip("a measure: MEASURE=1 go test -run MeasureFailedSearch -v")
	}
	water := stateNamed(t, "minecraft:water[level=0]")
	stone := block.ToStateID[block.Stone{}]
	sealed := func(w testWorld, x, z int) world.BlockPos { // a goal shut in a stone box
		w.fill(x-2, -60, z-2, x+2, -58, z+2, stone)
		w.fill(x-1, -60, z-1, x+1, -59, z+1, block.ToStateID[block.Air{}])
		return world.BlockPos{X: x, Y: -60, Z: z}
	}
	type scenario struct {
		name  string
		build func() (testWorld, world.BlockPos)
	}
	for _, sc := range []scenario{
		{"dry field 100x100", func() (testWorld, world.BlockPos) {
			w := testWorld{}
			enclosed(w, -10, -50, 100, 50)
			return w, sealed(w, 90, 0)
		}},
		{"lake 60x60, 6 deep", func() (testWorld, world.BlockPos) {
			w := testWorld{}
			enclosed(w, -10, -40, 80, 40)
			w.fill(0, -60, -30, 59, -55, 30, water)
			return w, sealed(w, 72, 0)
		}},
		{"ocean 100x100, 8 deep", func() (testWorld, world.BlockPos) {
			w := testWorld{}
			enclosed(w, -10, -60, 120, 60)
			w.fill(0, -60, -50, 99, -53, 50, water)
			w.fill(0, -60, -50, 99, -53, 50, water)
			return w, sealed(w, 112, 0)
		}},
		{"flooded cave 60x40, 3 deep, roofed", func() (testWorld, world.BlockPos) {
			w := testWorld{}
			enclosed(w, -10, -30, 80, 30)
			w.fill(0, -60, -25, 59, -58, 25, stone)
			w.fill(0, -60, -20, 59, -59, 20, water)
			return w, sealed(w, 72, 0)
		}},
	} {
		w, goal := sc.build()
		var reads atomic.Int64
		g := &Graph{World: counted{w, &reads}, Tags: ladderTags{}}
		start, ok := g.Start(-4.5, -60, 0.5)
		if !ok {
			t.Fatalf("%s: no start", sc.name)
		}
		t0 := time.Now()
		_, err := g.Find(start, goal, 100000)
		var np *NoPathError
		if !errors.As(err, &np) {
			t.Fatalf("%s: %v", sc.name, err)
		}
		t.Logf("%-38s %7.2fs searched %6d labels %7d reads %5.1fM limit %v air %v", sc.name, time.Since(t0).Seconds(), np.Searched, np.Labels, float64(reads.Load())/1e6, np.Limit, np.Air)
	}
}
