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
	Limit    bool
	From     world.BlockPos
}

func (e *NoPathError) Error() string {
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
	type rec struct {
		node   Node
		g      float64
		parent world.BlockPos
		root   bool
	}
	seen := map[world.BlockPos]*rec{start.Pos: {node: start, root: true}}
	open := &queue{{pos: start.Pos, f: h(start.Pos)}}
	closed := map[world.BlockPos]bool{}
	closest, closestH := start, h(start.Pos)
	n := 0
	for ; open.Len() > 0 && n < maxNodes; n++ {
		if n&255 == 255 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		cur := heap.Pop(open).(item).pos
		if closed[cur] {
			continue
		}
		closed[cur] = true
		r := seen[cur]
		if hc := h(cur); hc < closestH {
			closest, closestH = r.node, hc
		}
		if reached(cur) {
			var out []Node
			for !r.root {
				out = append(out, r.node)
				r = seen[r.parent]
			}
			for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
				out[i], out[j] = out[j], out[i]
			}
			return out, nil
		}
		for _, e := range g.neighbours(r.node) {
			cost := r.g + e.cost
			if old, ok := seen[e.to.Pos]; ok && old.g <= cost {
				continue
			}
			seen[e.to.Pos] = &rec{node: e.to, g: cost, parent: cur}
			heap.Push(open, item{pos: e.to.Pos, f: cost + h(e.to.Pos)})
		}
	}
	back := g.backNearest(start, h, maxNodes)
	return nil, &NoPathError{Closest: closest, Back: back, Searched: n, Limit: open.Len() > 0, From: start.Pos}
}

// backNearest is the place nearest the goal (by h) of those reached from
// start by edges that each have their way back — the places it can walk to
// and back from — searched breadth first, at most maxNodes (a search that
// found no way: run once more over what it can come back from).
func (g *Graph) backNearest(start Node, h func(world.BlockPos) float64, maxNodes int) Node {
	best, bestH := start, h(start.Pos)
	seen := map[world.BlockPos]bool{start.Pos: true}
	queue := []Node{start}
	for n := 0; len(queue) > 0 && n < maxNodes; n++ {
		cur := queue[0]
		queue = queue[1:]
		if hc := h(cur.Pos); hc < bestH {
			best, bestH = cur, hc
		}
		for _, e := range g.neighbours(cur) {
			// a level walk goes back the same way; any other edge only if
			// the place it reaches has the edge back (a drop of two, a gap's
			// jump: one way)
			if seen[e.to.Pos] || e.to.Kind != Walk && !g.edgeBack(e.to, cur) {
				continue
			}
			seen[e.to.Pos] = true
			queue = append(queue, e.to)
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

type item struct {
	pos world.BlockPos
	f   float64
}

type queue []item

func (q queue) Len() int           { return len(q) }
func (q queue) Less(i, j int) bool { return q[i].f < q[j].f }
func (q queue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)        { *q = append(*q, x.(item)) }
func (q *queue) Pop() any          { old := *q; it := old[len(old)-1]; *q = old[:len(old)-1]; return it }
