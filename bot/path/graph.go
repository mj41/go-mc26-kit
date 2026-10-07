// Package path finds a way through the blocks for a player and walks it the
// way a person does: turning toward the next block and pressing the keys,
// the physics doing the rest.
//
// A node is a block position the player can be in: standing on something
// (a full block below, or a slab or carpet in the cell itself), on a ladder,
// or in water. The moves are the ones the physics can do — walk, also
// diagonally past two clear sides; step or jump up one block; drop down up
// to three; jump over a one-block gap; climb; swim — each checked against the
// extracted block shapes.
package path

import (
	"math"
	"strings"
	"sync"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Graph is the world as the search sees it.
type Graph struct {
	World physics.Blocks
	Tags  physics.Tags
	// Avoid, when set, names cells the search never steps into (a field's
	// channel: a person walks round it, not through it); one it is in
	// already it can still leave.
	Avoid func(world.BlockPos) bool
}

// Kind is how a node is reached.
type Kind int

const (
	Walk Kind = iota
	Ascend
	Descend
	GapJump
	Climb
	Swim
)

func (k Kind) String() string {
	return [...]string{"walk", "ascend", "descend", "gap", "climb", "swim"}[k]
}

// Node is a place to be: the cell the feet are in and the height they are at.
type Node struct {
	Pos   world.BlockPos
	Floor float64
	Kind  Kind // how the step from the previous node goes
}

const (
	halfWidth = 0.3
	height    = 1.8
	maxStep   = 0.6 // walked up without a jump
	// nearlyFull: a block at least this high is stood on as a full one
	// (farmland and paths are 15/16, soul sand and mud 14/16)
	nearlyFull = 0.85
	maxJump    = 1.25 // jumped up onto
	maxDrop    = 3    // blocks fallen without harm
)

// boxes returns the collision boxes of a cell in world coordinates.
func (g *Graph) boxes(p world.BlockPos) ([]block.AABB, bool) {
	s, ok := g.World.BlockAt(p)
	if !ok {
		return nil, false
	}
	shape := block.CollisionShape(s)
	out := make([]block.AABB, len(shape))
	for i, b := range shape {
		out[i] = block.AABB{MinX: b.MinX + float64(p.X), MinY: b.MinY + float64(p.Y), MinZ: b.MinZ + float64(p.Z),
			MaxX: b.MaxX + float64(p.X), MaxY: b.MaxY + float64(p.Y), MaxZ: b.MaxZ + float64(p.Z)}
	}
	return out, true
}

func (g *Graph) state(p world.BlockPos) block.StateID {
	s, _ := g.World.BlockAt(p)
	return s
}

func (g *Graph) dangerous(p world.BlockPos) bool {
	if f := block.FluidOf(g.state(p)); f != nil && fluidIs(f, "lava") {
		return true
	}
	switch block.StateList[g.state(p)].ID() {
	case "minecraft:fire", "minecraft:soul_fire", "minecraft:magma_block", "minecraft:cactus",
		"minecraft:sweet_berry_bush", "minecraft:powder_snow", "minecraft:cobweb", "minecraft:campfire", "minecraft:soul_campfire":
		return true
	}
	return false
}

func fluidIs(f *block.Fluid, kind string) bool {
	return f.Name == "minecraft:"+kind || f.Name == "minecraft:flowing_"+kind
}

func (g *Graph) lava(p world.BlockPos) bool {
	f := block.FluidOf(g.state(p))
	return f != nil && fluidIs(f, "lava")
}

func (g *Graph) water(p world.BlockPos) bool {
	f := block.FluidOf(g.state(p))
	return f != nil && fluidIs(f, "water")
}

func (g *Graph) climbable(p world.BlockPos) bool {
	return g.Tags != nil && g.Tags.BlockIn(g.state(p), "minecraft:climbable")
}

// clear reports whether a player-sized box centred on the cell, from y0 to
// y1, touches no block, no lava (and no loaded-chunk edge).
func (g *Graph) clear(x, z int, y0, y1 float64) bool {
	box := block.AABB{MinX: float64(x) + 0.5 - halfWidth, MinY: y0, MinZ: float64(z) + 0.5 - halfWidth,
		MaxX: float64(x) + 0.5 + halfWidth, MaxY: y1, MaxZ: float64(z) + 0.5 + halfWidth}
	for y := int(math.Floor(y0)); y <= int(math.Floor(y1-1e-9)); y++ {
		bs, ok := g.boxes(world.BlockPos{X: x, Y: y, Z: z})
		if !ok || g.lava(world.BlockPos{X: x, Y: y, Z: z}) { // lava has no box, but is no room
			return false
		}
		for _, b := range bs {
			if b.MinX < box.MaxX && b.MaxX > box.MinX && b.MinY < box.MaxY && b.MaxY > box.MinY && b.MinZ < box.MaxZ && b.MaxZ > box.MinZ {
				return false
			}
		}
	}
	return true
}

// sweeps reports whether the body crosses from the middle of cell a to the
// middle of cell b (side by side) from y0 to y1 touching no block of either:
// a box at a cell's edge across the way (a shut door, a gate) stops the step
// though neither cell's middle is filled.
func (g *Graph) sweeps(a, b world.BlockPos, y0, y1 float64) bool {
	box := block.AABB{
		MinX: math.Min(float64(a.X), float64(b.X)) + 0.5 - halfWidth, MaxX: math.Max(float64(a.X), float64(b.X)) + 0.5 + halfWidth,
		MinZ: math.Min(float64(a.Z), float64(b.Z)) + 0.5 - halfWidth, MaxZ: math.Max(float64(a.Z), float64(b.Z)) + 0.5 + halfWidth,
		MinY: y0 + 1e-6, MaxY: y1,
	}
	for _, c := range [2]world.BlockPos{a, b} {
		for y := int(math.Floor(y0)); y <= int(math.Floor(y1-1e-9)); y++ {
			bs, ok := g.boxes(world.BlockPos{X: c.X, Y: y, Z: c.Z})
			if !ok {
				return false
			}
			for _, bb := range bs {
				if bb.MinX < box.MaxX && bb.MaxX > box.MinX && bb.MinY < box.MaxY && bb.MaxY > box.MinY && bb.MinZ < box.MaxZ && bb.MaxZ > box.MinZ {
					return false
				}
			}
		}
	}
	return true
}

// floor returns the height a player stands at with its feet in cell p, and
// whether it can be there at all: on what is below, on a low block in the
// cell itself (a slab), on a ladder, in water.
func (g *Graph) floor(p world.BlockPos) (float64, bool) {
	cell, ok := g.boxes(p)
	if !ok || g.dangerous(p) || g.lava(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) {
		return 0, false // lava at the feet, or at the head (pouring down from above)
	}
	top := 0.0
	for _, b := range cell {
		// a box beside the body standing in the cell's middle (an open door,
		// a trapdoor up against a side) is neither its floor nor a wall
		cx, cz := float64(p.X)+0.5, float64(p.Z)+0.5
		if b.MaxX <= cx-halfWidth || b.MinX >= cx+halfWidth || b.MaxZ <= cz-halfWidth || b.MinZ >= cz+halfWidth {
			continue
		}
		top = math.Max(top, b.MaxY-float64(p.Y))
	}
	var f float64
	switch {
	case g.climbable(p) || g.water(p):
		// a ladder is a thin full-height box: whether the body fits beside
		// it is the clearance check's
		f = float64(p.Y)
	case top > maxStep:
		return 0, false // the cell is a wall
	case top > 0:
		f = float64(p.Y) + top
	default:
		below := world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}
		bs, ok := g.boxes(below)
		if !ok || g.dangerous(below) {
			return 0, false
		}
		support := 0.0
		for _, b := range bs {
			support = math.Max(support, b.MaxY-float64(below.Y))
		}
		switch {
		case support >= 1-1e-9 || g.water(below) || g.climbable(below):
			f = float64(p.Y)
		case support >= nearlyFull:
			// farmland, a path, soul sand: a block short of full is stood
			// on like one, the feet a little into its cell (without it a
			// player on farmland had nowhere to start from)
			f = float64(below.Y) + support
		default:
			return 0, false
		}
	}
	if !g.clear(p.X, p.Z, f, f+height) {
		return 0, false
	}
	return f, true
}

