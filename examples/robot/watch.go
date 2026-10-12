package main

import (
	"math"
	"slices"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26/level/component"
	"github.com/mj41/go-mc26/protocol/types"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// The watchers: they only look, and never move the robot. Each raises
// signals for the loops that act — the reflexes read the surroundings, the
// day loop's housekeeping the pack — instead of each command looking at the
// mobs, the blocks round it and its pack in its own way.

// signals is what the watchers saw last.
type signals struct {
	wardenNear, creeperClose, mobClose bool // round it (the surroundings watch)
	inWall, byLava, inStream           bool
	underWater                         bool     // its eyes under the water: no air (drowning)
	packFull                           bool     // fewer than six slots free (the inventory watch)
	betterTool                         bool     // iron enough for an iron pickaxe it has not
	worn                               []string // the kinds (pickaxe, axe) whose only tool is nine tenths worn
}

// sigs are the signals now.
func (r *robot) sigs() signals {
	r.sigMu.Lock()
	defer r.sigMu.Unlock()
	return r.sig
}

// watchSurroundings looks round the robot every five ticks: the mobs near,
// its eyes in a block, lava beside it, moving water at it.
func (r *robot) watchSurroundings() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
		if !r.ctl.State().Loaded {
			continue
		}
		p := r.player.Position()
		var s signals
		for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, 32) {
			d := e.DistanceSqr(p.X, p.Y, p.Z)
			switch {
			case e.Type == "minecraft:warden":
				s.wardenNear = true
			case e.Type == "minecraft:creeper" && d <= 64:
				s.creeperClose = true
			case hostileType(e.Type) && d <= 36 && !r.isLeftAlone(e.ID):
				s.mobClose = true
			}
		}
		feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
		eyes := world.BlockPos{X: feet.X, Y: int(math.Floor(p.Y + 1.62)), Z: feet.Z}
		if b, ok := r.world.BlockAt(eyes); ok && block.FluidOf(b) == nil && len(block.CollisionShape(b)) > 0 {
			s.inWall = true
		}
		s.byLava = r.lavaNear(1)
		// its eyes under water (Entity.isEyeInFluid): air running out
		if b, ok := r.world.BlockAt(eyes); ok {
			if f := block.FluidOf(b); f != nil && (f.Name == "minecraft:water" || f.Name == "minecraft:flowing_water") && p.Y+1.62 < float64(eyes.Y)+float64(f.Height)+1.0/9 {
				s.underWater = true
			}
		}
		for _, c := range []world.BlockPos{feet, {X: feet.X, Y: feet.Y + 1, Z: feet.Z}} {
			if b, ok := r.world.BlockAt(c); ok {
				if f := block.FluidOf(b); f != nil && !f.Source && f.Name != "minecraft:lava" && f.Name != "minecraft:flowing_lava" {
					s.inStream = true
				}
			}
		}
		r.sigMu.Lock()
		s.packFull, s.betterTool, s.worn = r.sig.packFull, r.sig.betterTool, r.sig.worn
		r.sig = s
		r.sigMu.Unlock()
	}
}

// watchInventory looks at its pack every second: room left, iron enough for
// a better tool.
func (r *robot) watchInventory() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(time.Second):
		}
		if !r.ctl.State().Loaded {
			continue
		}
		full := r.freeSlots() < 6
		iron := r.ui.Count("minecraft:iron_ingot") + r.ui.Count("minecraft:raw_iron")
		better := iron >= 3 && r.ui.Count("minecraft:iron_pickaxe") == 0 && r.ui.Count("minecraft:diamond_pickaxe") == 0
		worn := r.wornKinds()
		r.sigMu.Lock()
		r.sig.packFull, r.sig.betterTool, r.sig.worn = full, better, worn
		r.sigMu.Unlock()
	}
}

// wornKinds are the tool kinds (pickaxe, axe) it has one tool of only, and
// that one nine tenths worn (its damage against its most): a spare wanted
// before it breaks, deep in a mine or up a tree.
func (r *robot) wornKinds() []string {
	r.screens.Lock()
	slots := append([]types.ItemStack(nil), r.screens.Inventory.Slots[9:45]...)
	r.screens.Unlock()
	var out []string
	for _, kind := range []string{"pickaxe", "axe"} {
		tools := tagItems("minecraft:" + kind + "s")
		n, worn := 0, false
		for _, s := range slots {
			if s.Count <= 0 || !slices.Contains(tools, itemName(int32(s.Item))) {
				continue
			}
			n++
			d, m := act.Component[*component.Damage](s), act.Component[*component.MaxDamage](s)
			if d != nil && m != nil && m.Value > 0 && 10*int(d.Value) >= 9*int(m.Value) {
				worn = true
			}
		}
		if n == 1 && worn {
			out = append(out, kind)
		}
	}
	return out
}
