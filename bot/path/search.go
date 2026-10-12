package path

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// ErrNoPath: the goal is not reachable within the nodes searched.
var ErrNoPath = errors.New("path: no way to the goal")

// NoPathError is ErrNoPath with what the search found: the place reached
// nearest the goal — where a player who must dig on starts digging — and the
// nearest it can walk back from (Back: reached with no one-way edge on its
// way, a drop it could not climb or a jump over a gap: where a walk to as
// near as the land lets ends, never a ledge down a cliff), how many it
// searched, and whether it stopped at the limit (else every place reachable
// was searched).
type NoPathError struct {
	Closest  Node
	Back     Node
	Searched int
	Labels   int // the ways to places kept (more than Searched where water gives a place several: air.go)
	Limit    bool
	From     world.BlockPos
	// Air: a step was refused for want of air (the way through water was longer than the air lasts): with
	// more air, or a way to breathe on the way, a path may exist.
	Air bool
}

func (e *NoPathError) Error() string {
	if e.Air {
		return fmt.Sprintf("%v: under water too long (the air does not last the way) from %v", ErrNoPath, e.From)
	}
	if e.Limit {
		return fmt.Sprintf("%v: %d places searched from %v, the limit", ErrNoPath, e.Searched, e.From)
	}
	return fmt.Sprintf("%v: every place reachable searched (%d) from %v", ErrNoPath, e.Searched, e.From)
}

func (e *NoPathError) Unwrap() error { return ErrNoPath }

// Start returns the node a player standing at (x, y, z) is in.
func (g *Graph) Start(x, y, z float64) (Node, bool) {
	p := world.BlockPos{X: int(math.Floor(x)), Y: int(math.Floor(y + 1e-6)), Z: int(math.Floor(z))}
	if f, ok := g.floor(p); ok {
		return Node{Pos: p, Floor: f}, true
	}
	// a player partway into the cell above (on a slab's top, mid-jump)
	for _, dy := range []int{-1, 1} {
		q := world.BlockPos{X: p.X, Y: p.Y + dy, Z: p.Z}
		if f, ok := g.floor(q); ok {
			return Node{Pos: q, Floor: f}, true
		}
	}
	return Node{Pos: p, Floor: y}, false
}

// Find returns the nodes from start to goal, start excluded, searching at most
// maxNodes. A goal that is not a place to stand is reached when the path ends
// next to it (within reach of 1.5 blocks), as for a block to dig or a player
// to follow.
func (g *Graph) Find(start Node, goal world.BlockPos, maxNodes int) ([]Node, error) {
	return g.FindContext(context.Background(), start, goal, maxNodes)
}

