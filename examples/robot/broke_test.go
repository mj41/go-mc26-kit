package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// The server's word that the tool in the main hand broke (entity event 47) is a `broke` event with the item it
// held, read when the word arrives; event 48 says the off hand, and an empty hand an empty item.
func TestBrokeIsTold(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		r, g := simRobot(t, simworld.Options{View: 3, Setup: []string{"give Robot minecraft:stone_pickaxe"}})
		path := filepath.Join(t.TempDir(), "events.jsonl")
		l, err := openEvents(path, "test")
		if err != nil {
			t.Fatal(err)
		}
		events = l
		t.Cleanup(func() { events = nil })
		held := func() string {
			r.screens.Lock()
			defer r.screens.Unlock()
			s := r.screens.Inventory.Slots[36+r.screens.HeldSlot]
			if s.Count == 0 {
				return ""
			}
			return itemName(int32(s.Item))
		}
		if !g.Until(400, func() bool { return r.ctl.State().Loaded && held() == "minecraft:stone_pickaxe" }) {
			t.Fatalf("not in the game with the pickaxe in hand: %q", held())
		}
		send := func(ev int8) {
			t.Helper()
			if err := g.S.Send(play.EntityEvent{EntityID: pk.Int(r.player.Login.PlayerID), EventID: pk.Byte(ev)}); err != nil {
				t.Fatal(err)
			}
			g.Step(4)
		}
		send(47) // main hand: the stone pickaxe
		send(48) // off hand: empty
		send(9)  // some other entity event: nothing
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var broke []string
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, `"name":"broke"`) {
				broke = append(broke, line)
			}
		}
		if len(broke) != 2 ||
			!strings.Contains(broke[0], `"hand":"main"`) || !strings.Contains(broke[0], `"item":"minecraft:stone_pickaxe"`) ||
			!strings.Contains(broke[1], `"hand":"off"`) || !strings.Contains(broke[1], `"item":""`) {
			t.Errorf("the broke events: %q", broke)
		}
	})
}
