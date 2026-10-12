package simworld

import (
	"math"

	"github.com/mj41/go-mc26/level/block"
)

// clipBlocks is Level.clip with the blocks' collision shapes and no fluid: how far along the segment from a
// to b (0 to 1) the first shape is met, and whether one is. The blocks the segment crosses are walked in
// order (a voxel traversal), each one's boxes tried. As the game's BlockGetter.traverseBlocks, the segment
// is walked from a hair (1e-7 of it) past its start to a hair before its end: a ray that ends on a block's
// face does not meet it.
func (g *Game) clipBlocks(a, b [3]float64) (float64, bool) {
	const hair = 1e-7
	a, b = [3]float64{a[0] + (b[0]-a[0])*hair, a[1] + (b[1]-a[1])*hair, a[2] + (b[2]-a[2])*hair},
		[3]float64{b[0] + (a[0]-b[0])*hair, b[1] + (a[1]-b[1])*hair, b[2] + (a[2]-b[2])*hair}
	var d [3]float64
	var cell, step [3]int
	var tMax, tDelta [3]float64
	for i := 0; i < 3; i++ {
		d[i] = b[i] - a[i]
		cell[i] = int(math.Floor(a[i]))
		switch {
		case d[i] > 0:
			step[i] = 1
			tMax[i] = (float64(cell[i]+1) - a[i]) / d[i]
			tDelta[i] = 1 / d[i]
		case d[i] < 0:
			step[i] = -1
			tMax[i] = (float64(cell[i]) - a[i]) / d[i]
			tDelta[i] = -1 / d[i]
		default:
			tMax[i], tDelta[i] = math.Inf(1), math.Inf(1)
		}
	}
	best := math.Inf(1)
	for n := 0; n < 4096; n++ {
		for _, c := range block.CollisionShape(g.S.World.At(cell[0], cell[1], cell[2])) {
			lo := [3]float64{float64(cell[0]) + c.MinX, float64(cell[1]) + c.MinY, float64(cell[2]) + c.MinZ}
			hi := [3]float64{float64(cell[0]) + c.MaxX, float64(cell[1]) + c.MaxY, float64(cell[2]) + c.MaxZ}
			if t, ok := rayBox(a, d, lo, hi); ok && t < best {
				best = t
			}
		}
		next := min(tMax[0], tMax[1], tMax[2])
		if best <= next || next > 1 {
			break // (no box of a later block is met sooner)
		}
		for i := 0; i < 3; i++ {
			if tMax[i] == next {
				cell[i] += step[i]
				tMax[i] += tDelta[i]
				break
			}
		}
	}
	if best <= 1 {
		return best, true
	}
	return 1, false
}

// rayBox is where, along a + t·d for t in 0 to 1, the ray enters the box (0 when it starts in it).
func rayBox(a, d, lo, hi [3]float64) (float64, bool) {
	t0, t1 := 0.0, 1.0
	for i := 0; i < 3; i++ {
		if d[i] == 0 {
			if a[i] < lo[i] || a[i] > hi[i] {
				return 0, false
			}
			continue
		}
		u, v := (lo[i]-a[i])/d[i], (hi[i]-a[i])/d[i]
		if u > v {
			u, v = v, u
		}
		t0, t1 = max(t0, u), min(t1, v)
		if t0 > t1 {
			return 0, false
		}
	}
	return t0, true
}

// sees is Sensing.hasLineOfSight: no block's collision shape between the two points (eyes to eyes).
func (g *Game) sees(a, b [3]float64) bool {
	_, hit := g.clipBlocks(a, b)
	return !hit
}

// seenPercent is ServerExplosion.getSeenPercent: of the points of a grid over the box (a step of one over twice
// its size and one), the share from which nothing stands between the point and the blast's centre.
func (g *Game) seenPercent(centre, lo, hi [3]float64) float64 {
	dx := 1 / ((hi[0]-lo[0])*2 + 1)
	dy := 1 / ((hi[1]-lo[1])*2 + 1)
	dz := 1 / ((hi[2]-lo[2])*2 + 1)
	ox := (1 - math.Floor(1/dx)*dx) / 2
	oz := (1 - math.Floor(1/dz)*dz) / 2
	seen, all := 0, 0
	for k := 0.0; k <= 1; k += dx {
		for l := 0.0; l <= 1; l += dy {
			for m := 0.0; m <= 1; m += dz {
				p := [3]float64{lo[0] + k*(hi[0]-lo[0]) + ox, lo[1] + l*(hi[1]-lo[1]), lo[2] + m*(hi[2]-lo[2]) + oz}
				if g.sees(p, centre) {
					seen++
				}
				all++
			}
		}
	}
	if all == 0 {
		return 0
	}
	return float64(seen) / float64(all)
}
