package act_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/packetid"
)

// TestWorldSeen: the bot's world is the fake server's, block by block — the
// flat ground and a log set by the test.
func TestWorldSeen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.World.Set(2, 64, 0, "minecraft:oak_log")
		b := fakeserver.Join(t, s, "Tester")
		if err := b.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			x, y, z int
			name    string
		}{{2, 64, 0, "minecraft:oak_log"}, {0, 63, 0, "minecraft:grass_block"}, {5, 62, 5, "minecraft:dirt"}, {-20, 40, 17, "minecraft:stone"}, {0, 64, 0, ""}} {
			got, ok := b.World.BlockAt(world.BlockPos{X: c.x, Y: c.y, Z: c.z})
			want := fakeserver.State(orAir(c.name))
			if !ok || int(got) != int(want) {
				t.Errorf("block at %d %d %d: %v (%v), want %s", c.x, c.y, c.z, got, ok, orAir(c.name))
			}
		}
	})
}

func orAir(n string) string {
	if n == "" {
		return "minecraft:air"
	}
	return n
}

// TestDigOnFakeWorld: a block of dirt beside it dug by hand: the dig sent
// (started, finished), the server's answer (the block gone) seen by its world.
func TestDigOnFakeWorld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.Digs = true
		s.World.Set(1, 64, 0, "minecraft:dirt")
		b := fakeserver.Join(t, s, "Tester")
		if err := b.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		at := world.BlockPos{X: 1, Y: 64, Z: 0}
		done := make(chan act.Dig, 1)
		b.Hands.Dig(at, func(d act.Dig) { done <- d })
		var d act.Dig
		select {
		case d = <-done:
			if d.Err != nil {
				t.Fatalf("dig: %v", d.Err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the dig never ended")
		}
		t.Logf("dug in %d ticks, %d of them off the ground", d.Ticks, d.Airborne)
		// the client predicts the block gone and ends its dig as it sends the
		// finish: the server's side is waited for — the finish come, its world
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := s.WaitFor(ctx, packetid.ServerboundPlayPlayerAction, 2); err != nil {
			t.Fatal(err)
		}
		if got := s.World.At(1, 64, 0); got != fakeserver.State("minecraft:air") {
			t.Fatalf("the dirt still there in the server's world: %v", got)
		}
		if got, _ := b.World.BlockAt(at); int(got) != int(fakeserver.State("minecraft:air")) {
			t.Fatalf("the dirt still there in the bot's world: %v", got)
		}
	})
}

// TestDigOnFakeWorldStepped: the same dig on a stepped clock — as long in
// ticks as on the wall's, the server's answer seen by the bot's world.
func TestDigOnFakeWorldStepped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.Digs = true
		s.World.Set(1, 64, 0, "minecraft:dirt")
		b := fakeserver.JoinStepped(t, s, "Tester")
		if err := b.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		at := world.BlockPos{X: 1, Y: 64, Z: 0}
		done := make(chan act.Dig, 1)
		b.Hands.Dig(at, func(d act.Dig) { done <- d })
		var d act.Dig
		ended := b.Until(200, func() bool {
			select {
			case d = <-done:
				return true
			default:
				return false
			}
		})
		if !ended || d.Err != nil {
			t.Fatalf("the dig: ended %v, %v", ended, d.Err)
		}
		b.Until(20, func() bool { return s.World.At(1, 64, 0) == fakeserver.State("minecraft:air") })
		if got := s.World.At(1, 64, 0); got != fakeserver.State("minecraft:air") {
			t.Fatalf("the dirt still there in the server's world: %v", got)
		}
		t.Logf("dug in %d ticks, %d of them off the ground", d.Ticks, d.Airborne)
	})
}