var dirs = [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}

// edge is a move from one node to another and what it costs.
type edge struct {
	to   Node
	cost float64
}

// neighbours returns the nodes reachable from n in one move.
func (g *Graph) neighbours(n Node) []edge {
	var out []edge
	p := n.Pos
	inWater := g.water(p)
	for _, d := range dirs {
		diagonal := d[0] != 0 && d[1] != 0
		if diagonal {
			// both sides passable at this height, or the corner catches the player
			if !g.clear(p.X+d[0], p.Z, n.Floor, n.Floor+height) || !g.clear(p.X, p.Z+d[1], n.Floor, n.Floor+height) {
				continue
			}
			// and neither side avoided: the body crosses a corner of each
			if g.Avoid != nil && (g.Avoid(world.BlockPos{X: p.X + d[0], Y: p.Y, Z: p.Z}) || g.Avoid(world.BlockPos{X: p.X, Y: p.Y, Z: p.Z + d[1]})) {
				continue
			}
		}
		base := 1.0
		if diagonal {
			base = math.Sqrt2
		}
		if inWater {
			base *= 2
		}
		// up: onto a block one higher, with room above to jump
		up := world.BlockPos{X: p.X + d[0], Y: p.Y + 1, Z: p.Z + d[1]}
		if f, ok := g.floor(up); ok && f-n.Floor <= maxJump && !diagonal && g.clear(p.X, p.Z, n.Floor, f+height) && g.sweeps(p, up, f, f+height) {
			out = append(out, edge{Node{up, f, Ascend}, base + 1})
			continue
		}
		// level: walk, a slab stepped onto
		level := world.BlockPos{X: p.X + d[0], Y: p.Y, Z: p.Z + d[1]}
		if f, ok := g.floor(level); ok && (diagonal || g.sweeps(p, level, math.Max(n.Floor, f), math.Max(n.Floor, f)+height)) {
			kind, cost := Walk, base
			if f-n.Floor > maxStep {
				if diagonal || !g.clear(p.X, p.Z, n.Floor, f+height) {
					continue
				}
				kind, cost = Ascend, base+1
			}
			if g.water(level) {
				kind = Swim
			}
			out = append(out, edge{Node{level, f, kind}, cost})
			continue
		}
		// down: the body fits over the edge, then a drop of up to three
		if diagonal || !g.clear(level.X, level.Z, n.Floor, n.Floor+height) || !g.sweeps(p, level, n.Floor, n.Floor+height) {
			continue
		}
		for k := 1; k <= maxDrop; k++ {
			down := world.BlockPos{X: level.X, Y: p.Y - k, Z: level.Z}
			if f, ok := g.floor(down); ok {
				out = append(out, edge{Node{down, f, Descend}, base + 0.5*float64(k)})
				break
			}
			if !g.clear(down.X, down.Z, float64(down.Y), float64(down.Y)+1) {
				break // something solid with nothing to stand on: a wall in the way
			}
		}
		// a one-block gap: nothing to land on below the next cell, the one after level
		if _, ok := g.floorWithin(level, maxDrop); !ok {
			far := world.BlockPos{X: p.X + 2*d[0], Y: p.Y, Z: p.Z + 2*d[1]}
			if f, ok := g.floor(far); ok && math.Abs(f-n.Floor) < 1e-9 &&
				g.clear(level.X, level.Z, n.Floor, n.Floor+height+1.25) && g.clear(p.X, p.Z, n.Floor, n.Floor+height+1.25) {
				out = append(out, edge{Node{far, f, GapJump}, 3})
			}
		}
	}
	// up and down a ladder or through water
	if g.climbable(p) || inWater {
		kind := Climb
		if inWater {
			kind = Swim
		}
		above := world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}
		if f, ok := g.floor(above); ok && (g.climbable(above) || g.water(above) || f > n.Floor) {
			out = append(out, edge{Node{above, f, kind}, 1.5})
		}
		below := world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}
		if f, ok := g.floor(below); ok && (g.climbable(below) || g.water(below)) {
			out = append(out, edge{Node{below, f, kind}, 1.5})
		}
	}
	if g.Avoid != nil {
		kept := out[:0]
		for _, e := range out {
			if !g.Avoid(e.to.Pos) {
				kept = append(kept, e)
			}
		}
		out = kept
	}
	for i := range out {
		out[i].cost += g.wet(out[i].to.Pos) + g.nearLava(out[i].to.Pos)
		if g.OnTree(out[i].to) {
			out[i].cost += treeCost
		}
	}
	return out
}

