package path

import (
	"math"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// Breath: the air a walk through water costs (SEARCH.md, "Breath"). A body whose eyes are in water loses a
// tick of air each tick, from 300 (15 s) and, at none, drowns; out of water it gets four back each tick.

const (
	// FullAir is the air supply in ticks (Entity.getMaxAirSupply: 15 s).
	FullAir = 300
	// eyeHeight is the standing eyes' height over the feet.
	eyeHeight = 1.62
	// swimTicks is the ticks one block of swimming takes, with a margin over what the walker's swim measured
	// (10.3 along a flooded passage; up through a shaft 5.5): the air budget counts the swim at this speed.
	swimTicks = 11
	// airRefill is the air got back in a tick of breathing.
	airRefill = 4
)

// submerged reports whether the eyes of a body at the node are in water: it loses air there. A body whose
// feet are in water and its head out (swimming at the surface) breathes.
func (g *Graph) submerged(n Node) bool {
	return g.water(world.BlockPos{X: n.Pos.X, Y: int(math.Floor(n.Floor + eyeHeight)), Z: n.Pos.Z})
}

// airFrom is the air a walk starts with (FullAir when Air is unset or says none).
func (g *Graph) airFrom() int {
	if g.Air == nil {
		return FullAir
	}
	return min(max(g.Air(), 0), FullAir)
}

// airBudget is the air (ticks) a plan may spend: three quarters of what the walk starts with. The rest is
// the margin for a swim that goes slower than counted, a push by a current or a mob, a plan made again from
// the middle of the water; and it is in proportion, so a body that has little left may still swim the few
// blocks up to breathe.
func (g *Graph) airBudget() int {
	if g.noAirLimit {
		return math.MaxInt / 2
	}
	return g.airFrom() * 3 / 4
}

// swimTime is the ticks the step from a to b takes in water: swimTicks a block, a diagonal as long as it is.
func swimTime(a, b world.BlockPos) int {
	d := math.Sqrt(float64((a.X-b.X)*(a.X-b.X) + (a.Y-b.Y)*(a.Y-b.Y) + (a.Z-b.Z)*(a.Z-b.Z)))
	return int(math.Ceil(d * swimTicks))
}

// airUse is what a step costs of the air spent so far (used, ticks): a step that starts or ends with the eyes
// under water loses its whole time; one between two places with the head out gives the air back. ok is false
// when the step spends more than the walk has (budget): such a step is not taken.
func (g *Graph) airUse(used, budget int, from, to Node) (int, bool) {
	t := swimTime(from.Pos, to.Pos)
	if g.submerged(from) || g.submerged(to) {
		used += t
		return used, used <= budget
	}
	return max(used-airRefill*t, 0), true
}

// airNeed is the most air (ticks) the way on from start along path spends before the head is out of water: the
// walker compares it with the air it has left, and plans again when the way has grown longer than the air.
func (g *Graph) airNeed(start Node, path []Node) int {
	used, peak, from := 0, 0, start
	for _, n := range path {
		used, _ = g.airUse(used, math.MaxInt, from, n)
		peak = max(peak, used)
		from = n
	}
	return peak
}
