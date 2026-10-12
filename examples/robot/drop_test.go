package main

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// column is a world of one column of blocks: at gives the state of a block, air where none is set.
func column(t *testing.T, blocks map[int]string) func(world.BlockPos) (block.StateID, bool) {
	air := stateOf(t, "minecraft:air")
	return func(p world.BlockPos) (block.StateID, bool) {
		if name, ok := blocks[p.Y]; ok {
			return stateOf(t, name), true
		}
		return air, true
	}
}

// TestDropLanding: the days run of 2026-10-11 stood on a lone block of a cliff ten over the water it was sent to
// and dug sideways for good: the fall that is a good one is told apart from the others.
func TestDropLanding(t *testing.T) {
	for _, c := range []struct {
		name   string
		blocks map[int]string // by y; the ledge's feet at 72, the cell off its edge under it at 71
		health float32
		land   int // the landing's y (feet), 0: none
		hurt   float32
	}{
		{"into the pond at the cliff's foot, whatever the height", map[int]string{62: "minecraft:water[level=0]", 61: "minecraft:stone"}, 8, 62, 0},
		{"onto stone, ten down, health to spare: 7 hurt", map[int]string{61: "minecraft:stone"}, 15, 62, 7},
		{"onto stone, ten down, too little health: it would be left with less than seven", map[int]string{61: "minecraft:stone"}, 12, 0, 0},
		{"three down: no hurt, any health", map[int]string{68: "minecraft:stone"}, 3, 69, 0},
		{"onto lava: never", map[int]string{62: "minecraft:lava[level=0]", 61: "minecraft:stone"}, 20, 0, 0},
		{"onto a cactus: never", map[int]string{70: "minecraft:cactus[age=0]", 69: "minecraft:sand"}, 20, 0, 0},
		{"nothing within reach under it: a void", map[int]string{}, 20, 0, 0},
	} {
		land, hurt, ok := dropLanding(column(t, c.blocks), 5, 72, 5, c.health)
		if c.land == 0 {
			if ok {
				t.Errorf("%s: a drop to %v was taken", c.name, land)
			}
			continue
		}
		if !ok || land.Y != c.land || hurt != c.hurt {
			t.Errorf("%s: lands %v hurt %v ok %v, want y %d hurt %v", c.name, land, hurt, ok, c.land, c.hurt)
		}
	}
}
