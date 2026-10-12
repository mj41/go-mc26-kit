package path

import (
	"errors"
	"math"
	"testing"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// testWorld is flat (grass at y -61 and below, air above) with blocks set on top.
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

type ladderTags struct{}

func (ladderTags) BlockIn(s block.StateID, tag string) bool {
	id := block.StateList[s].ID()
	return tag == "minecraft:climbable" && (id == "minecraft:ladder" || id == "minecraft:vine")
}

// walk drives a walker and the physics together for at most ticks and returns
// the walker's status and where the player ended.
func walk(t *testing.T, w testWorld, from physics.Vec3, goal world.BlockPos, ticks int) (string, basic.Position) {
	t.Helper()
	bp := basic.NewPlayer(bot.NewClient(), basic.DefaultSettings, basic.EventsListener{})
	bp.SetPosition(basic.Position{X: from.X, Y: from.Y, Z: from.Z})
	g := &Graph{World: w, Tags: ladderTags{}}
	walker := &Walker{Graph: g}
	mover := &physics.Mover{World: w, Tags: ladderTags{}}
	var s control.State
	walker.Goto(goal)
	for range ticks {
		walker.Think(&s, bp)
		walker.waitSearch()
		mover.Step(&s, bp)
		if st := walker.Status(); st != "walking" {
			return st, bp.Position()
		}
	}
	return walker.Status(), bp.Position()
}

func TestWalkCourse(t *testing.T) {
	stone := block.ToStateID[block.Stone{}]
	w := testWorld{}
	w.fill(3, -60, -5, 3, -59, 5, stone) // a wall 2 high across x=3, with a gap at z=0
	delete(w, world.BlockPos{X: 3, Y: -60, Z: 0})
	delete(w, world.BlockPos{X: 3, Y: -59, Z: 0})
	w.fill(6, -60, -1, 7, -60, 1, stone)                        // a block to step onto: ascend
	w.fill(9, -61, -1, 9, -63, 1, block.ToStateID[block.Air{}]) // a hole after it: gap or drop
	st, p := walk(t, w, physics.Vec3{X: 0.5, Y: -60, Z: -3.5}, world.BlockPos{X: 12, Y: -60, Z: 0}, 600)
	if st != "arrived" {
		t.Fatalf("walker: %s at %+v", st, p)
	}
	if math.Floor(p.X) != 12 || math.Floor(p.Z) != 0 || p.Y != -60 {
		t.Errorf("ended at %+v, want in 12 -60 0", p)
	}
}

func TestClimbLadder(t *testing.T) {
	stone := block.ToStateID[block.Stone{}]
	ladder := block.ToStateID[block.Ladder{Facing: block.DirectionWest}]
	w := testWorld{}
	w.fill(5, -60, 0, 5, -56, 0, stone)  // a wall
	w.fill(4, -60, 0, 4, -57, 0, ladder) // a ladder on its west side, four high
	w.fill(5, -56, 0, 7, -56, 0, stone)  // a platform on top of the wall, at -55
	w.fill(6, -60, 0, 7, -57, 0, stone)
	st, p := walk(t, w, physics.Vec3{X: 1.5, Y: -60, Z: 0.5}, world.BlockPos{X: 6, Y: -55, Z: 0}, 800)
	if st != "arrived" || p.Y != -55 {
		t.Fatalf("walker: %s at %+v, want arrived on the platform at -55", st, p)
	}
}

// TestAvoid: cells the graph is told to avoid (a field's channel) are walked
// round, never through.
func TestAvoid(t *testing.T) {
	w := testWorld{}
	avoid := map[world.BlockPos]bool{}
	for x := 3; x <= 6; x++ {
		avoid[world.BlockPos{X: x, Y: -60, Z: 0}] = true
	}
	bp := basic.NewPlayer(bot.NewClient(), basic.DefaultSettings, basic.EventsListener{})
	bp.SetPosition(basic.Position{X: 0.5, Y: -60, Z: 0.5})
	g := &Graph{World: w, Tags: ladderTags{}, Avoid: func(p world.BlockPos) bool { return avoid[p] }}
	walker := &Walker{Graph: g}
	mover := &physics.Mover{World: w, Tags: ladderTags{}}
	var s control.State
	walker.Goto(world.BlockPos{X: 10, Y: -60, Z: 0})
	for range 600 {
		walker.Think(&s, bp)
		walker.waitSearch()
		mover.Step(&s, bp)
		p := bp.Position()
		if c := (world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))}); avoid[c] {
			t.Fatalf("walked into the avoided cell %v", c)
		}
		if walker.Status() != "walking" {
			break
		}
	}
	if st := walker.Status(); st != "arrived" {
		t.Fatalf("walker: %s at %+v", st, bp.Position())
	}
}

