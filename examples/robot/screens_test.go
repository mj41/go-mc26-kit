package main

import (
	"encoding/json"
	"testing"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/types"
)

func itemID(t *testing.T, name string) pk.VarInt {
	for i, n := range registryid.Item {
		if n == name {
			return pk.VarInt(i)
		}
	}
	t.Fatalf("no item %s", name)
	return 0
}

func stack(t *testing.T, name string, n int) screen.Slot {
	var s screen.Slot
	s.Item, s.Count = itemID(t, name), pk.VarInt(n)
	return s
}

// TestScreenEvent: an open crafting table as the screen event tells it —
// the slots with something in them, the cursor, the clicks made in it (not
// another menu's), the block it was opened from; closed, only that; the
// inventory while crafting by hand.
func TestScreenEvent(t *testing.T) {
	r := &robot{screens: screen.NewManager(bot.NewClient(), screen.EventsListener{})}
	if b, _ := json.Marshal(r.screenEvent(nil, false)); string(b) != `{"open":false}` {
		t.Fatalf("nothing open: %s", b)
	}
	slots := make([]screen.Slot, 46)
	slots[0] = stack(t, "minecraft:stick", 4)
	slots[2] = stack(t, "minecraft:oak_planks", 1)
	slots[5] = stack(t, "minecraft:oak_planks", 1)
	slots[37] = stack(t, "minecraft:oak_planks", 30)
	r.screens.Screens[3] = &screen.Menu{ID: 3, Type: "minecraft:crafting", Slots: slots}
	r.screens.Cursor = stack(t, "minecraft:stick", 4)
	noteOpened(struct{ X, Y, Z int }{1, 64, -2})
	e := r.screenEvent([]screen.Clicked{{ID: 3, Slot: -1, Recipe: "minecraft:stick"}, {ID: 3, Slot: 0, Mode: types.ContainerInputPickup}, {ID: 0, Slot: 9}}, false)
	b, _ := json.Marshal(e)
	want := `{"block":{"x":1,"y":64,"z":-2},"clicks":[{"recipe":"minecraft:stick"},{"button":0,"mode":"pickup","slot":0}],"cursor":["minecraft:stick",4],"id":3,"open":true,"size":46,"slots":[[0,"minecraft:stick",4],[2,"minecraft:oak_planks",1],[5,"minecraft:oak_planks",1],[37,"minecraft:oak_planks",30]],"type":"minecraft:crafting"}`
	if string(b) != want {
		t.Errorf("crafting table:\n got %s\nwant %s", b, want)
	}
	delete(r.screens.Screens, 3)
	r.screens.Cursor = screen.Slot{}
	r.screens.Inventory.Slots[36] = stack(t, "minecraft:oak_log", 3)
	e = r.screenEvent(nil, true)
	if e["type"] != "inventory" || e["id"] != 0 || e["size"] != 46 {
		t.Errorf("crafting by hand: %v", e)
	}
	if s, _ := json.Marshal(e["slots"]); string(s) != `[[36,"minecraft:oak_log",3]]` {
		t.Errorf("crafting by hand: slots %s", s)
	}
}
