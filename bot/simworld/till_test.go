package simworld

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// use is the bot's use of what it holds on the block, waited for.
func use(t *testing.T, g *Game, at world.BlockPos) {
	t.Helper()
	ch := make(chan act.Use, 1)
	g.B.Hands.Interact(at, func(u act.Use) { ch <- u })
	if !g.Until(200, func() bool { return len(ch) > 0 }) {
		t.Fatal("the use was not answered")
	}
	g.Step(2)
}

func blockAt(g *Game, x, y, z int) string { return block.StateList[g.S.World.At(x, y, z)].ID() }

// A hoe on grass with air over it tills it (farmland); on grass with a block
// over it, nothing.
func TestHoeTills(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Give("minecraft:stone_hoe", 1) // (the first thing given: in hand)
		g.Step(2)
		use(t, g, world.BlockPos{X: 1, Y: 63, Z: 0})
		if got := blockAt(g, 1, 63, 0); got != "minecraft:farmland" {
			t.Fatalf("grass hoed: %s, want farmland", got)
		}
		g.S.World.Set(2, 64, 0, "minecraft:stone")
		use(t, g, world.BlockPos{X: 2, Y: 63, Z: 0})
		if got := blockAt(g, 2, 63, 0); got != "minecraft:grass_block" {
			t.Fatalf("grass under a block hoed: %s, want it as it was", got)
		}
	})
}

// Seeds on farmland's top are sown: wheat over it, a seed used.
func TestSeedsSown(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Give("minecraft:wheat_seeds", 3)
		g.Step(2)
		g.S.World.Set(1, 63, 0, "minecraft:farmland")
		use(t, g, world.BlockPos{X: 1, Y: 63, Z: 0})
		if got := blockAt(g, 1, 64, 0); got != "minecraft:wheat" {
			t.Fatalf("seeds on farmland: %s over it, want wheat", got)
		}
		if n := g.Count("minecraft:wheat_seeds"); n != 2 {
			t.Errorf("%d seeds left, want 2", n)
		}
		use(t, g, world.BlockPos{X: 2, Y: 63, Z: 0}) // grass: nothing sown
		if got := blockAt(g, 2, 64, 0); got != "minecraft:air" {
			t.Errorf("seeds on grass: %s over it", got)
		}
		// on the side of a block beside the cell over farmland: sown there, as the game places a seed
		g.S.World.Set(3, 63, 0, "minecraft:farmland")
		g.S.World.Set(4, 64, 0, "minecraft:stone")
		g.Step(2)
		use(t, g, world.BlockPos{X: 4, Y: 64, Z: 0}) // the stone's face toward the player: its west
		if got := blockAt(g, 3, 64, 0); got != "minecraft:wheat" {
			t.Errorf("a seed put over farmland, against a block beside: %s", got)
		}
	})
}

// A block put where the player stands is refused, as the server refuses one whose shape collides with it.
func TestNoBlockInThePlayer(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Give("minecraft:cobblestone", 4)
		g.Step(2)
		use(t, g, world.BlockPos{X: 0, Y: 63, Z: 0}) // its feet's cell, on the block under them
		if got := blockAt(g, 0, 64, 0); got != "minecraft:air" {
			t.Fatalf("a block put in the player: %s", got)
		}
		use(t, g, world.BlockPos{X: 2, Y: 63, Z: 0}) // beside it: put
		if got := blockAt(g, 2, 64, 0); got != "minecraft:cobblestone" {
			t.Fatalf("a block beside the player: %s", got)
		}
	})
}

// A chest is put as a block, a single chest facing the player.
func TestChestPut(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Give("minecraft:chest", 1)
		g.Step(2)
		use(t, g, world.BlockPos{X: 2, Y: 63, Z: 0})
		if got := blockAt(g, 2, 64, 0); got != "minecraft:chest" {
			t.Fatalf("a chest put: %s", got)
		}
		if n := g.Count("minecraft:chest"); n != 0 {
			t.Errorf("%d chests left, want none", n)
		}
	})
}

// A chestplate used from the hand is worn: in the chest's slot, the hand empty.
func TestArmorWorn(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Give("minecraft:iron_chestplate", 1)
		g.Step(2)
		ch := make(chan act.Use, 1)
		g.B.Hands.UseItemToward(0.5, 70, 5.5, func(u act.Use) { ch <- u })
		if !g.Until(200, func() bool { return len(ch) > 0 }) {
			t.Fatal("the use was not answered")
		}
		g.Step(2)
		if got := g.Worn(6); got != "minecraft:iron_chestplate" {
			t.Errorf("worn on the chest: %q", got)
		}
		if h := g.heldItem(); h.n != 0 {
			t.Errorf("the hand holds %s after the chestplate was put on", h)
		}
	})
}

// A pickaxe used on a block does nothing (it has no use on one).
func TestPickaxeUseDoesNothing(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Give("minecraft:iron_pickaxe", 1)
		g.Step(2)
		use(t, g, world.BlockPos{X: 1, Y: 63, Z: 0})
		if got := blockAt(g, 1, 63, 0); got != "minecraft:grass_block" {
			t.Errorf("grass after a pickaxe's use: %s", got)
		}
	})
}

// A block put into a cell of water or lava replaces the fluid (the game's BlockItem: fluids are replaceable);
// the stones a pickaxe drops besides cobblestone are put like it.
func TestBlockReplacesWater(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Give("minecraft:cobblestone", 4)
		g.Give("minecraft:andesite", 4)
		g.Step(2)
		g.S.World.Set(2, 64, 0, "minecraft:water")
		g.S.World.Set(2, 64, 1, "minecraft:lava")
		use(t, g, world.BlockPos{X: 2, Y: 63, Z: 0}) // the cobblestone in hand, on the ground's top: into the water over it
		if got := blockAt(g, 2, 64, 0); got != "minecraft:cobblestone" {
			t.Fatalf("a block put into water: %s, want it in the water's place", got)
		}
		if n := g.Count("minecraft:cobblestone"); n != 3 {
			t.Errorf("%d cobblestone left, want 3", n)
		}
		use(t, g, world.BlockPos{X: 2, Y: 63, Z: 1})
		if got := blockAt(g, 2, 64, 1); got != "minecraft:cobblestone" {
			t.Fatalf("a block put into lava: %s", got)
		}
		// a block that is not replaceable stays: the stone put over it is refused
		g.S.World.Set(2, 64, -1, "minecraft:stone")
		use(t, g, world.BlockPos{X: 2, Y: 63, Z: -1})
		if got := blockAt(g, 2, 64, -1); got != "minecraft:stone" {
			t.Errorf("a block put where one stands: %s", got)
		}
	})
}

func TestPickaxeStonesAreUsable(t *testing.T) {
	Run(t, func(t *testing.T) {
		for _, stone := range []string{"andesite", "diorite", "granite", "tuff", "cobbled_deepslate", "deepslate"} {
			g := Start(t, Options{View: 2})
			g.Give("minecraft:"+stone, 2)
			g.Step(2)
			use(t, g, world.BlockPos{X: 2, Y: 63, Z: 0})
			if got := blockAt(g, 2, 64, 0); got != "minecraft:"+stone {
				t.Errorf("%s put: %s", stone, got)
			}
		}
	})
}