// treeCost is what a step on a tree costs over the ground: a person walks
// under the trees, not over their crowns and trunks — leaves give way as the
// tree is felled or decays, a fall from a crown hurts. Over them only where
// the ground has no way: a steep hillside, a mountain, for a few steps.
const treeCost = 10

// OnTree reports whether the feet at n stand on a tree — its log, wood or
// leaves, a big mushroom, mangrove roots — and not on the ground.
func (g *Graph) OnTree(n Node) bool {
	if math.Abs(n.Floor-float64(n.Pos.Y)) > 1e-9 || g.water(n.Pos) || g.climbable(n.Pos) {
		return false // on a low block in its cell, a ladder, in water
	}
	s := g.state(world.BlockPos{X: n.Pos.X, Y: n.Pos.Y - 1, Z: n.Pos.Z})
	t := treeStates()
	return int(s) >= 0 && int(s) < len(t) && t[s]
}

// treeStates marks the block states that are of a tree, by the block's name.
var treeStates = sync.OnceValue(func() []bool {
	t := make([]bool, len(block.StateList))
	for i, b := range block.StateList {
		id := b.ID()
		for _, suf := range []string{"_log", "_wood", "_leaves", "_hyphae", "_mushroom_block", "mushroom_stem", "crimson_stem", "warped_stem", ":mangrove_roots"} {
			if strings.HasSuffix(id, suf) {
				t[i] = true
				break
			}
		}
	}
	return t
})

