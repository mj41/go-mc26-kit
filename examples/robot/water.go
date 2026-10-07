package main

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// stillWater reports whether p is a source of water out under the sky with
// more of it beside: a pond, a river, a lake — water a bucket can be filled
// at again and again, not a lone source in a cave.
func (r *robot) stillWater(p world.BlockPos) bool {
	if !r.isSource(p) || !r.openAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) {
		return false
	}
	if sky, _, ok := r.world.LightAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}); ok && sky < 13 {
		return false
	}
	n := 0
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if r.isSource(world.BlockPos{X: p.X + d[0], Y: p.Y, Z: p.Z + d[1]}) {
			n++
		}
	}
	return n > 0
}

// waterInSight finds the nearest still water in sight (stillWater).
func (r *robot) waterInSight() (world.BlockPos, bool) {
	if p, ok := r.nearestWhere([]string{"minecraft:water"}, nil, func(p world.BlockPos) bool {
		return r.stillWater(p) && !r.keptOut(p) && len(r.beside(p)) > 0 // at its edge: a bucket is filled from there
	}); ok {
		return p, true
	}
	// no pond or river: a spring, a lone source under the sky — it wets a
	// field round it, and gives a bucket (the pool's two come from two)
	return r.nearestWhere([]string{"minecraft:water"}, nil, func(p world.BlockPos) bool {
		if !r.isSource(p) || r.keptOut(p) || len(r.beside(p)) == 0 || !r.openAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) {
			return false
		}
		sky, _, ok := r.world.LightAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z})
		return !ok || sky >= 13
	})
}

// cmdWater finds still water and remembers it, as a person notes the pond
// on the first day: the water for the field's buckets — or, before there is
// iron for a bucket, the place for a field round it. Known and still there:
// that. Else in sight, else the search's rings round home, leg after leg, as
// far as the day's light allows.
func cmdWater(r *robot, _ []string) (string, error) {
	if w := r.water; w != nil {
		if s, ok := r.world.BlockAt(*w); !ok || block.FluidOf(s) != nil {
			return fmt.Sprintf("water known at %d %d %d", w.X, w.Y, w.Z), nil
		}
		r.water = nil // gone (a bucket took the last of it)
	}
	look := func() (string, bool) {
		if p, ok := r.waterInSight(); ok {
			r.water = &p
			plan("water: still water at %v", p)
			return fmt.Sprintf("water at %d %d %d", p.X, p.Y, p.Z), true
		}
		return "", false
	}
	// ice melts into water: an ice block broken (not with silk touch) over
	// a block leaves a source of water where it was — a frozen pond, the
	// ice of the cold heights
	melt := func() (string, bool) {
		ice, ok := r.nearestWhere([]string{"minecraft:ice"}, nil, func(p world.BlockPos) bool {
			below, ok := r.world.BlockAt(world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z})
			if !ok || len(block.CollisionShape(below)) == 0 && block.FluidOf(below) == nil {
				return false
			}
			sky, _, ok := r.world.LightAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z})
			return r.openAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) && (!ok || sky >= 13) && len(r.beside(p)) > 0
		})
		if !ok {
			return "", false
		}
		if err := r.standBeside(ice); err != nil {
			plan("water: ice at %v: %v", ice, err)
			return "", false
		}
		if _, err := cmdDig(r, []string{strconv.Itoa(ice.X), strconv.Itoa(ice.Y), strconv.Itoa(ice.Z)}); err != nil {
			plan("water: ice at %v: %v", ice, err)
			return "", false
		}
		_ = r.ctl.WaitTicks(r.ctx, 10)
		if !r.isSource(ice) {
			plan("water: the ice at %v left no water", ice)
			return "", false
		}
		r.water = &ice
		plan("water: ice melted into water at %v", ice)
		return fmt.Sprintf("water at %d %d %d (ice melted)", ice.X, ice.Y, ice.Z), true
	}
	if ans, ok := look(); ok {
		return ans, nil
	}
	if ans, ok := melt(); ok {
		return ans, nil
	}
	if has := areaHas("minecraft:water_bucket"); r.goToKnown("water", has) {
		if ans, ok := look(); ok {
			return ans, nil
		}
	}
	// downhill first, as a person looks for water: rivers and lakes lie at
	// the bottoms of the valleys — the lowest ground of eight ways each leg
	for leg := 0; leg < 10; leg++ {
		if err := r.dayWork(); err != nil {
			return "", err
		}
		p := r.player.Position()
		x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
		best, bestY := world.BlockPos{}, y-2 // clearly lower, or no leg
		for k := 0; k < 8; k++ {
			a := float64(k) * math.Pi / 4
			gx, gz := x+int(math.Round(math.Cos(a)*32)), z+int(math.Round(math.Sin(a)*32))
			if g, ok := r.groundAt(gx, gz, y); ok && g.Y < bestY && r.openSky(g) { // land, not a cave's floor
				best, bestY = g, g.Y
			}
		}
		if bestY >= y-2 {
			break // a valley's bottom, dry: the rings
		}
		plan("water: downhill to %v", best)
		if _, err := cmdGoto(r, []string{strconv.Itoa(best.X), strconv.Itoa(best.Y), strconv.Itoa(best.Z)}); err != nil {
			plan("water: downhill: %v", err)
			break
		}
		r.learn()
		if ans, ok := look(); ok {
			return ans, nil
		}
		if ans, ok := melt(); ok {
			return ans, nil
		}
	}
	// the rings, all the day long (the search goes on tomorrow where it
	// stopped); legs that go nowhere six times running end it
	for try, idle := 0, 0; try < 200 && idle < 6; try++ {
		before := r.player.Position()
		err := r.explore(try)
		if after := r.player.Position(); math.Hypot(after.X-before.X, after.Z-before.Z) < 4 {
			idle++
		} else {
			idle = 0
		}
		// a look round after every leg, a failed one too: the water is what
		// stops a walk (the path keeps out of it)
		if ans, ok := look(); ok {
			return ans, nil
		}
		if errors.Is(err, errLate) {
			return "", err
		} else if err != nil {
			if errors.Is(err, errNoWay) && !r.underSky() && !r.skyWithin(8) && r.home != nil && r.distanceTo(r.home.centre()) > 8 {
				// covered over out there (a cave's mouth, snow): up to the
				// sky, then on
				if cerr := r.upToSky(); cerr != nil {
					return "", fmt.Errorf("%w (and up to the sky: %v)", err, cerr)
				}
				continue
			}
			if errors.Is(err, errNoWay) {
				return "", err // hurt with nothing to eat: not now
			}
			plan("water: %v", err)
			continue
		}
		if ans, ok := look(); ok {
			return ans, nil
		}
		if ans, ok := melt(); ok {
			return ans, nil
		}
	}
	return "", fmt.Errorf("%w: no still water found round home", errNoWay)
}

