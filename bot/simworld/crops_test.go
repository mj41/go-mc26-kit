package simworld

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// Wheat on moist farmland ripens in about half an hour of game (the random
// ticks' growth); on dry farmland, alone, slower; a wheat cut is no crop.
func TestWheatGrows(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2, Setup: []string{
			"fill 0 63 4 2 63 6 minecraft:farmland[moisture=7]", "setblock 1 63 7 minecraft:water", "fill 0 64 4 2 64 6 minecraft:wheat[age=0]",
			"setblock 20 63 20 minecraft:farmland[moisture=0]", "setblock 20 64 20 minecraft:wheat[age=0]",
		}})
		ripe := func(x, y, z int) bool { return wheatAge(g.S.World.At(x, y, z)) == 7 }
		at := -1
		for tick := 0; tick < 20*60*60 && at < 0; tick++ { // (the server's ticks alone: the growth is the server's)
			if err := g.cropsTick(); err != nil {
				t.Fatal(err)
			}
			if ripe(1, 64, 5) {
				at = tick
			}
		}
		if min := at / 20 / 60; at < 0 || min < 15 || min > 45 {
			t.Fatalf("the field's middle ripe at %d ticks, want about half an hour", at)
		}
		if ripe(20, 64, 20) {
			t.Errorf("the dry one alone ripe as soon as the moist field's middle")
		}
		g.S.World.Set(0, 64, 4, "minecraft:air")
		_ = g.cropsTick()
		if g.crops[[3]int{0, 64, 4}] {
			t.Errorf("a cut wheat still a crop")
		}
		g.Step(2)
		if s, _ := g.B.World.BlockAt(world.BlockPos{X: 1, Y: 64, Z: 5}); wheatAge(s) != 7 {
			t.Errorf("the client's wheat at age %d, want 7: not told", wheatAge(s))
		}
	})
}
