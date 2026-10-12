package screen

import (
	"slices"
	"testing"

	"github.com/mj41/go-mc26/data/registryid"

	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// TestOpenReplacesTheOldMenu: a crafting table left open (never closed by
// the bot), then the server opens a furnace: only the furnace is open — Open
// never gives the old menu's slots for the new one.
func TestOpenReplacesTheOldMenu(t *testing.T) {
	m := &Manager{Screens: map[int]Container{}}
	m.Screens[0] = &m.Inventory
	open := func(id int, typ int32) {
		t.Helper()
		p := pk.Marshal(play.OpenScreen{}.PacketID(), play.OpenScreen{ContainerID: pk.VarInt(id), Type: pk.VarInt(typ)})
		if err := m.onOpenScreen(p); err != nil {
			t.Fatal(err)
		}
	}
	open(1, int32(slices.Index(registryid.Menu, "minecraft:crafting")))
	open(2, int32(slices.Index(registryid.Menu, "minecraft:furnace")))
	if _, ok := m.Screens[1]; ok {
		t.Fatalf("the crafting table still open beside the furnace: %v", m.Screens)
	}
	for i := 0; i < 20; i++ { // the map's order is random: every time
		if got, ok := m.Open(); !ok || got.ID != 2 {
			t.Fatalf("Open: %v %v; want the furnace (2)", got.ID, ok)
		}
	}
}