// wildSoil are the field's cells round water w that has no channel of its
// own: grass and dirt (and farmland) at the water's level within four blocks
// of it, where the water keeps them wet, the nearest first.
func (r *robot) wildSoil(w world.BlockPos) []world.BlockPos {
	var out []world.BlockPos
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			p := world.BlockPos{X: w.X + dx, Y: w.Y, Z: w.Z + dz}
			s, ok := r.world.BlockAt(p)
			if !ok || int(s) >= len(block.StateList) {
				continue
			}
			switch block.StateList[s].ID() {
			case "minecraft:grass_block", "minecraft:dirt", "minecraft:farmland":
			default:
				continue
			}
			// open above, or its wheat
			if a, ok := r.world.BlockAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}); !ok || block.FluidOf(a) != nil ||
				!block.Replaceable(a) && (int(a) >= len(block.StateList) || block.StateList[a].ID() != "minecraft:wheat") {
				continue
			}
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		di := math.Hypot(float64(out[i].X-w.X), float64(out[i].Z-w.Z))
		dj := math.Hypot(float64(out[j].X-w.X), float64(out[j].Z-w.Z))
		return di < dj
	})
	return out
}

// upToSky climbs from under cover (a cave's mouth, a snowdrift) to the open
// sky over it: stairs dug up to the first open place over the highest block
// above its head.
func (r *robot) upToSky() error {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	top := y + 1
	for dy := 2; dy <= 64; dy++ {
		if s, ok := r.world.BlockAt(world.BlockPos{X: x, Y: y + dy, Z: z}); ok && len(block.CollisionShape(s)) > 0 {
			top = y + dy
		}
	}
	plan("under cover at %d %d %d: up to the sky over %d", x, y, z, top)
	err := r.climbOut(world.BlockPos{X: x, Y: top + 1, Z: z})
	if err == nil {
		return nil
	}
	// no way up here: a place under the sky near, walked to
	for d := 4; d <= 16; d += 4 {
		for k := 0; k < 8; k++ {
			a := float64(k) * math.Pi / 4
			gx, gz := x+int(math.Round(math.Cos(a)*float64(d))), z+int(math.Round(math.Sin(a)*float64(d)))
			for dy := 6; dy >= -6; dy-- {
				g := world.BlockPos{X: gx, Y: y + dy, Z: gz}
				if !r.standable(g) || !r.openSky(g) {
					continue
				}
				if _, gerr := cmdGoto(r, []string{strconv.Itoa(g.X), strconv.Itoa(g.Y), strconv.Itoa(g.Z)}); gerr == nil {
					return nil
				}
				break
			}
		}
	}
	return err
}

