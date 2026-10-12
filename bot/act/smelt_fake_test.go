package act_test

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// openFurnace has the server open a furnace, the raw iron and coal in the
// player's part, and waits till the bot has it open.
func openFurnace(t *testing.T, s *fakeserver.Server, b *fakeserver.Bot) {
	t.Helper()
	furnace := slices.Index(registryid.Menu, "minecraft:furnace")
	send(t, s, play.OpenScreen{ContainerID: 2, Type: pk.VarInt(furnace), Title: types.Text(chat.Text("Furnace"))})
	send(t, s, play.ContainerSetContent{ContainerID: 2, StateID: 1, Items: contents(3)})
	for t0 := time.Now(); ; time.Sleep(5 * time.Millisecond) {
		if m, ok := b.Screens.Open(); ok && m.ID == 2 && m.PlayerStart() == 3 {
			return
		}
		if time.Since(t0) > 2*time.Second {
			t.Fatal("the furnace never open")
		}
	}
}

// TestSmeltOnASlowServer: a server running at a third of the game's speed
// (the bench's in its busiest minute) cooks the ingot in three times the
// game's 200 ticks of the client — the furnace's progress going up all the
// while. Smelt waits for it, as long as the furnace works: before, it gave up
// after the client's n×200+200 ticks ("the furnace did not make enough") with
// the iron still cooking.
func TestSmeltOnASlowServer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		b := fakeserver.Join(t, s, "Tester")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := b.Ctl.WaitTicks(ctx, 1); err != nil {
			t.Fatal(err)
		}
		openFurnace(t, s, b)
		tick := fakeserver.FastTick
		go func() {
			// the server's furnace: cooking progress 0..200, a server tick every three of the client's
			for p := 0; p <= 200 && ctx.Err() == nil; p += 2 {
				_ = s.Send(play.ContainerSetData{ContainerID: 2, ID: 2, Value: pk.Short(p)})
				_ = s.Send(play.ContainerSetData{ContainerID: 2, ID: 0, Value: pk.Short(1600 - p)}) // lit
				time.Sleep(6 * tick)                                                                // two server ticks, at a third of the speed
			}
			_ = s.Send(play.ContainerSetSlot{ContainerID: 2, StateID: 2, Slot: 2, ItemStack: stack("minecraft:iron_ingot", 1)})
		}()
		got, err := b.Act.Smelt(ctx, "minecraft:raw_iron", "minecraft:coal", 1)
		if err != nil || got != 1 {
			t.Fatalf("Smelt on a slow server: %d, %v; want the ingot", got, err)
		}
	})
}

// TestSmeltStalled: a furnace that does not work (no progress, not lit) is
// given up on — it says so, not waiting for ever.
func TestSmeltStalled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		b := fakeserver.Join(t, s, "Tester")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := b.Ctl.WaitTicks(ctx, 1); err != nil {
			t.Fatal(err)
		}
		openFurnace(t, s, b)
		if _, err := b.Act.Smelt(ctx, "minecraft:raw_iron", "minecraft:coal", 1); err == nil {
			t.Fatal("a furnace not working: Smelt said nothing")
		}
	})
}