// FindContext is Find that ends with the context's error when ctx is done: it looks at it every 256 places
// searched (a walker's search runs on a goroutine of its own and is cancelled by the next walk it is asked).
func (g *Graph) FindContext(ctx context.Context, start Node, goal world.BlockPos, maxNodes int) ([]Node, error) {
	_, goalStandable := g.floor(goal)
	reached := func(p world.BlockPos) bool {
		if goalStandable {
			return p == goal
		}
		dx, dy, dz := float64(p.X-goal.X), float64(p.Y-goal.Y), float64(p.Z-goal.Z)
		return dx*dx+dy*dy+dz*dz <= 1.5*1.5+1
	}
	h := func(p world.BlockPos) float64 {
		dx, dy, dz := float64(p.X-goal.X), float64(p.Y-goal.Y), float64(p.Z-goal.Z)
		return math.Sqrt(dx*dx + dy*dy + dz*dz)
	}
	budget := g.airBudget()
	airRefused := false
	labels := 1
	exits := map[world.BlockPos]int{} // the ticks from a place to air, once asked (air.go)
	root := &label{node: start}
	seen := map[world.BlockPos][]*label{start.Pos: {root}}
	open := &queue{{r: root, f: h(start.Pos)}}
	closest, closestH := start, h(start.Pos)
	n := 0
	for ; open.Len() > 0 && n < maxNodes; n++ {
		if n&255 == 255 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		r := heap.Pop(open).(item).r
		if r.dead || r.closed {
			continue
		}
		r.closed = true
		cur := r.node.Pos
		if hc := h(cur); hc < closestH {
			closest, closestH = r.node, hc
		}
		if reached(cur) && r.parent != nil && !g.canLeave(r.node, budget-r.used, exits) {
			airRefused = true // the goal is under water and the air does not last the way out (air.go)
		} else if reached(cur) {
			var out []Node
			for ; r.parent != nil; r = r.parent {
				out = append(out, r.node)
			}
			for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
				out[i], out[j] = out[j], out[i]
			}
			return out, nil
		}
		for _, e := range g.neighbours(r.node) {
			used, ok := g.airUse(r.used, budget, r.node, e.to)
			if !ok {
				airRefused = true
				continue
			}
			cost := r.g + e.cost
			better := true
			for _, o := range seen[e.to.Pos] {
				if !o.dead && o.g <= cost && o.used/airBucket <= used/airBucket {
					better = false
					break
				}
			}
			if !better {
				continue
			}
			for _, o := range seen[e.to.Pos] {
				if o.g >= cost && o.used/airBucket >= used/airBucket {
					o.dead = true
				}
			}
			nr := &label{node: e.to, g: cost, used: used, parent: r}
			seen[e.to.Pos] = append(seen[e.to.Pos], nr)
			labels++
			heap.Push(open, item{r: nr, f: cost + h(e.to.Pos)})
		}
	}
	if airRefused && !g.noAirLimit {
		// a step was refused for want of air: was that why there is no way? With no limit on the air, is there one?
		free := *g
		free.noAirLimit = true
		_, err := free.FindContext(ctx, start, goal, min(maxNodes, airAskNodes))
		airRefused = err == nil
	} else {
		airRefused = false
	}
	back := g.backNearest(start, h, maxNodes)
	return nil, &NoPathError{Closest: closest, Back: back, Searched: n, Labels: labels, Limit: open.Len() > 0, From: start.Pos, Air: airRefused}
}

// backNearest is the place nearest the goal (by h) of those reached from
// start by edges that each have their way back — the places it can walk to
// and back from — searched breadth first, at most maxNodes (a search that
// found no way: run once more over what it can come back from).
func (g *Graph) backNearest(start Node, h func(world.BlockPos) float64, maxNodes int) Node {
	best, bestH := start, h(start.Pos)
	budget := g.airBudget()
	type visit struct {
		node Node
		used int
	}
	least := map[world.BlockPos]int{start.Pos: 0} // the least air spent reaching a place
	queue := []visit{{start, 0}}
	for n := 0; len(queue) > 0 && n < maxNodes; n++ {
		cur := queue[0]
		queue = queue[1:]
		if hc := h(cur.node.Pos); hc < bestH {
			best, bestH = cur.node, hc
		}
		for _, e := range g.neighbours(cur.node) {
			// a level walk goes back the same way; any other edge only if
			// the place it reaches has the edge back (a drop of two, a gap's
			// jump: one way)
			used, ok := g.airUse(cur.used, budget, cur.node, e.to)
			if !ok {
				continue // (not through more water than the air lasts)
			}
			if old, seen := least[e.to.Pos]; seen && old <= used || e.to.Kind != Walk && !g.edgeBack(e.to, cur.node) {
				continue
			}
			least[e.to.Pos] = used
			queue = append(queue, visit{e.to, used})
		}
	}
	return best
}

// edgeBack reports whether there is a move from b back to a.
func (g *Graph) edgeBack(b, a Node) bool {
	for _, e := range g.neighbours(b) {
		if e.to.Pos == a.Pos {
			return true
		}
	}
	return false
}

// airAskNodes bounds the search that asks whether the air alone refused the way: a plan refused for the air is
// asked again, with no limit on the air, at most this many places (a bot in a big flooded cave replans often).
const airAskNodes = 20000

// label is a way to a place: the cost so far and the air spent (used, ticks; air.go). The labels of a place are
// the ways to it that neither cost more nor have spent more than another; with no water there is one each.
type label struct {
	node   Node
	g      float64
	used   int
	parent *label
	dead   bool // a way with less cost and less air spent was found
	closed bool
}

type item struct {
	r *label
	f float64
}

type queue []item

func (q queue) Len() int           { return len(q) }
func (q queue) Less(i, j int) bool { return q[i].f < q[j].f }
func (q queue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)        { *q = append(*q, x.(item)) }
func (q *queue) Pop() any          { old := *q; it := old[len(old)-1]; *q = old[:len(old)-1]; return it }
