package act_test

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// placeAnswered places dirt beside the bot, the server answering by answer
// (the bot's UseItemOn read), and gives the place's result.
func placeAnswered(t *testing.T, answer func(s *fakeserver.Server, seq int32)) act.Use {
	t.Helper()
	s := fakeserver.StartMem(t)
	s.Hook = func(p fakeserver.Packet) {
		var u play.UseItemOn
		if p.ID != packetid.ServerboundPlayUseItemOn || p.Scan(&u) != nil {
			return
		}
		go answer(s, int32(u.Sequence))
	}
	b := fakeserver.Join(t, s, "Tester")
	if err := b.Loaded(3 * time.Second); err != nil {
		t.Fatal(err)
	}
	inv := make([]types.ItemStack, 46)
	inv[36] = stack("minecraft:dirt", 8) // the hotbar's first slot, held
	send(t, s, play.ContainerSetContent{ContainerID: 0, StateID: 1, Items: inv})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Ctl.WaitTicks(ctx, 2); err != nil {
		t.Fatal(err)
	}
	done := make(chan act.Use, 1)
	b.Hands.Place(world.BlockPos{X: 1, Y: 64, Z: 0}, func(u act.Use) { done <- u })
	select {
	case u := <-done:
		return u
	case <-ctx.Done():
		t.Fatal("the place never ended")
	}
	return act.Use{}
}

// TestPlaceAnsweredLate: a busy server places the block after 20 of the
// client's ticks: the place is done when its block comes, not refused at the
// 20th tick (botprobe's hut, round 5).
func TestPlaceAnsweredLate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		u := placeAnswered(t, func(s *fakeserver.Server, seq int32) {
			time.Sleep(300 * time.Millisecond) // 30 of the fast client's ticks
			_ = s.SetBlock(1, 64, 0, "minecraft:dirt")
			_ = s.Send(play.BlockChangedAck{Sequence: pk.VarInt(seq)})
		})
		if u.Err != nil {
			t.Fatalf("a place answered late: %v", u.Err)
		}
	})
}

// TestPlaceRefused: a place the server acks with no block change is refused
// at the ack, said so.
func TestPlaceRefused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		t0 := time.Now()
		u := placeAnswered(t, func(s *fakeserver.Server, seq int32) {
			_ = s.Send(play.BlockChangedAck{Sequence: pk.VarInt(seq)})
		})
		if u.Err == nil || !strings.Contains(u.Err.Error(), "did not change") {
			t.Fatalf("a place refused: %v", u.Err)
		}
		if d := time.Since(t0); d > 3*time.Second {
			t.Fatalf("refused only after %v", d)
		}
	})
}
