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
	// airBucket is the coarseness of the air spent when ways to a place are compared (search.go): of two ways
	// to a place whose air spent is within the same three blocks of swimming, the cheaper is kept. It keeps the
	// labels of a place few where there is water (the quarter of the air the budget leaves is 75 ticks: a way
	// dropped for it has spent less than 33 ticks more than the one kept).
	airBucket = 33
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
	if g.noAirLimit {
		return 0, true // no limit: the air is not counted, and a place has one label as ever
	}
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

// airSearchNodes bounds the search for the nearest air (NearestAir): a body deep in a big flooded cave has no
// more places to look at than this, and no air for that many strokes either.
const airSearchNodes = 5000

// NearestAir is the nearest place to breathe from a node: the cheapest way, in ticks of swimming, to one whose
// eyes are out of water, and what it costs, when that is within air ticks. A node that breathes is its own
// nearest air (cost 0). A body that stops swimming ahead to go up (a brain's "air low") goes there rather than
// back to where it last breathed: a pocket ahead may be nearer.
func (g *Graph) NearestAir(from Node, air int) (Node, int, bool) {
	if !g.submerged(from) {
		return from, 0, true
	}
	type step struct {
		node  Node
		ticks int
		prev  *step
	}
	best := map[world.BlockPos]int{from.Pos: 0}
	open := []*step{{node: from}}
	for n := 0; len(open) > 0 && n < airSearchNodes; n++ {
		// the cheapest open place (the lists are short: a Dijkstra by a linear scan is not the cost here, the
		// neighbours are)
		bi := 0
		for i, c := range open {
			if c.ticks < open[bi].ticks {
				bi = i
			}
		}
		cur := open[bi]
		open = append(open[:bi], open[bi+1:]...)
		if t, ok := best[cur.node.Pos]; ok && t < cur.ticks {
			continue
		}
		if cur.ticks > air {
			return Node{}, 0, false
		}
		if cur.prev != nil && !g.submerged(cur.node) {
			return cur.node, cur.ticks, true
		}
		for _, e := range g.neighbours(cur.node) {
			t := cur.ticks + swimTime(cur.node.Pos, e.to.Pos)
			if old, ok := best[e.to.Pos]; ok && old <= t {
				continue
			}
			best[e.to.Pos] = t
			open = append(open, &step{node: e.to, ticks: t, prev: cur})
		}
	}
	return Node{}, 0, false
}

// canLeave reports whether a body that has got to a goal with room ticks of its budget left has the way out
// from there to air in what remains, after the Linger it spends at the goal: a goal under water (a block to dig
// on a lake's bed) leaves it there with what it has left, and a way out that does not fit is a plan to drown. A
// goal with the head out needs no way out. exits holds the ticks asked for before (-1: none within the budget).
func (g *Graph) canLeave(goal Node, room int, exits map[world.BlockPos]int) bool {
	if g.noAirLimit || !g.submerged(goal) {
		return true
	}
	out, ok := exits[goal.Pos]
	if !ok {
		out = -1
		if _, ticks, found := g.NearestAir(goal, g.airBudget()); found {
			out = ticks
		}
		exits[goal.Pos] = out
	}
	return out >= 0 && g.Linger+out <= room
}