// nearLava is what being at p costs for lava beside it (at the feet or the
// head): one more block dug, a mob's push, and it is in.
func (g *Graph) nearLava(p world.BlockPos) float64 {
	for dy := 0; dy <= 1; dy++ {
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if g.lava(world.BlockPos{X: p.X + d[0], Y: p.Y + dy, Z: p.Z + d[1]}) {
				return 8
			}
		}
	}
	return 0
}

// wet is what being at p costs over dry ground: a person keeps out of water
// as of lava where there is a way round — still water slows and drowns, a
// stream pushes off the way — but swims where there is none.
func (g *Graph) wet(p world.BlockPos) float64 {
	cost := 0.0
	for _, c := range []world.BlockPos{p, {X: p.X, Y: p.Y + 1, Z: p.Z}} {
		f := block.FluidOf(g.state(c))
		if f == nil || !fluidIs(f, "water") {
			continue
		}
		if f.Source {
			cost = math.Max(cost, 4)
		} else {
			cost = math.Max(cost, 30) // a stream: it pushes off the way, round it almost always
		}
	}
	return cost
}

// floorWithin reports whether something to stand on is below p within depth.
func (g *Graph) floorWithin(p world.BlockPos, depth int) (float64, bool) {
	for k := 0; k <= depth; k++ {
		if f, ok := g.floor(world.BlockPos{X: p.X, Y: p.Y - k, Z: p.Z}); ok {
			return f, true
		}
	}
	return 0, false
}
