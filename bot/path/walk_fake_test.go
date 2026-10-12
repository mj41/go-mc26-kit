package path_test

import (
	"math"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/world"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// TestWalkOnFakeWorld: a walk to a place three off, a block in the way to
// step up onto: the walker's path, the physics' moves, on the fake server's
// ground — arrived, on the block's top.
func TestWalkOnFakeWorld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.World.Set(2, 64, 0, "minecraft:stone")
		s.World.Set(3, 64, 0, "minecraft:stone")
		b := fakeserver.Join(t, s, "Tester")
		if err := b.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		b.Walker.Goto(world.BlockPos{X: 3, Y: 65, Z: 0})
		for t0 := time.Now(); ; time.Sleep(20 * time.Millisecond) {
			st := b.Walker.Status()
			if st == "arrived" {
				break
			}
			if len(st) > 6 && st[:6] == "failed" || time.Since(t0) > 4*time.Second {
				t.Fatalf("the walk: %q, at %+v", st, b.Player.Position())
			}
		}
		p := b.Player.Position()
		if math.Floor(p.X) != 3 || math.Abs(p.Y-65) > 0.01 {
			t.Fatalf("arrived at %.2f %.2f %.2f; want on the stone at 3 65 0", p.X, p.Y, p.Z)
		}
	})
}

// TestWalkOnFakeWorldStepped: the same walk on a stepped clock — the ticks
// the test gives it, not the wall's; arrived in as many ticks as it took.
func TestWalkOnFakeWorldStepped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.World.Set(2, 64, 0, "minecraft:stone")
		s.World.Set(3, 64, 0, "minecraft:stone")
		b := fakeserver.JoinStepped(t, s, "Tester")
		if err := b.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		b.Walker.Goto(world.BlockPos{X: 3, Y: 65, Z: 0})
		if !b.Until(400, func() bool { return b.Walker.Status() == "arrived" }) {
			t.Fatalf("the walk: %q, at %+v", b.Walker.Status(), b.Player.Position())
		}
		p := b.Player.Position()
		if math.Floor(p.X) != 3 || math.Abs(p.Y-65) > 0.01 {
			t.Fatalf("arrived at %.2f %.2f %.2f; want on the stone at 3 65 0", p.X, p.Y, p.Z)
		}
	})
}

// TestWalkToAGoalBehindAWall: a goal inside a ring of stone three high (no
// way in on foot): the walk fails having searched every place reachable,
// and says the place reached nearest the goal — outside the ring, beside it;
// a walk there ends at the wall.
func TestWalkToAGoalBehindAWall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.World.Fill(8, 64, -2, 12, 66, 2, "minecraft:stone") // the ring: a block of stone…
		s.World.Fill(9, 64, -1, 11, 66, 1, "minecraft:air")   // …hollowed
		b := fakeserver.Join(t, s, "Tester")
		if err := b.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		b.Walker.Goto(world.BlockPos{X: 10, Y: 64, Z: 0})
		for t0 := time.Now(); ; time.Sleep(20 * time.Millisecond) {
			if st := b.Walker.Status(); len(st) > 6 && st[:6] == "failed" {
				break
			}
			if time.Since(t0) > 5*time.Second {
				t.Fatalf("the walk into the ring: %q", b.Walker.Status())
			}
		}
		np, ok := b.Walker.NoPath()
		if !ok || np.Limit {
			t.Fatalf("no complete search said: %+v %v", np, ok)
		}
		c := np.Closest.Pos
		if c.X != 7 || c.Y != 64 || c.Z < -1 || c.Z > 1 {
			t.Fatalf("the place nearest the goal: %v, want beside the ring's west wall (7 64 -1..1)", c)
		}
		b.Walker.Goto(c)
		for t0 := time.Now(); b.Walker.Status() != "arrived"; time.Sleep(20 * time.Millisecond) {
			if time.Since(t0) > 5*time.Second {
				t.Fatalf("the walk to the wall: %q at %+v", b.Walker.Status(), b.Player.Position())
			}
		}
		if _, ok := b.Walker.NoPath(); ok {
			t.Fatal("a walk that arrived still says it found no way")
		}
	})
}

// A real (not stepped) walker searches while block updates arrive on the connection's reader goroutine and its
// readers (Status, NoPath, Goto) call in: under -race this is the proof that what the search copies is complete
// (the start, the goal, the limit, the graph) and that it reads the world only through the world's own lock
// (SEARCH.md). Each walk is to a goal shut in stone, so it searches every place it can reach; the walk fails.
func TestSearchWhileTheWorldChanges(t *testing.T) {
	s := fakeserver.Start(t)
	s.World.Fill(20, 64, -2, 22, 66, 2, "minecraft:stone")
	s.World.Set(21, 64, 0, "minecraft:air")
	s.World.Set(21, 65, 0, "minecraft:air")
	b := fakeserver.Join(t, s, "Racer")
	if err := b.Loaded(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	b.Walker.MaxNodes = 1500 // a search that ends in under a second under -race, many updates in it
	stone, air := fakeserver.State("minecraft:stone"), fakeserver.State("minecraft:air")
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // block updates, as fast as the connection takes them, round the player and the goal
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			state := stone
			if i%2 == 1 {
				state = air
			}
			_ = s.Send(play.BlockUpdate{Pos: pk.Position{X: -8 + i%17, Y: 64, Z: -8 + i/17%17}, BlockState: pk.VarInt(state)})
		}
	}()
	go func() { // the readers
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = b.Walker.Status()
			_, _ = b.Walker.NoPath()
			time.Sleep(time.Millisecond)
		}
	}()
	failed := 0
	goal := world.BlockPos{X: 21, Y: 64, Z: 0}
	for i := 0; i < 3; i++ {
		b.Walker.Goto(goal)
		// a caller that asks each round: the same goal again keeps the search, and writes the job under the lock
		for t0 := time.Now(); b.Walker.Status() == "walking"; time.Sleep(2 * time.Millisecond) {
			if time.Since(t0) > 20*time.Second {
				close(stop)
				wg.Wait()
				t.Fatalf("walk %d: still searching after 20 s", i)
			}
			b.Walker.Goto(goal)
		}
		if st := b.Walker.Status(); strings.HasPrefix(st, "failed: path: no way") {
			failed++
		} else {
			t.Errorf("walk %d: %q, want a failed search", i, st)
		}
	}
	close(stop)
	wg.Wait()
	if failed != 3 {
		t.Errorf("%d of 3 searches failed", failed)
	}
}