// TestWalkOnFarmland: farmland (15/16 high) is walked on like any ground:
// from on it, along it and off it.
func TestWalkOnFarmland(t *testing.T) {
	w := testWorld{}
	w.fill(2, -61, -1, 9, -61, 1, block.ToStateID[block.Farmland{}]) // a field strip at the ground's level
	st, p := walk(t, w, physics.Vec3{X: 3.5, Y: -61 + 0.9375, Z: 0.5}, world.BlockPos{X: 12, Y: -60, Z: 0}, 600)
	if st != "arrived" {
		t.Fatalf("from farmland: %s at %+v", st, p)
	}
	st, p = walk(t, w, physics.Vec3{X: 12.5, Y: -60, Z: 0.5}, world.BlockPos{X: 4, Y: -60, Z: 1}, 600)
	if st != "arrived" || math.Abs(p.Y-(-61+0.9375)) > 1e-6 {
		t.Fatalf("onto farmland: %s at %+v", st, p)
	}
}

// stateOf is the first state of the block named id.
func stateOf(t *testing.T, id string) block.StateID {
	t.Helper()
	for i, b := range block.StateList {
		if b.ID() == id {
			return block.StateID(i)
		}
	}
	t.Fatalf("no block %s", id)
	return 0
}

// TestTreeWalkedRound: a hedge of leaves across the way is walked round, as
// a person does, not jumped onto and over — though over is shorter.
func TestTreeWalkedRound(t *testing.T) {
	w := testWorld{}
	w.fill(5, -60, -4, 5, -60, 4, stateOf(t, "minecraft:oak_leaves"))
	g := &Graph{World: w, Tags: ladderTags{}}
	start, ok := g.Start(0.5, -60, 0.5)
	if !ok {
		t.Fatal("no start")
	}
	nodes, err := g.Find(start, world.BlockPos{X: 10, Y: -60, Z: 0}, 20000)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if g.OnTree(n) {
			t.Fatalf("the way goes over the leaves at %v: %v", n.Pos, nodes)
		}
	}
}

// TestTreeWhenOnly: a tree is stood on where the ground has no way — a
// ledge two high, a leaf block at its foot the one step up.
func TestTreeWhenOnly(t *testing.T) {
	stone := block.ToStateID[block.Stone{}]
	w := testWorld{}
	w.fill(6, -60, -20, 9, -59, 20, stone) // a ledge two high across the way, too long to go round
	w[world.BlockPos{X: 5, Y: -60, Z: 0}] = stateOf(t, "minecraft:oak_leaves")
	g := &Graph{World: w, Tags: ladderTags{}}
	start, ok := g.Start(0.5, -60, 0.5)
	if !ok {
		t.Fatal("no start")
	}
	nodes, err := g.Find(start, world.BlockPos{X: 8, Y: -58, Z: 0}, 20000)
	if err != nil {
		t.Fatal(err)
	}
	on := false
	for _, n := range nodes {
		on = on || g.OnTree(n)
	}
	if !on {
		t.Fatalf("up the ledge without the leaves? %v", nodes)
	}
}

// stateNamed is the state the game writes as name.
func stateNamed(t *testing.T, name string) block.StateID {
	t.Helper()
	for i := range block.StateList {
		if world.StateString(block.StateID(i)) == name {
			return block.StateID(i)
		}
	}
	t.Fatalf("no state %s", name)
	return 0
}

// TestDoor: a corridor one wide with a door across it — open, it is walked
// through (its panel is along the side); shut, it is a wall.
func TestDoor(t *testing.T) {
	stone := block.ToStateID[block.Stone{}]
	for _, c := range []struct {
		open bool
		way  bool
	}{{true, true}, {false, false}} {
		w := testWorld{}
		w.fill(0, -60, -1, 10, -58, -1, stone) // the corridor's walls, along x, z = 0 between
		w.fill(0, -60, 1, 10, -58, 1, stone)
		w.fill(0, -58, 0, 10, -58, 0, stone)   // its roof, two high
		w.fill(-1, -60, -1, -1, -58, 1, stone) // its ends shut: no way round
		w.fill(11, -60, -1, 11, -58, 1, stone)
		open := map[bool]string{true: "true", false: "false"}[c.open]
		w[world.BlockPos{X: 5, Y: -60, Z: 0}] = stateNamed(t, "minecraft:oak_door[facing=east,half=lower,hinge=left,open="+open+",powered=false]")
		w[world.BlockPos{X: 5, Y: -59, Z: 0}] = stateNamed(t, "minecraft:oak_door[facing=east,half=upper,hinge=left,open="+open+",powered=false]")
		g := &Graph{World: w, Tags: ladderTags{}}
		start, ok := g.Start(1.5, -60, 0.5)
		if !ok {
			t.Fatal("no start")
		}
		_, err := g.Find(start, world.BlockPos{X: 9, Y: -60, Z: 0}, 5000)
		if (err == nil) != c.way {
			t.Errorf("door open=%v: a way %v, want %v (%v)", c.open, err == nil, c.way, err)
		}
	}
}