// openSky reports whether a body standing at g is under the open sky (the
// sky's light at its head near full; not known: nothing solid over it).
func (r *robot) openSky(g world.BlockPos) bool {
	head := world.BlockPos{X: g.X, Y: g.Y + 1, Z: g.Z}
	if sky, _, ok := r.world.LightAt(head); ok {
		return sky >= 13
	}
	return !r.underground(g)
}

// poolWater reports whether p is still water a bucket can be filled at, out
// under the sky or not: a source with more of it beside, open above, with a
// place to stand at its edge — water met in its mine is the field's too.
func (r *robot) poolWater(p world.BlockPos) bool {
	if !r.isSource(p) || !r.openAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) || len(r.beside(p)) == 0 {
		return false
	}
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if r.isSource(world.BlockPos{X: p.X + d[0], Y: p.Y, Z: p.Z + d[1]}) {
			return true
		}
	}
	return false
}

// needsWater reports whether the field by home still wants its water (not
// made, or its pool and channel not all still water).
func (r *robot) needsWater() bool {
	return r.field == nil || !r.fieldWet(*r.field)
}

// noteBucketWater remembers still water seen under ground (its mine, its
// stairs), for the field's buckets when there is none on the land.
func (r *robot) noteBucketWater() {
	if r.bucketWater != nil || r.water != nil || r.underSky() {
		return
	}
	if p, ok := r.nearestWhere([]string{"minecraft:water"}, nil, r.poolWater); ok && r.distanceTo(p) < 24 {
		r.bucketWater = &p
		plan("water: still water under ground at %v (for buckets)", p)
	}
}

// fillForField fills the empty buckets it carries at still water near, when
// the field by home still wants its water: at a night's end in its mine, the
// water carried home for the morning.
func (r *robot) fillForField() {
	if !r.needsWater() || r.ui.Count("minecraft:bucket") == 0 {
		return
	}
	for r.ui.Count("minecraft:bucket") > 0 {
		p, ok := r.nearestWhere([]string{"minecraft:water"}, nil, r.poolWater)
		if !ok || r.distanceTo(p) > 16 {
			return
		}
		if err := r.fillAt(p); err != nil {
			plan("water: buckets for the field at %v: %v", p, err)
			return
		}
		plan("water: a bucket for the field filled at %v", p)
	}
}

// lowerBank makes the bank round water w a field's level with it: the soil
// a block over the water (a lake's grass bank) dug off within four blocks of
// it, so the dirt under it — at the water's level, where the water keeps it
// wet — is open to till. Only where the dirt under is soil and the bank's
// block is grass or dirt with nothing on it.
func (r *robot) lowerBank(w world.BlockPos) int {
	soil := func(p world.BlockPos) bool {
		s, ok := r.world.BlockAt(p)
		if !ok || int(s) >= len(block.StateList) {
			return false
		}
		switch block.StateList[s].ID() {
		case "minecraft:grass_block", "minecraft:dirt", "minecraft:farmland":
			return true
		}
		return false
	}
	n := 0
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			low := world.BlockPos{X: w.X + dx, Y: w.Y, Z: w.Z + dz}
			bank := world.BlockPos{X: low.X, Y: w.Y + 1, Z: low.Z}
			if !soil(low) || !soil(bank) {
				continue
			}
			if a, ok := r.world.BlockAt(world.BlockPos{X: bank.X, Y: bank.Y + 1, Z: bank.Z}); !ok || !block.Replaceable(a) || block.FluidOf(a) != nil {
				continue
			}
			if r.ownColumn(bank) {
				continue
			}
			if err := r.reach(bank); err != nil {
				continue
			}
			if s, ok := r.world.BlockAt(bank); ok {
				_ = r.holdBestTool(s)
			}
			if _, err := cmdDig(r, []string{strconv.Itoa(bank.X), strconv.Itoa(bank.Y), strconv.Itoa(bank.Z)}); err != nil {
				plan("farm: the bank at %v: %v", bank, err)
				continue
			}
			n++
		}
	}
	if n > 0 {
		plan("farm: the bank round the water at %v lowered: %d dug off", w, n)
	}
	return n
}
