package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// A shelter dug into a hill under a tree's leaves: a layer of leaves is the roof and the cells round it. Leaves
// are solid, so the hill passes for rock; but they decay once the tree's logs are cut (the survival run of
// 2026-10-12 saw one of them open: a roof's edge over the way in). The robot takes the leaves out of the room's
// shell and puts cobblestone there, and also those of the cells round the shell that touch it, so that no
// decay leaves a hole in the roof's corner or beside the way in.
func TestShelterUnderLeaves(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		r, g := simRobot(t, simworld.Options{View: 5, Setup: []string{
			"fill 6 64 -4 16 72 4 minecraft:stone", "fill 6 66 -4 16 66 4 minecraft:oak_leaves[distance=3,persistent=false,waterlogged=false]",
			"give Robot minecraft:stone_pickaxe", "give Robot minecraft:furnace", "give Robot minecraft:chest", "give Robot minecraft:crafting_table",
			"give Robot minecraft:torch 8", "give Robot minecraft:cobblestone 40",
		}})
		g.Step(40)
		ans, err, ok := run(t, g, 20*600, func() (string, error) { return cmdShelter(r, nil) })
		t.Logf("shelter: %q err %v ended %v", ans, err, ok)
		if !ok || err != nil {
			t.Fatalf("no shelter: %v (ended %v)", err, ok)
		}
		var how string
		var x, y, z, fdx, fdz int
		if _, err := fmt.Sscanf(ans, "%s at %d %d %d facing %d %d", &how, &x, &y, &z, &fdx, &fdz); err != nil || how != "dug" {
			t.Fatalf("the shelter is %q, want one dug into the hill", ans)
		}
		kind := func(dx, dy, dz int) string {
			return block.StateList[g.S.World.At(x+dx, y+dy, z+dz)].ID()
		}
		// the room is closed by what touches it, and no leaf is left within a cell of it
		var leaves []string
		for dx := -2; dx <= 2; dx++ {
			for dz := -2; dz <= 2; dz++ {
				for dy := -1; dy <= 2; dy++ {
					if corner := (dx == -2 || dx == 2) && (dz == -2 || dz == 2); corner { // (touches nothing of the room)
						continue
					}
					if k := kind(dx, dy, dz); strings.HasSuffix(k, "_leaves") {
						leaves = append(leaves, fmt.Sprintf("%s at %v", k, world.BlockPos{X: x + dx, Y: y + dy, Z: z + dz}))
					}
				}
			}
		}
		// the room (three by three, two high) and the tunnel's first cell are the only open cells; each cell that
		// touches one by a face holds something that is not replaceable (a mob does not pass it)
		inside := func(dx, dz, dy int) bool { return dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1 && dy >= 0 && dy <= 1 }
		tunnel := func(dx, dz, dy int) bool { return dx == 2*fdx && dz == 2*fdz && dy >= 0 && dy <= 1 }
		open := func(dx, dz, dy int) bool { return inside(dx, dz, dy) || tunnel(dx, dz, dy) }
		for dx := -2; dx <= 2; dx++ {
			for dz := -2; dz <= 2; dz++ {
				for dy := -1; dy <= 2; dy++ {
					if open(dx, dz, dy) {
						continue
					}
					for _, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
						if open(dx+d[0], dz+d[1], dy+d[2]) && block.Replaceable(g.S.World.At(x+dx, y+dy, z+dz)) {
							t.Errorf("the shelter is open at %v", world.BlockPos{X: x + dx, Y: y + dy, Z: z + dz})
							break
						}
					}
				}
			}
		}
		if len(leaves) > 0 {
			t.Errorf("leaves left round the shelter at %d %d %d: %v", x, y, z, leaves)
		}
	})
}
