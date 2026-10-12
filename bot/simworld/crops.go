package simworld

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// randomTicks is the game's randomTickSpeed: three blocks of each 16-cube
// section get a random tick each tick — a block one in 4096/3 ticks (a minute
// on average).
const randomTicks = 3

// cropsTick grows the wheat, as its random ticks do (CropBlock.randomTick): a
// tick that falls on it grows it an age with the chance 1/(⌊25/f⌋+1), f its
// farmland's worth (getGrowthSpeed) — light is not modelled (a crop under
// the sky has it, day and night: the sky's light is not the time's). A wheat
// gone (cut, trodden) is dropped.
func (g *Game) cropsTick() error {
	if len(g.crops) == 0 {
		return nil
	}
	cells := make([][3]int, 0, len(g.crops))
	for c := range g.crops {
		cells = append(cells, c)
	}
	slices.SortFunc(cells, func(a, b [3]int) int { // (a map's order: the chance drawn in one order, the game the same each time)
		for i := range a {
			if a[i] != b[i] {
				return a[i] - b[i]
			}
		}
		return 0
	})
	for _, c := range cells {
		s := g.S.World.At(c[0], c[1], c[2])
		if block.StateList[s].ID() != "minecraft:wheat" {
			delete(g.crops, c)
			continue
		}
		if g.cropRand.Intn(4096) >= randomTicks {
			continue
		}
		age := wheatAge(s)
		if age >= 7 || g.cropRand.Intn(int(25/g.growthSpeed(c))+1) != 0 {
			continue
		}
		next := stateByText(fmt.Sprintf("minecraft:wheat[age=%d]", age+1))
		g.S.World.SetState(c[0], c[1], c[2], next)
		if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: c[0], Y: c[1], Z: c[2]}, BlockState: pk.VarInt(next)}); err != nil {
			return err
		}
	}
	return nil
}

// wheatAge is the wheat's age, 0 to 7.
func wheatAge(s block.StateID) int {
	str := world.StateString(s)
	i := strings.Index(str, "age=")
	if i < 0 {
		return 0
	}
	return int(str[i+4] - '0')
}

// growthSpeed is CropBlock.getGrowthSpeed: 1, and for the farmland under it
// and round that, 1 each (3 moist), the eight round it a quarter of that —
// the halving for a crop of its kind on both sides is not modelled. Farmland
// is moist with water within four blocks (FarmBlock.isNearWater: four round,
// its level and one up), whatever its moisture says.
func (g *Game) growthSpeed(c [3]int) float64 {
	f := 1.0
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			x, y, z := c[0]+dx, c[1]-1, c[2]+dz
			if block.StateList[g.S.World.At(x, y, z)].ID() != "minecraft:farmland" {
				continue
			}
			v := 1.0
			if g.waterNear(x, y, z) {
				v = 3
			}
			if dx != 0 || dz != 0 {
				v /= 4
			}
			f += v
		}
	}
	return f
}

// waterNear: water within four blocks round the farmland, at its level or one up.
func (g *Game) waterNear(x, y, z int) bool {
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			for dy := 0; dy <= 1; dy++ {
				if f := block.FluidOf(g.S.World.At(x+dx, y+dy, z+dz)); f != nil && (f.Name == "minecraft:water" || f.Name == "minecraft:flowing_water") {
					return true
				}
			}
		}
	}
	return false
}