// TestFencePen: a fence is a block and a half high — not jumped onto nor
// over; in a pen of it there is no way out (one stood over the top of a
// fence it could not jump, three minutes, walking out of a pen).
func TestFencePen(t *testing.T) {
	ns := stateNamed(t, "minecraft:oak_fence[east=false,north=true,south=true,waterlogged=false,west=false]")
	ew := stateNamed(t, "minecraft:oak_fence[east=true,north=false,south=false,waterlogged=false,west=true]")
	w := testWorld{}
	w.fill(-3, -60, -3, -3, -60, 3, ns)
	w.fill(3, -60, -3, 3, -60, 3, ns)
	w.fill(-3, -60, -3, 3, -60, -3, ew)
	w.fill(-3, -60, 3, 3, -60, 3, ew)
	g := &Graph{World: w, Tags: ladderTags{}}
	start, ok := g.Start(0.5, -60, 0.5)
	if !ok {
		t.Fatal("no start")
	}
	if nodes, err := g.Find(start, world.BlockPos{X: 10, Y: -60, Z: 0}, 20000); err == nil {
		t.Fatalf("out of the pen over the fence: %v", nodes)
	}
}

// TestVineTopNoJump: at a vine's top under a ledge a block higher there is
// nothing to jump from (a vine is no box, as a ladder is): no way up there —
// the walk does not hang at the top trying.
func TestVineTopNoJump(t *testing.T) {
	stone := block.ToStateID[block.Stone{}]
	vine := stateNamed(t, "minecraft:vine[east=true,north=false,south=false,up=false,west=false]")
	w := testWorld{}
	w.fill(5, -60, -20, 5, -56, 20, stone) // a wall too long to go round
	w.fill(4, -60, 0, 4, -57, 0, vine)     // a vine on its west side, its top a block under the wall's
	w.fill(6, -60, -20, 7, -56, 20, stone)
	g := &Graph{World: w, Tags: ladderTags{}}
	start, ok := g.Start(1.5, -60, 0.5)
	if !ok {
		t.Fatal("no start")
	}
	if nodes, err := g.Find(start, world.BlockPos{X: 6, Y: -55, Z: 0}, 20000); err == nil {
		t.Fatalf("up from the vine's top: %v", nodes)
	}
}

// TestNoJumpOutOfWaterOntoAHighBank: a pond whose banks are a block higher
// than the water all round: no path from the water onto them (a floating body
// cannot jump a block up); with one low place on the shore, the path leaves
// the water there.
func TestNoJumpOutOfWaterOntoAHighBank(t *testing.T) {
	stone := block.ToStateID[block.Stone{}]
	water := stateNamed(t, "minecraft:water[level=0]")
	air := block.ToStateID[block.Air{}]
	w := testWorld{}
	// the ground is raised to -60 (its top at -59) round a pond 5x5, two deep
	w.fill(-10, -60, -10, 10, -60, 10, stone)
	w.fill(-2, -62, -2, 2, -60, 2, air)
	w.fill(-2, -62, -2, 2, -61, 2, water) // water in -62 and -61: its top at -60, the banks' at -59
	g := &Graph{World: w, Tags: ladderTags{}}
	start, ok := g.Start(0.5, -61, 0.5)
	if !ok {
		t.Fatal("no start in the water")
	}
	if nodes, err := g.Find(start, world.BlockPos{X: 6, Y: -59, Z: 0}, 20000); err == nil {
		t.Fatalf("out of the water onto a bank a block higher: %v", nodes)
	}
	// a low place: the bank at x=3 cut down to the water's top
	delete(w, world.BlockPos{X: 3, Y: -60, Z: 0})
	w[world.BlockPos{X: 3, Y: -61, Z: 0}] = stone
	nodes, err := g.Find(start, world.BlockPos{X: 6, Y: -59, Z: 0}, 20000)
	if err != nil {
		t.Fatalf("out by the low place: %v", err)
	}
	via := false
	for _, n := range nodes {
		if n.Pos.X == 3 && n.Pos.Z == 0 && n.Pos.Y == -60 {
			via = true
		}
	}
	if !via {
		t.Errorf("not out by the low place: %v", nodes)
	}
}

