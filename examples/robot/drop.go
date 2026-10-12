package main

import (
	"math"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Dropping off a ledge: the last way down when the goal is under it and the stairs have nowhere to go (DESIGN.md,
// "Getting stuck"). A person on a cliff's last block with the pond at its foot steps off.

// maxLook is how far under a ledge the landing is looked for.
const maxLook = 24

// keepHealth is the health a fall must leave: the same measure as the drop from the last leaf of a felled canopy.
const keepHealth = 7

// dropLanding says where a fall lands when the feet, at height y, go off an edge to the column x, z: down the
// column from the cell under y — into water (a cell of it: the fall's hurt none) or on the first block that
// holds it (not lava, cactus, magma, fire or a campfire in the landing cell), with the cell over it open — the fall's
// hurt the blocks fallen less three. ok is false when nothing safe is there within maxLook, or the hurt would leave
// less than keepHealth (a fall that hurts not at all is always taken).
func dropLanding(at func(world.BlockPos) (block.StateID, bool), x, y, z int, health float32) (land world.BlockPos, hurt float32, ok bool) {
	for dy := 1; dy <= maxLook; dy++ {
		c := world.BlockPos{X: x, Y: y - dy, Z: z}
		s, known := at(c)
		if !known {
			return world.BlockPos{}, 0, false // not loaded: a drop into the unknown
		}
		name := blockID(s)
		if f := block.FluidOf(s); f != nil {
			if f.Name == "minecraft:lava" || f.Name == "minecraft:flowing_lava" {
				return world.BlockPos{}, 0, false
			}
			return c, 0, true // water: no hurt, whatever the height
		}
		if len(block.CollisionShape(s)) == 0 {
			continue // air (a plant, a torch): on down
		}
		for _, bad := range []string{"cactus", "magma_block", "campfire", "fire", "sweet_berry_bush", "pointed_dripstone"} {
			if strings.Contains(name, bad) {
				return world.BlockPos{}, 0, false
			}
		}
		land = world.BlockPos{X: x, Y: c.Y + 1, Z: z} // standing on it
		fall := float32(y - land.Y)                   // blocks fallen from the feet at y
		hurt = max(fall-3, 0)
		return land, hurt, hurt == 0 || health-hurt >= keepHealth
	}
	return world.BlockPos{}, 0, false
}

// dropOff steps off the ledge at the way d (one of the four sides) when the fall toward the goal is a good one:
// the landing is no farther from the goal than the ledge, the hurt leaves keepHealth, the way forward is open at
// the feet and head. It reports whether it landed lower.
func (r *robot) dropOff(d [2]int, goal world.BlockPos) bool {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	fx, fz := x+d[0], z+d[1]
	for _, c := range []world.BlockPos{{X: fx, Y: y, Z: fz}, {X: fx, Y: y + 1, Z: fz}} {
		if !r.openAt(c) {
			return false // a wall: no step off that way (the stairs dig into it)
		}
	}
	// the floor of the cell ahead is open: it is a fall, not a step; else not a drop
	if !r.openAt(world.BlockPos{X: fx, Y: y - 1, Z: fz}) {
		return false
	}
	// it lands a block or two on (the fall carries what the walk had): the second column must be a good fall too
	land, hurt, ok := dropLanding(r.world.BlockAt, fx, y, fz, r.player.Status().Health)
	if !ok {
		return false
	}
	if _, hurt2, ok2 := dropLanding(r.world.BlockAt, fx+d[0], y, fz+d[1], r.player.Status().Health); !ok2 {
		return false
	} else {
		hurt = max(hurt, hurt2)
	}
	here := math.Hypot(float64(goal.X-x), float64(goal.Z-z))
	there := math.Hypot(float64(goal.X-land.X), float64(goal.Z-land.Z))
	if there > here+1e-9 || land.Y < goal.Y-3 {
		return false // away from the goal, or far under it
	}
	plan("climb out: drop off the ledge at %d %d %d toward %v: lands at %v, hurt %.0f", x, y, z, d, land, hurt)
	act.LookAt(r.player, false, float64(fx)+0.5, float64(y)+0.5, float64(fz)+0.5)
	r.ctl.SetInput(control.Input{Forward: true})
	defer r.ctl.SetInput(control.Input{})
	left := false
	for t := 0; t < 80; t++ {
		if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
			return false
		}
		st := r.ctl.State()
		if !st.OnGround && !left {
			left = true
			r.ctl.SetInput(control.Input{}) // off the edge: the keys let go, the fall carries it a little way (held, it lands four blocks on)
		}
		if left && st.OnGround {
			break
		}
	}
	return r.player.Position().Y < float64(y)-0.5
}
