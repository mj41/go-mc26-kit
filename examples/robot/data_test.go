package main

import (
	"slices"
	"testing"
)

// TestData: the kinds the robot goes by, from the game's data.
func TestData(t *testing.T) {
	for _, f := range []string{"minecraft:bread", "minecraft:apple", "minecraft:cooked_beef", "minecraft:carrot"} {
		if !slices.Contains(foods, f) {
			t.Errorf("%s is not food", f)
		}
	}
	for _, f := range []string{"minecraft:rotten_flesh", "minecraft:spider_eye", "minecraft:chorus_fruit", "minecraft:chicken", "minecraft:pufferfish"} {
		if slices.Contains(foods, f) {
			t.Errorf("%s is food: eating it does more", f)
		}
	}
	if i, j := slices.Index(foods, "minecraft:cooked_beef"), slices.Index(foods, "minecraft:apple"); i > j {
		t.Errorf("an apple (%d) before cooked beef (%d)", j, i)
	}
	for typ, want := range map[string]bool{
		"minecraft:zombie": true, "minecraft:creeper": true, "minecraft:skeleton": true,
		"minecraft:enderman": false, "minecraft:cow": false, "minecraft:item": false,
	} {
		if hostileType(typ) != want {
			t.Errorf("hostile %s = %v", typ, !want)
		}
	}
	if !gameType("minecraft:cow") || gameType("minecraft:zombie") {
		t.Error("game: a cow is, a zombie is not")
	}
}