// TestBackIsNotDownAOneWayDrop: a plateau, a ledge two blocks below it (a drop
// it cannot climb back), a canyon too deep and too wide round the ledge, the
// goal past it. No way to the goal: the place reached nearest it is the
// ledge (Closest), but the nearest it can walk back from is the plateau's
// edge (Back) — a walk to as near as the land lets ends there, not down on
// the ledge (a desert's canyon ledge held a bot for three minutes).
func TestBackIsNotDownAOneWayDrop(t *testing.T) {
	stone := block.ToStateID[block.Stone{}]
	air := block.ToStateID[block.Air{}]
	w := testWorld{}
	w.fill(-20, -70, -20, 9, -61, 20, air)   // a canyon all round, ten deep
	w.fill(-10, -70, -10, 2, -59, 10, stone) // the plateau standing in it: its top at -58
	w.fill(3, -70, -1, 4, -61, 1, stone)     // the ledge beside it: its top at -60, two below the plateau's
	g := &Graph{World: w, Tags: ladderTags{}}
	start, ok := g.Start(0.5, -58, 0.5)
	if !ok {
		t.Fatal("no start on the plateau")
	}
	_, err := g.Find(start, world.BlockPos{X: 15, Y: -60, Z: 0}, 20000)
	var np *NoPathError
	if !errors.As(err, &np) || np.Limit {
		t.Fatalf("across the canyon: %v, want no way, every place searched", err)
	}
	if np.Closest.Pos.X != 4 || np.Closest.Pos.Y != -60 {
		t.Errorf("the nearest place reached %v, want the ledge's far edge (4 -60)", np.Closest.Pos)
	}
	if np.Back.Pos.X != 2 || np.Back.Pos.Y != -58 {
		t.Errorf("the nearest place to walk back from %v, want the plateau's edge (2 -58)", np.Back.Pos)
	}
	// with a way back up from the ledge (a step at its side), the ledge is Back too
	w.fill(3, -60, 2, 3, -60, 2, stone)
	w.fill(3, -70, 2, 3, -61, 2, stone)
	_, err = g.Find(start, world.BlockPos{X: 15, Y: -60, Z: 0}, 20000)
	if !errors.As(err, &np) || np.Back.Pos.X != 4 {
		t.Errorf("with a step back up: Back %v (%v), want the ledge", np.Back.Pos, err)
	}
}

// TestNoStepIntoAStreamOverADrop: two plateaus split by a gap, a canyon along
// their south side; a bridge at the canyon's lip with a stream on it (the
// water flowing, its south side open over the canyon, twelve deep). No way
// across but that stream: no path (a stream over a drop is no place to
// step, as lava is not). A dry bridge far north too: the path takes it, the
// longer way, never the stream (a bot crossed such a stream at a waterfall's
// lip to a tree, was carried, got out, and walked back in, 92 times).
func TestNoStepIntoAStreamOverADrop(t *testing.T) {
	stone := block.ToStateID[block.Stone{}]
	air := block.ToStateID[block.Air{}]
	stream := stateNamed(t, "minecraft:water[level=2]")
	w := testWorld{}
	w.fill(-10, -60, -25, 10, -59, 2, stone) // the plateaus: their top at -58
	w.fill(-1, -70, -25, 1, -59, 2, air)     // the gap between them, three wide
	w.fill(-10, -70, 3, 10, -59, 12, air)    // the canyon along the south
	w.fill(-1, -70, 2, 1, -59, 2, stone)     // the bridge at the lip
	w.fill(-1, -58, 2, 1, -58, 2, stream)
	g := &Graph{World: w, Tags: ladderTags{}}
	start, _ := g.Start(-5.5, -58, 1.5)
	goal := world.BlockPos{X: 5, Y: -58, Z: 1}
	if nodes, err := g.Find(start, goal, 50000); err == nil {
		t.Fatalf("across by the stream over the canyon: %v", nodes)
	}
	w.fill(-1, -70, -20, 1, -59, -20, stone) // a dry bridge, far
	nodes, err := g.Find(start, goal, 50000)
	if err != nil {
		t.Fatalf("by the dry bridge: %v", err)
	}
	dry := false
	for _, n := range nodes {
		if n.Pos.Z == 2 && n.Pos.X >= -1 && n.Pos.X <= 1 {
			t.Fatalf("through the stream over the canyon: %v", nodes)
		}
		dry = dry || n.Pos == (world.BlockPos{X: 0, Y: -58, Z: -20})
	}
	if !dry {
		t.Errorf("not by the dry bridge: %v", nodes)
	}
}
