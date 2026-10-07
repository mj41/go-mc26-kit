package main

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
)

func slotOf(t *testing.T, name string) screen.Slot {
	t.Helper()
	for id, n := range registryid.Item {
		if n == name {
			var s screen.Slot
			s.Item, s.Count = pk.VarInt(id), 1
			return s
		}
	}
	t.Fatalf("no item %s", name)
	return screen.Slot{}
}

// TestWeaponScore: damage a second — an iron sword over a stone axe (it hits
// harder, half as often), anything over the fist only when it does more.
func TestWeaponScore(t *testing.T) {
	sword, axe, hand := weaponScore(slotOf(t, "minecraft:iron_sword")), weaponScore(slotOf(t, "minecraft:stone_axe")), weaponScore(screen.Slot{})
	if !(sword > axe && axe > hand) {
		t.Errorf("iron sword %.1f, stone axe %.1f, hand %.1f: want sword > axe > hand", sword, axe, hand)
	}
	if hand != 4 {
		t.Errorf("the hand: %.1f, want 1 x 4", hand)
	}
}
