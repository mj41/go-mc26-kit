package act_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// A dig the client predicts and the server does not take: the server answers
// with the sequence's ack and no block update, and the client's world must
// show the server's block again at the ack, as the game's client does
// (BlockStatePredictionHandler: the server's state kept for each predicted
// block, set again when the ack comes). A block left as the client guessed it
// is a ghost: air in the bot's world, solid in the server's — its walks
// pressed against a face its path finder sees open.

// ackOnly answers the bot's dig action with only its ack: the server's world
// unchanged.
func ackOnly(s *fakeserver.Server, action int32) func(fakeserver.Packet) {
	return func(p fakeserver.Packet) {
		var a play.PlayerAction
		if p.ID != a.PacketID() || p.Scan(&a) != nil || int32(a.Action) != action {
			return
		}
		_ = s.Send(play.BlockChangedAck{Sequence: a.Sequence})
	}
}

// predictedBack digs the block at pos, the server answering as ackOnly, and
// waits for the bot's world to show want there again.
func predictedBack(t *testing.T, s *fakeserver.Server, b *fakeserver.Bot, pos world.BlockPos, want string) {
	t.Helper()
	done := make(chan act.Dig, 1)
	b.Hands.Dig(pos, func(d act.Dig) { done <- d })
	select {
	case d := <-done:
		if d.Err != nil {
			t.Fatalf("dig: %v", d.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the dig never ended")
	}
	for t0 := time.Now(); ; time.Sleep(5 * time.Millisecond) {
		if got, _ := b.World.BlockAt(pos); int(got) == int(fakeserver.State(want)) {
			return
		}
		if time.Since(t0) > 2*time.Second {
			got, _ := b.World.BlockAt(pos)
			t.Fatalf("the bot's world has %v at %v after the server's ack, the server %s: a ghost", got, pos, want)
		}
	}
}

// TestDigNotTakenByServer: dirt dug by hand, the finish sent; the server
// (whose ticks counted less than 0.7 of the dig: it breaks it later, or never)
// acks it and keeps the dirt.
func TestDigNotTakenByServer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.World.Set(1, 64, 0, "minecraft:dirt")
		s.Hook = ackOnly(s, int32(types.PlayerActionActionStopDestroyBlock))
		b := fakeserver.Join(t, s, "Tester")
		if err := b.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		predictedBack(t, s, b, world.BlockPos{X: 1, Y: 64, Z: 0}, "minecraft:dirt")
	})
}

// TestInstantBreakNotTakenByServer: grass the client breaks at once (its
// start the whole dig); the server, finding it no instant break, acks the
// start and keeps it.
func TestInstantBreakNotTakenByServer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.World.Set(1, 64, 0, "minecraft:short_grass")
		s.Hook = ackOnly(s, int32(types.PlayerActionActionStartDestroyBlock))
		b := fakeserver.Join(t, s, "Tester")
		if err := b.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		predictedBack(t, s, b, world.BlockPos{X: 1, Y: 64, Z: 0}, "minecraft:short_grass")
	})
}
