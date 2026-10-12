package act_test

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// stack is n of the item, as a slot holds them.
func stack(item string, n int) types.ItemStack {
	return types.ItemStack{SlotData: component.SlotData{Count: pk.VarInt(n), Item: pk.VarInt(slices.Index(registryid.Item, item))}}
}

// contents is a menu's slots: its own first (n of them, empty), then the
// player's 27 and its hotbar's 9 — raw iron, coal and dirt in the player's.
func contents(own int) []types.ItemStack {
	s := make([]types.ItemStack, own+36)
	s[own+0] = stack("minecraft:raw_iron", 6)
	s[own+1] = stack("minecraft:coal", 2)
	s[own+7] = stack("minecraft:dirt", 6) // where a crafting table's layout has the raw iron in a furnace's
	return s
}

// TestLoadAfterMenusLeftOpen: crafting tables the bot never closed, then a
// furnace the server opens: Load puts the raw iron in, from the furnace's
// slots — not a crafting table's (whose raw iron is at the furnace's dirt).
// Before kit 9dcd2e7 the menus left open stayed beside the furnace and Load's
// clicks were made by one of their layouts: dirt went in for raw iron.
func TestLoadAfterMenusLeftOpen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		b := fakeserver.Join(t, s, "Tester")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := b.Ctl.WaitTicks(ctx, 1); err != nil {
			t.Fatalf("not ticking: %v", err)
		}
		crafting, furnace := slices.Index(registryid.Menu, "minecraft:crafting"), slices.Index(registryid.Menu, "minecraft:furnace")
		// four tables opened, never closed (a craft stopped with its screen open);
		// the furnace last: the server's screen now
		for id := 3; id <= 6; id++ {
			send(t, s, play.OpenScreen{ContainerID: pk.VarInt(id), Type: pk.VarInt(crafting), Title: types.Text(chat.Text("Crafting"))})
			send(t, s, play.ContainerSetContent{ContainerID: pk.VarInt(id), StateID: 1, Items: contents(10)})
		}
		send(t, s, play.OpenScreen{ContainerID: 2, Type: pk.VarInt(furnace), Title: types.Text(chat.Text("Furnace"))})
		send(t, s, play.ContainerSetContent{ContainerID: 2, StateID: 2, Items: contents(3)})
		for t0 := time.Now(); ; {
			if m, ok := b.Screens.Open(); ok && m.ID == 2 && m.PlayerStart() == 3 {
				break
			}
			if time.Since(t0) > 2*time.Second {
				t.Fatal("the furnace never open with its contents")
			}
			time.Sleep(5 * time.Millisecond)
		}

		if err := b.Act.Load(ctx, "minecraft:raw_iron", 6, "minecraft:coal", 1); err != nil {
			t.Fatalf("Load: %v", err)
		}
		clicks := s.Got(packetid.ServerboundPlayContainerClick)
		if len(clicks) == 0 {
			t.Fatal("Load clicked nothing")
		}
		// the raw iron picked up from where the furnace has it (slot 3), put in
		// its input (slot 0); the coal from slot 4 into its fuel (slot 1)
		var picked []int
		for _, c := range clicks {
			var cl play.ContainerClick
			if err := c.Scan(&cl); err != nil {
				t.Fatal(err)
			}
			if cl.ContainerID != 2 {
				t.Fatalf("a click in container %d, not the furnace (2)", cl.ContainerID)
			}
			picked = append(picked, int(cl.SlotNum))
		}
		if picked[0] != 3 || !slices.Contains(picked, 0) || !slices.Contains(picked, 4) || slices.Contains(picked, 10) {
			t.Fatalf("Load's clicks on slots %v; want the raw iron from 3 into 0, the coal from 4, never 10 (the dirt)", picked)
		}
	})
}

func send(t *testing.T, s *fakeserver.Server, p fakeserver.Clientbound) {
	t.Helper()
	if err := s.Send(p); err != nil {
		t.Fatal(err)
	}
}

// TestLoadBeforeTheContentsCame: Load called as the server opens the furnace,
// its contents a moment behind (a busy server): the raw iron is put in from
// the furnace's own slot (3), not by the inventory menu's layout, which
// left the furnace with its coal alone (botprobe's smelt, round 4).
func TestLoadBeforeTheContentsCame(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		b := fakeserver.Join(t, s, "Tester")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := b.Ctl.WaitTicks(ctx, 1); err != nil {
			t.Fatalf("not ticking: %v", err)
		}
		furnace := slices.Index(registryid.Menu, "minecraft:furnace")
		send(t, s, play.OpenScreen{ContainerID: 2, Type: pk.VarInt(furnace), Title: types.Text(chat.Text("Furnace"))})
		for t0 := time.Now(); ; time.Sleep(5 * time.Millisecond) {
			if m, ok := b.Screens.Open(); ok && m.ID == 2 {
				break
			}
			if time.Since(t0) > 2*time.Second {
				t.Fatal("the furnace never open")
			}
		}
		go func() {
			time.Sleep(150 * time.Millisecond) // the contents a few ticks behind
			_ = s.Send(play.ContainerSetContent{ContainerID: 2, StateID: 2, Items: contents(3)})
		}()
		if err := b.Act.Load(ctx, "minecraft:raw_iron", 6, "minecraft:coal", 1); err != nil {
			t.Fatalf("Load: %v", err)
		}
		clicks := s.Got(packetid.ServerboundPlayContainerClick)
		if len(clicks) == 0 {
			t.Fatal("Load clicked nothing")
		}
		var first play.ContainerClick
		if err := clicks[0].Scan(&first); err != nil {
			t.Fatal(err)
		}
		if first.ContainerID != 2 || first.SlotNum != 3 {
			t.Fatalf("the first click: container %d slot %d, want the raw iron's slot 3 of the furnace (2)", first.ContainerID, first.SlotNum)
		}
	})
}
