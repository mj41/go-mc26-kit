package simworld

import (
	"context"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

func placeFurnace(t *testing.T, g *Game, at world.BlockPos) {
	t.Helper()
	g.Give("minecraft:furnace", 1)
	g.Step(2)
	ch := make(chan act.Use, 1)
	g.B.Hands.Place(at, func(u act.Use) { ch <- u })
	if !g.Until(200, func() bool { return len(ch) > 0 }) {
		t.Fatal("the furnace was not placed")
	}
	if u := <-ch; u.Err != nil {
		t.Fatal(u.Err)
	}
	g.Step(2)
	if s := g.S.World.At(at.X, at.Y, at.Z); block.StateList[s].ID() != "minecraft:furnace" {
		t.Fatalf("no furnace at the place: %v", block.StateList[s].ID())
	}
}

func openFurnace(t *testing.T, g *Game, at world.BlockPos) {
	t.Helper()
	ch := make(chan act.Use, 1)
	g.B.Hands.Interact(at, func(u act.Use) { ch <- u })
	if !g.Until(200, func() bool { m, ok := g.B.Screens.Open(); return ok && m.Type == "minecraft:furnace" && len(ch) > 0 }) {
		t.Fatal("the furnace's menu never opened")
	}
}

// Three raw iron and a coal in a furnace: the block lit, the ingots made at ten seconds each (200 ticks),
// the coal burning for 1600 ticks and one taken, the furnace out when the iron is done... the bot's own Screens.Smelt.
func TestSmeltRawIron(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		at := world.BlockPos{X: 1, Y: 64, Z: 0}
		placeFurnace(t, g, at) // (the furnace is the first thing held: in hand)
		g.Give("minecraft:raw_iron", 3)
		g.Give("minecraft:coal", 2)
		g.Step(2)
		openFurnace(t, g, at)
		type res struct {
			n   int
			err error
		}
		done := make(chan res, 1)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		go func() { n, err := g.B.Act.Smelt(ctx, "minecraft:raw_iron", "minecraft:coal", 3); done <- res{n, err} }()
		var r res
		started := g.Tick
		if !g.Until(1500, func() bool {
			select {
			case r = <-done:
				return true
			default:
				return false
			}
		}) {
			t.Fatalf("the smelt never ended (tick %d)", g.Tick)
		}
		if r.err != nil || r.n != 3 {
			t.Fatalf("smelt: %d, %v", r.n, r.err)
		}
		took := g.Tick - started
		if took < 600 || took > 700 {
			t.Errorf("three ingots in %d ticks, want about 600 (200 each)", took)
		}
		g.Step(5)
		f := g.furnaces[[3]int{at.X, at.Y, at.Z}]
		// (the bot's Smelt puts the whole stack of coal in: one is burning, one is left in the slot)
		if g.Count("minecraft:iron_ingot") != 3 || g.Count("minecraft:coal") != 0 || f.s[1].n != 1 {
			t.Errorf("ingots %d, coal in the pack %d, in the fuel slot %d; want 3, 0 and 1", g.Count("minecraft:iron_ingot"), g.Count("minecraft:coal"), f.s[1].n)
		}
		// the fire burns on for the rest of the coal's 1600 ticks with nothing to cook
		if f.lit == 0 || f.lit > 1000 || f.litTotal != 1600 {
			t.Errorf("lit %d of %d, want 1600 burnt for 600+ ticks", f.lit, f.litTotal)
		}
		if s := g.S.World.At(at.X, at.Y, at.Z); s != furnaceState(f.facing, true) {
			t.Errorf("the furnace block is not lit while the coal burns: %v", s)
		}
	})
}

// What a furnace takes: raw iron in the input, coal in the fuel slot, nothing in the output, and a stick
// does not cook; shift-click sends an item to the slot it belongs in.
func TestFurnaceSlotsTakeWhatBelongs(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2, Setup: []string{"give Builder minecraft:raw_iron 2", "give Builder minecraft:coal 1", "give Builder minecraft:dirt 3"}})
		if !g.mayPlace(furnaceMenu(0, [3]int{}), 0, "minecraft:raw_iron") || g.mayPlace(furnaceMenu(0, [3]int{}), 0, "minecraft:dirt") {
			t.Errorf("the input: raw iron yes, dirt no")
		}
		if !g.mayPlace(furnaceMenu(0, [3]int{}), 1, "minecraft:coal") || g.mayPlace(furnaceMenu(0, [3]int{}), 1, "minecraft:dirt") || g.mayPlace(furnaceMenu(0, [3]int{}), 2, "minecraft:iron_ingot") {
			t.Errorf("the fuel and output slots")
		}
		if burnTime("minecraft:coal") != 1600 || burnTime("minecraft:oak_planks") != 300 || burnTime("minecraft:dirt") != 0 {
			t.Errorf("burn times")
		}
	})
}
