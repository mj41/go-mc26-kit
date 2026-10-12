package path

import (
	"errors"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// A sealed room (stone round it, 3 high) the only way into which is a flooded passage of the given length,
// two blocks deep, with a stone roof: the head is under water the whole way. pockets are the x offsets of
// cells of the passage (from its start) that are air over the water: a place to breathe.
//
//	x = 3 … 3+n-1   the passage (z = 0, y = -60 and -59 water, -58 the roof)
//	x = 3+n … 3+n+3 the room (dry)
func floodedRoom(t *testing.T, n int, pockets ...[2]int) (testWorld, world.BlockPos) {
	t.Helper()
	stone := block.ToStateID[block.Stone{}]
	water := stateNamed(t, "minecraft:water[level=0]")
	air := block.ToStateID[block.Air{}]
	w := testWorld{}
	// (a wall round the whole, so that a search that finds no way has a small world to search: the ground is endless)
	x1 := 3 + n + 8
	w.fill(-6, -60, -10, x1, -58, -10, stone)
	w.fill(-6, -60, 10, x1, -58, 10, stone)
	w.fill(-6, -60, -10, -6, -58, 10, stone)
	w.fill(x1, -60, -10, x1, -58, 10, stone)
	w.fill(3, -60, -8, 3+n+4, -58, 8, stone)
	w.fill(3, -60, 0, 3+n-1, -59, 0, water)
	w.fill(3+n, -60, -3, 3+n+3, -59, 3, air)
	for _, p := range pockets {
		w.fill(3+p[0], -59, 0, 3+p[1], -59, 0, air) // the water's top cell: a body there has its head out
	}
	return w, world.BlockPos{X: 3 + n + 2, Y: -60, Z: 0}
}

// find plans from the dry ground outside the passage's mouth, with the given air.
func find(t *testing.T, w testWorld, goal world.BlockPos, air func() int) ([]Node, error) {
	t.Helper()
	g := &Graph{World: w, Tags: ladderTags{}, Air: air}
	start, ok := g.Start(0.5, -60, 0.5)
	if !ok {
		t.Fatal("no start")
	}
	return g.Find(start, goal, 30000)
}

// A flooded passage is planned through when the air lasts it, and refused when it does not: the way to a room
// that has no other way in. At 11 ticks a block the three quarters of a full breath (225 ticks) are 20 blocks.
func TestAirLongPassageIsRefused(t *testing.T) {
	for _, c := range []struct {
		name string
		n    int // the passage's length
		air  func() int
		ok   bool
	}{
		{"a short passage, full air", 8, nil, true},
		{"a passage the air lasts", 18, nil, true},
		{"a long passage, full air", 40, nil, false},
		{"a short passage, the air half gone", 8, func() int { return 150 }, true},
		{"a passage the half air does not last", 18, func() int { return 150 }, false},
		{"a short passage, nearly no air", 8, func() int { return 60 }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, goal := floodedRoom(t, c.n)
			path, err := find(t, w, goal, c.air)
			if c.ok {
				if err != nil {
					t.Fatalf("no way through a passage of %d: %v", c.n, err)
				}
				if last := path[len(path)-1].Pos; last != goal {
					t.Fatalf("the way ends at %v, want %v", last, goal)
				}
				return
			}
			if err == nil {
				t.Fatalf("a way through %d blocks of flooded passage (%s): %v", c.n, c.name, path)
			}
			var np *NoPathError
			if !errors.As(err, &np) || !np.Air {
				t.Fatalf("the refusal should say it is the air: %v", err)
			}
			if !strings.Contains(err.Error(), "under water too long") {
				t.Errorf("the reason is not in the message: %q", err)
			}
		})
	}
}

// Not every no-way is the air: a room shut in stone, passage and all, says no more than that.
func TestAirNoWayThatIsNotTheAir(t *testing.T) {
	w, goal := floodedRoom(t, 40)
	stone := block.ToStateID[block.Stone{}]
	w.fill(3+20, -60, 0, 3+20, -59, 0, stone) // the passage shut half way: no air would help
	_, err := find(t, w, goal, nil)
	var np *NoPathError
	if !errors.As(err, &np) || np.Air || strings.Contains(err.Error(), "under water") {
		t.Fatalf("a shut passage: %v (Air %v)", err, np != nil && np.Air)
	}
}

