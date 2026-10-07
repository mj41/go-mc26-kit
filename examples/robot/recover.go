package main

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

// After a death: what it carried lies where it died, for five minutes (the
// game removes dropped things after 6000 ticks). A person respawns and runs
// back for them, then goes home; what was put in the chest at home was never
// at risk (storeValuables).

// itemsLast is how long dropped things lie before the game removes them.
const itemsLast = 5 * time.Minute

// cmdRecover goes back to where it died, if its things can still be there —
// running — picks up what lies round, and goes home. It answers what it got
// back.
func cmdRecover(r *robot, _ []string) (string, error) {
	r.seenMu.Lock()
	at, when := r.deathAt, r.deathTime
	r.seenMu.Unlock()
	if at == nil {
		return "nothing to recover", nil
	}
	before := 0
	r.screens.Lock()
	for _, s := range r.screens.Inventory.Slots[9:45] {
		before += int(s.Count)
	}
	r.screens.Unlock()
	// what killed it may be there still (a skeleton by a dark cave): a
	// person with nothing on them does not walk back into it
	killers := false
	for _, e := range r.entities.Nearby(float64(at.X)+0.5, float64(at.Y), float64(at.Z)+0.5, 16) {
		if hostileType(e.Type) {
			killers = true
		}
	}
	if killers {
		plan("recover: mobs where it died (%v): its things left", *at)
	} else if left := itemsLast - time.Since(when); left > 10*time.Second {
		plan("recover: to %v, where it died (%s left)", *at, left.Round(time.Second))
		g, ok := r.groundNear(at.X, at.Z, at.Y) // where it died, not the surface over it
		if !ok {
			g = *at
		}
		r.walker.Sprint = true
		_, err := cmdGoto(r, []string{strconv.Itoa(g.X), strconv.Itoa(g.Y), strconv.Itoa(g.Z)})
		r.walker.Sprint = false
		if err != nil {
			plan("recover: %v", err)
		}
		if math.Hypot(float64(g.X)-r.player.Position().X, float64(g.Z)-r.player.Position().Z) < 12 {
			for i := 0; i < 3; i++ {
				if _, err := cmdCollect(r, []string{"10"}); err != nil {
					plan("recover: collect: %v", err)
				}
			}
		}
	} else {
		plan("recover: its things at %v are gone (died %s ago)", *at, time.Since(when).Round(time.Second))
	}
	r.seenMu.Lock()
	r.deathAt = nil
	r.seenMu.Unlock()
	after := 0
	r.screens.Lock()
	for _, s := range r.screens.Inventory.Slots[9:45] {
		after += int(s.Count)
	}
	r.screens.Unlock()
	if r.home != nil {
		if _, err := cmdIn(r, nil); err != nil {
			plan("recover: home: %v", err)
		}
	}
	return fmt.Sprintf("got back %d things (carries %d)", after-before, after), nil
}

// valuables are what goes into the chest at home each time it comes in: what
// is worth much and wanted rarely on the way — the rest (tools, food, torches,
// coal, blocks) it keeps.
var valuables = []string{
	"minecraft:diamond", "minecraft:emerald", "minecraft:gold_ingot", "minecraft:copper_ingot",
	"minecraft:raw_gold", "minecraft:raw_copper", "minecraft:redstone", "minecraft:lapis_lazuli",
	"minecraft:gold_nugget", "minecraft:amethyst_shard",
}

// storeValuables puts its valuables (and iron, once its iron tools are made)
// into the chest at home, as a person banks their finds before going out
// again: a death or a long walk then loses little.
func (r *robot) storeValuables() {
	pos, ok := r.nearest([]string{"minecraft:chest"}, nil)
	if !ok || r.distanceTo(pos) > 6 {
		return
	}
	kinds := append([]string(nil), valuables...)
	if r.ui.Count("minecraft:iron_pickaxe") > 0 && r.ui.Count("minecraft:iron_sword") > 0 && r.ui.Count("minecraft:bucket")+r.ui.Count("minecraft:water_bucket") > 0 {
		kinds = append(kinds, "minecraft:iron_ingot", "minecraft:raw_iron")
	}
	have := false
	for _, k := range kinds {
		if r.ui.Count(k) > 0 {
			have = true
		}
	}
	if !have {
		return
	}
	if err := r.reach(pos); err != nil {
		return
	}
	if _, err := cmdOpen(r, []string{strconv.Itoa(pos.X), strconv.Itoa(pos.Y), strconv.Itoa(pos.Z)}); err != nil {
		plan("store: %v", err)
		return
	}
	moved := 0
	for _, k := range kinds {
		if r.ui.Count(k) == 0 {
			continue
		}
		n, err := r.ui.Move(r.ctx, k, true)
		if err != nil {
			plan("store %s: %v", k, err)
		}
		moved += n
	}
	r.closeScreen()
	plan("stored %d valuables in the chest at %v", moved, pos)
}