// A place to breathe on the way gives the air back: two stretches the air lasts one after the other, a
// pocket between them, are one way; the same length with no pocket is none.
func TestAirPocketIsBreathed(t *testing.T) {
	w, goal := floodedRoom(t, 36, [2]int{15, 20}) // 15 flooded, 6 with the head out, 15 flooded
	if _, err := find(t, w, goal, nil); err != nil {
		t.Fatalf("no way through a passage with a pocket to breathe in: %v", err)
	}
	w, goal = floodedRoom(t, 36)
	if _, err := find(t, w, goal, nil); err == nil {
		t.Fatal("a way through 36 blocks under water with no air")
	}
}

// walkAir drives a walker and the physics together as walk does, counting the air the walker says it has: the
// least it was, the status and where the player ended. air0, when set, is the air the walker begins with.
func walkAir(t *testing.T, w testWorld, from physics.Vec3, goal world.BlockPos, ticks, air0 int) (string, basic.Position, int) {
	t.Helper()
	bp := basic.NewPlayer(bot.NewClient(), basic.DefaultSettings, basic.EventsListener{})
	bp.SetPosition(basic.Position{X: from.X, Y: from.Y, Z: from.Z})
	walker := &Walker{Graph: &Graph{World: w, Tags: ladderTags{}}, MaxNodes: 30000}
	if air0 > 0 {
		walker.air, walker.airSeen = air0, true
	}
	mover := &physics.Mover{World: w, Tags: ladderTags{}}
	var s control.State
	walker.Goto(goal)
	least := FullAir
	for range ticks {
		walker.Think(&s, bp)
		walker.waitSearch()
		mover.Step(&s, bp)
		least = min(least, walker.Air())
		if st := walker.Status(); st != "walking" {
			return st, bp.Position(), least
		}
	}
	return walker.Status(), bp.Position(), least
}

// The walker swims a passage the air lasts, with air to spare at the end; one it does not last it never
// enters: the walk fails, the reason the air, the player still on the dry ground with all its air.
func TestWalkThroughAFloodedPassage(t *testing.T) {
	w, goal := floodedRoom(t, 12)
	st, p, least := walkAir(t, w, physics.Vec3{X: 0.5, Y: -60, Z: 0.5}, goal, 1500, 0)
	if st != "arrived" {
		t.Fatalf("the walk through 12 blocks of water: %s at %+v (air %d)", st, p, least)
	}
	if least < 100 {
		t.Errorf("the air went down to %d ticks on a swim of 12 blocks", least)
	}

	w, goal = floodedRoom(t, 40)
	st, p, least = walkAir(t, w, physics.Vec3{X: 0.5, Y: -60, Z: 0.5}, goal, 1500, 0)
	if !strings.HasPrefix(st, "failed:") || !strings.Contains(st, "under water too long") {
		t.Fatalf("the walk through 40 blocks of water: %q at %+v", st, p)
	}
	if p.X > 2.5 || least != FullAir {
		t.Errorf("it went on (x %.2f, the air down to %d): the refusal should come before the first stroke", p.X, least)
	}
}

// A body already in the water with little air plans from its own: out by the near mouth of the passage, ten
// blocks back, which its air lasts; the room 20 blocks on, which it does not.
func TestWalkUnderWaterWithLittleAir(t *testing.T) {
	w, room := floodedRoom(t, 30)
	from := physics.Vec3{X: 11.5, Y: -60, Z: 0.5} // eight blocks in
	out := world.BlockPos{X: 1, Y: -60, Z: 0}
	st, p, least := walkAir(t, w, from, out, 1500, 150)
	if st != "arrived" {
		t.Fatalf("out of the passage by the near mouth: %s at %+v (air %d)", st, p, least)
	}
	if least < 20 {
		t.Errorf("the air went down to %d on the way out: it should have kept some", least)
	}
	st, p, _ = walkAir(t, w, from, room, 1500, 150)
	if !strings.Contains(st, "under water too long") {
		t.Errorf("the room twenty blocks on, with 150 ticks of air: %q at %+v, want the air said", st, p)
	}
}
