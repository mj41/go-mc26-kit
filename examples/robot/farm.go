package main

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// cmdFarm starts a wheat field, as a person does on the second day: around a
// still water source near — or a hole on level ground it pours a bucket of
// water into — the grass and dirt within two blocks tilled with a hoe and sown
// with seeds (broken out of the grass about). farm [seeds] (8). It answers
// where the field is, how many tilled and sown.
func cmdFarm(r *robot, args []string) (string, error) {
	if err := r.dayWork(); err != nil { // outdoor work: not started late in the day
		return "", err
	}
	seeds := 8
	if len(args) == 1 {
		v, err := strconv.Atoi(args[0])
		if err != nil {
			return "", err
		}
		seeds = v
	}
	for _, g := range []struct {
		item string
		n    int
	}{{"minecraft:stone_hoe", 1}, {"minecraft:wheat_seeds", seeds}} {
		if err := r.get(g.item, g.n, 0); err != nil {
			return "", err
		}
	}
	// no iron for a bucket yet, and water found: the field round that water
	// (its own pool and channel by home come once there is a bucket)
	// water in the cold freezes: no field round it
	wild := !r.bucketable() && r.water != nil && !r.coldAt(*r.water)
	var home world.BlockPos
	if !wild {
		// the field by home: tended every day, it is no use far from it
		if r.home != nil && r.distanceTo(r.home.front) > 24 {
			if err := r.goTo(r.home.front); err != nil {
				plan("farm: home first: %v", err)
			}
		}
		h, err := r.waterForFarm()
		if err != nil {
			if errors.Is(err, errLate) || r.water == nil || r.coldAt(*r.water) {
				return "", err // dusk: the field tomorrow, not the wild one instead
			}
			// no flat ground by home (never built up over holes): round
			// the water found, for now
			plan("farm: %v: round the water found instead", err)
			wild = true
		}
		home = h
	}
	if wild {
		w := *r.water
		r.wild = &w
		plan("farm: no bucket yet: a field round the water at %v", w)
		// there first: from afar its land is not even loaded
		if err := r.standBeside(w); err != nil {
			if err := r.reach(w); err != nil {
				return "", fmt.Errorf("to the water at %v: %w", w, err)
			}
		}
		r.lowerBank(w) // the field at the water's level: wet
		tilled, sown, err := r.tendCells(r.wildSoil(w))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("field at %d %d %d tilled=%d sown=%d (round the water found)", w.X, w.Y, w.Z, tilled, sown), nil
	}
	w := home
	plan("farm: around the water at %v", w)
	r.field = &w
	tilled, sown, err := r.tendField(w)
	if err != nil {
		return "", err
	}
	if sown == 0 {
		return "", fmt.Errorf("nothing sown round the water at %v", w)
	}
	return fmt.Sprintf("field at %d %d %d tilled=%d sown=%d", w.X, w.Y, w.Z, tilled, sown), nil
}

// The field: a pool of still water two by two at the west end — two buckets
// poured in its opposite corners, the other two fill of themselves: water
// that never runs out — and from it, at the field's level, two channels ten
// long (along x), the rows across the field seed, water, seed, seed, water,
// seed: forty plants, each next to water (it keeps soil wet four blocks out).
// Its centre c is the first channel's fifth cell; the pool is where the
// eight-long field of before had it, so a field made then grows into this.
const (
	fieldLen  = 10 // a channel's length, along x
	fieldFrom = -4 // the channels' first cell, from c.X (next to the pool)
)

var (
	fieldChannels = []int{0, 3}        // the channels' rows (z from c)
	fieldRows     = []int{1, 2, -1, 4} // the soil's rows, those between the channels first
)

// fieldWater are the channels' cells round the field's centre c, each from
// the pool's end.
func fieldWater(c world.BlockPos) []world.BlockPos {
	var out []world.BlockPos
	for _, dz := range fieldChannels {
		for dx := fieldFrom; dx < fieldFrom+fieldLen; dx++ {
			out = append(out, world.BlockPos{X: c.X + dx, Y: c.Y, Z: c.Z + dz})
		}
	}
	return out
}

// fieldPool are the pool's cells, west of the first channel: its two
// opposite corners (the buckets go there) first.
func fieldPool(c world.BlockPos) []world.BlockPos {
	x := c.X + fieldFrom - 2
	return []world.BlockPos{{X: x, Y: c.Y, Z: c.Z}, {X: x + 1, Y: c.Y, Z: c.Z + 1}, {X: x + 1, Y: c.Y, Z: c.Z}, {X: x, Y: c.Y, Z: c.Z + 1}}
}

// fieldSoil are the farmland's cells, the rows between the channels first.
func fieldSoil(c world.BlockPos) []world.BlockPos {
	var out []world.BlockPos
	for _, dz := range fieldRows {
		for dx := fieldFrom; dx < fieldFrom+fieldLen; dx++ {
			out = append(out, world.BlockPos{X: c.X + dx, Y: c.Y, Z: c.Z + dz})
		}
	}
	return out
}

// fieldBox is the field's box, m blocks round it, from under its floor to
// above its crops: what its own works keep out of.
func fieldBox(c world.BlockPos, m int) *[2]world.BlockPos {
	return &[2]world.BlockPos{
		{X: c.X + fieldFrom - 4 - m, Y: c.Y - 2, Z: c.Z - 2 - m},
		{X: c.X + fieldFrom + fieldLen + 1 + m, Y: c.Y + 4, Z: c.Z + 5 + m},
	}
}

// fieldHeadland is the strip across the field's east end at its level: the
// way on foot from the rows between the channels round to the others — a
// person walks round the water at the end of a field, not over it (with no
// such way, the robot tending the middle rows was penned in by its own water).
func fieldHeadland(c world.BlockPos) []world.BlockPos {
	var out []world.BlockPos
	for dz := -1; dz <= 4; dz++ {
		out = append(out, world.BlockPos{X: c.X + fieldFrom + fieldLen, Y: c.Y, Z: c.Z + dz})
	}
	return out
}

// fieldArea is all the field's cells: pool, channels, rows and headland.
func fieldArea(c world.BlockPos) []world.BlockPos {
	return append(append(append(fieldPool(c), fieldWater(c)...), fieldSoil(c)...), fieldHeadland(c)...)
}

// fieldShut are the blocks that keep the field's water in: the floor under
// the pool and the channel, the pool's outer sides, the channel's far end and
// its sides (the soil rows) — no waterfall over the plot's edge, none into a
// cave.
func fieldShut(c world.BlockPos) []world.BlockPos {
	var out []world.BlockPos
	for _, w := range append(fieldPool(c), fieldWater(c)...) {
		out = append(out, world.BlockPos{X: w.X, Y: w.Y - 1, Z: w.Z})
	}
	x := c.X + fieldFrom - 2
	out = append(out,
		world.BlockPos{X: x - 1, Y: c.Y, Z: c.Z}, world.BlockPos{X: x - 1, Y: c.Y, Z: c.Z + 1},
		world.BlockPos{X: x, Y: c.Y, Z: c.Z - 1}, world.BlockPos{X: x + 1, Y: c.Y, Z: c.Z - 1},
		world.BlockPos{X: x, Y: c.Y, Z: c.Z + 2}, world.BlockPos{X: x + 1, Y: c.Y, Z: c.Z + 2})
	for _, dz := range fieldChannels {
		// each channel's far end; a channel not along the pool its near end too
		out = append(out, world.BlockPos{X: c.X + fieldFrom + fieldLen, Y: c.Y, Z: c.Z + dz})
		if dz != 0 && dz != 1 {
			out = append(out, world.BlockPos{X: c.X + fieldFrom - 1, Y: c.Y, Z: c.Z + dz})
		}
		// its sides: the soil rows
		for dx := fieldFrom; dx < fieldFrom+fieldLen; dx++ {
			out = append(out, world.BlockPos{X: c.X + dx, Y: c.Y, Z: c.Z + dz - 1}, world.BlockPos{X: c.X + dx, Y: c.Y, Z: c.Z + dz + 1})
		}
	}
	return out
}

// tendField tills and sows round the water w as far as its seeds go,
// nearest the water first: farmland bare of a crop sown again, then grass
// and dirt tilled and sown. It answers how many it tilled and sowed.
func (r *robot) tendField(w world.BlockPos) (tilled, sown int, err error) {
	return r.tendCells(fieldSoil(w))
}

// tendCells tills and sows the soil cells given, in their order, as far as
// its seeds go.
func (r *robot) tendCells(soil []world.BlockPos) (tilled, sown int, err error) {
	if len(soil) > 0 {
		r.nearTo(soil[0]) // its land loaded and in reach, not tended from afar
	}
	type cell struct{ soil world.BlockPos }
	var cells []cell
	for _, p := range soil {
		cells = append(cells, cell{p})
	}
	for _, c := range cells {
		if r.ui.Count("minecraft:wheat_seeds") == 0 {
			break
		}
		soil := c.soil
		above := world.BlockPos{X: soil.X, Y: soil.Y + 1, Z: soil.Z}
		s, ok := r.world.BlockAt(soil)
		a, ok2 := r.world.BlockAt(above)
		if !ok || !ok2 || !block.Replaceable(a) || block.FluidOf(a) != nil {
			continue
		}
		switch block.StateList[s].ID() {
		case "minecraft:grass_block", "minecraft:dirt", "minecraft:farmland":
		default:
			continue
		}
		if block.StateList[s].ID() != "minecraft:farmland" {
			if err := r.reach(soil); err != nil {
				plan("farm: %v out of reach: %v", soil, err)
				continue
			}
			// tall grass, leaf litter, a flower in the way of the hoe (it
			// tills only under air): broken first, by hand
			if a, ok := r.world.BlockAt(above); ok && block.StateList[a].ID() != "minecraft:air" {
				_ = r.holdBestTool(a) // the hand: no tool's wear on it
				if _, err := cmdDig(r, []string{strconv.Itoa(above.X), strconv.Itoa(above.Y), strconv.Itoa(above.Z)}); err != nil {
					plan("farm: %v over the soil: %v", world.StateString(a), err)
				}
			}
			// then the hoe in hand (the hand broke the grass: the hoe after)
			if _, err := r.toHotbar("minecraft:stone_hoe", true); err != nil {
				if gerr := r.get("minecraft:stone_hoe", 1, 0); gerr != nil {
					return tilled, sown, gerr
				}
				if _, err := r.toHotbar("minecraft:stone_hoe", true); err != nil {
					return tilled, sown, err
				}
			}
			if _, err := waitUse(r, func(done func(act.Use)) { r.hands.UseBlock(soil, done) }); err != nil {
				plan("farm: till %v: %v", soil, err)
				continue
			}
			tilled++
		} else if err := r.reach(soil); err != nil {
			continue
		}
		if _, err := r.toHotbar("minecraft:wheat_seeds", true); err != nil {
			break // the last one sown a moment ago: the sowing is done
		}
		if _, err := waitUse(r, func(done func(act.Use)) { r.hands.PlaceOn(above, soil, done) }); err != nil {
			plan("farm: sow %v: %v", above, err)
			continue
		}
		sown++
	}
	return tilled, sown, nil
}

// nearWorks reports whether c is within d blocks (across) of the shelter's
// room or the top of its stairs.
func (r *robot) nearWorks(c world.BlockPos, d float64) bool {
	near := func(p world.BlockPos) bool { return math.Hypot(float64(p.X-c.X), float64(p.Z-c.Z)) < d }
	if r.home != nil && near(r.home.centre()) {
		return true
	}
	for i, s := range r.stairs {
		if i >= 12 {
			break
		}
		if near(s) {
			return true
		}
	}
	return false
}

// fieldGround is the ground the field is made on: its own cells, the
// headland, and a ring one block round all of it.
func fieldGround(c world.BlockPos) []world.BlockPos {
	var out []world.BlockPos
	for x := c.X + fieldFrom - 4; x <= c.X+fieldFrom+fieldLen+1; x++ {
		for z := c.Z - 2; z <= c.Z+5; z++ {
			out = append(out, world.BlockPos{X: x, Y: c.Y, Z: z})
		}
	}
	return out
}

// preparePlot makes the field's ground ready before any water, as a person
// does with a shovel and dirt — never on rough ground as it is: every cell of
// the ground solid at c's level (a hole filled from the bottom up), three
// blocks clear over it, the soil's cells soil, the floor under every pool and
// channel cell solid. The walls the water needs are then already there, and
// there is ground to stand on round every cell.
func (r *robot) preparePlot(c world.BlockPos) error {
	// the trees in it and round it felled first, whole
	n, err := r.fellTreesAround(c, 4)
	if n > 0 {
		plan("farm: %d logs of the trees round the plot cut", n)
	}
	if err != nil {
		return err // dusk: home, the rest tomorrow
	}
	water := map[world.BlockPos]bool{}
	for _, w := range append(fieldPool(c), fieldWater(c)...) {
		water[w] = true
	}
	soil := map[world.BlockPos]bool{}
	for _, s := range fieldSoil(c) {
		soil[s] = true
	}
	var dig, fill, resoil []world.BlockPos
	for _, at := range fieldGround(c) {
		for h := 3; h >= 1; h-- {
			if up := (world.BlockPos{X: at.X, Y: at.Y + h, Z: at.Z}); !r.openAt(up) || r.fluidAt(up) {
				dig = append(dig, up)
			}
		}
		switch {
		case water[at]:
			// dug out for the water later (a field grown keeps what it has):
			// its floor solid
			if f := (world.BlockPos{X: at.X, Y: at.Y - 1, Z: at.Z}); r.openAt(f) {
				fill = append(fill, f)
			}
		case r.openAt(at):
			// a hole a block deep is filled; a deeper one is rough ground,
			// not built over (the plot chosen has none)
			if !r.openAt(world.BlockPos{X: at.X, Y: at.Y - 1, Z: at.Z}) {
				fill = append(fill, at)
			}
		case soil[at] && !r.soilAt(at):
			resoil = append(resoil, at)
		}
	}
	plan("farm: preparing the plot at %v: %d dug off, %d filled, %d made soil", c, len(dig), len(fill), len(resoil))
	if need := len(fill) + len(resoil); need > 0 {
		// the dirt from round it, not from the plot itself (and a few more:
		// a hole may want a column under it)
		r.keepOut = fieldBox(c, 1)
		if err := r.get("minecraft:dirt", r.ui.Count("minecraft:dirt")+need+4, 1); err != nil {
			plan("farm: dirt: %v", err)
		}
		r.keepOut = nil
	}
	// outdoor work: at dusk home, the rest tomorrow (the plot found again,
	// less to do)
	for _, p := range dig {
		if err := r.dayWork(); err != nil {
			return err
		}
		if r.openAt(p) && !r.fluidAt(p) {
			continue // fell with the block under it
		}
		if err := r.reach(p); err != nil {
			plan("farm: dig %v: %v", p, err)
			continue
		}
		if err := r.clear(p); err != nil {
			plan("farm: dig %v: %v", p, err)
		}
	}
	for _, p := range fill {
		if err := r.dayWork(); err != nil {
			return err
		}
		if err := r.reach(p); err != nil {
			plan("farm: fill %v: %v", p, err)
			continue
		}
		if err := r.fillSoil(p); err != nil {
			plan("farm: fill %v: %v", p, err)
		}
	}
	for _, p := range resoil {
		if err := r.reach(p); err != nil {
			continue
		}
		if err := r.clear(p); err != nil {
			continue
		}
		if err := r.fillSoil(p); err != nil {
			plan("farm: soil at %v: %v", p, err)
		}
	}
	if left := r.plotUnready(c); left > 0 {
		return fmt.Errorf("the plot at %v: %d cells of its ground not ready", c, left)
	}
	return nil
}

// fluidAt reports whether p holds water or lava (no ground, no headroom).
func (r *robot) fluidAt(p world.BlockPos) bool {
	s, ok := r.world.BlockAt(p)
	return ok && block.FluidOf(s) != nil
}

// cmdHarvest tends the field it made (farm): the ripe wheat broken and
// picked up, the farmland sown again, more of it tilled as the seeds allow,
// and bread baked of the wheat — the robot's food, as it hunts nothing.
// It answers what it harvested, sowed and has.
func cmdHarvest(r *robot, _ []string) (string, error) {
	// outdoor work like any other: not out again late in the day (from home
	// at dusk back out to the field, it walled itself in there)
	if err := r.dayWork(); err != nil {
		return "", err
	}
	if r.field == nil && r.wild == nil { // none made yet (its day went otherwise): made now
		return cmdFarm(r, nil)
	}
	// its fields: the one round the water it found, the one by home
	soil := func() []world.BlockPos {
		var out []world.BlockPos
		if r.wild != nil {
			out = append(out, r.wildSoil(*r.wild)...)
		}
		if r.field != nil {
			out = append(out, fieldSoil(*r.field)...)
		}
		return out
	}
	cut := 0
	for _, s := range soil() {
		crop := world.BlockPos{X: s.X, Y: s.Y + 1, Z: s.Z}
		st, ok := r.world.BlockAt(crop)
		if !ok || int(st) >= len(block.StateList) || block.StateList[st].ID() != "minecraft:wheat" {
			continue
		}
		if !strings.Contains(world.StateString(st), "age=7") {
			continue // not ripe
		}
		if err := r.reach(crop); err != nil {
			continue
		}
		_ = r.holdBestTool(st) // by hand
		if _, err := cmdDig(r, []string{strconv.Itoa(crop.X), strconv.Itoa(crop.Y), strconv.Itoa(crop.Z)}); err != nil {
			plan("harvest: %v: %v", crop, err)
			continue
		}
		cut++
		if cut%6 == 0 {
			_, _ = cmdCollect(r, []string{"6"})
		}
	}
	if cut > 0 {
		if _, err := cmdCollect(r, []string{"8"}); err != nil {
			plan("harvest: collect: %v", err)
		}
	}
	// the field by home, with its own water, once there is a bucket; a dry
	// one gets its pool and channel
	if r.field == nil && r.bucketable() {
		if w, err := r.waterForFarm(); err != nil {
			plan("harvest: a field by home: %v", err)
		} else {
			r.field = &w
		}
	} else if r.field != nil && !r.fieldWet(*r.field) && r.bucketable() {
		if r.plotUnready(*r.field) > 0 { // grown to the field of now: its ground prepared first
			if err := r.preparePlot(*r.field); err != nil {
				plan("harvest: %v", err)
			}
		}
		if err := r.makeWater(*r.field); err != nil {
			plan("harvest: water: %v", err)
		}
	}
	// seeds for the fields' empty soil, from the grass round them — a few a
	// day (one grass in eight gives one), till all their rows are sown
	cells := soil()
	empty := 0
	for _, s := range cells {
		if st, ok := r.world.BlockAt(world.BlockPos{X: s.X, Y: s.Y + 1, Z: s.Z}); ok && int(st) < len(block.StateList) && block.StateList[st].ID() == "minecraft:air" {
			empty++
		}
	}
	if have := r.ui.Count("minecraft:wheat_seeds"); have < empty && len(cells) > 0 {
		r.bounds = &bounds{at: cells[0], r: 32} // the grass round the field, not a wander from tuft to tuft
		err := r.get("minecraft:wheat_seeds", have+min(empty-have, 8), 1)
		r.bounds = nil
		if err != nil { // the grass in sight: no leg out for seeds
			plan("harvest: seeds: %v", err)
		}
	}
	tilled, sown, err := r.tendCells(cells)
	if err != nil {
		plan("harvest: %v", err)
	}
	// by the water anyway: fish for the days ahead, while it carries few
	if r.canFish() && r.fishCount() < 6 {
		if ans, err := cmdFish(r, []string{"4"}); err != nil {
			plan("harvest: fish: %v", err)
		} else {
			plan("harvest: fish: %s", ans)
		}
	}
	if n := r.ui.Count("minecraft:wheat") / 3; n > 0 {
		if err := r.get("minecraft:bread", r.ui.Count("minecraft:bread")+n, 0); err != nil {
			plan("harvest: bread: %v", err)
		}
	}
	return fmt.Sprintf("cut=%d tilled=%d sown=%d seeds=%d bread=%d", cut, tilled, sown,
		r.ui.Count("minecraft:wheat_seeds"), r.ui.Count("minecraft:bread")), nil
}

// bucketable reports whether it has a bucket (empty or full), or can make
// one of what it carries.
func (r *robot) bucketable() bool {
	if r.ui.Count("minecraft:bucket")+r.ui.Count("minecraft:water_bucket") > 0 {
		return true
	}
	return r.get("minecraft:bucket", 1, 1) == nil
}

// waterForFarm makes the field's ground and water, and answers its centre
// (the channel's middle): the plot near home with the most soil at one level
// over ten by five — clear of the shelter and its stairs — levelled with dirt
// where it is not, its channel dug and filled from a bucket poured in the
// middle (water flows seven blocks along it: one bucket fills ten) — the
// bucket made of the mines' iron, filled at the nearest water in sight. With
// no bucket of water to be had, the field is dry (wheat grows on dry farmland
// too, slower) and its channel left for later.
func (r *robot) waterForFarm() (world.BlockPos, error) {
	soilAt := func(p world.BlockPos) bool {
		s, ok := r.world.BlockAt(p)
		a, ok2 := r.world.BlockAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z})
		if !ok || !ok2 || !block.Replaceable(a) || block.FluidOf(a) != nil {
			return false
		}
		// snow on it, or snowy grass: the cold heights, no field
		if block.StateList[a].ID() == "minecraft:snow" || strings.Contains(world.StateString(s), "snowy=true") {
			return false
		}
		switch block.StateList[s].ID() {
		case "minecraft:grass_block", "minecraft:dirt", "minecraft:farmland":
			return true
		}
		return false
	}
	p := r.player.Position()
	at := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	if r.home != nil {
		at = r.home.front
	}
	// a home in the cold (a snowy mountain's slope): the field down in the
	// nearest warm land, where its water does not freeze
	if r.coldAt(at) {
		if w, ok := r.warmLand(at, at.Y+8); ok {
			plan("farm: home in the cold at %v: the field in the warm land at %v", at, w)
			if g, ok := r.groundAt(w.X, w.Z, w.Y); ok {
				w = g
			}
			if err := r.goTo(w); err != nil {
				plan("farm: to the warm land: %v", err)
			}
			at = w
		} else {
			plan("farm: home in the cold at %v, no warm land known: near home", at)
		}
	}
	const near = 32
	// nice ground only, as a person picks a field: flat (no hole deeper
	// than a block, few to fill, little to dig off), in the sun (wheat grows
	// only in light; a plot cut into a hill is shaded by its banks), the
	// least to prepare, then the nearest; rough ground only where there is
	// none (and prepared all the same)
	plot, plotN, plotD := world.BlockPos{}, -1, math.MaxFloat64
	bestPrep := math.MaxInt
	for _, strict := range []bool{true, false} {
		for dy := -8; dy <= 6; dy++ {
			for dx := -near; dx <= near; dx++ {
				for dz := -near; dz <= near; dz++ {
					c := world.BlockPos{X: at.X + dx, Y: at.Y - 1 + dy, Z: at.Z + dz}
					// a plot's middle: ground open above, clear of its own works
					// (preparing over the shelter or the stairs opens them)
					if !r.standable(world.BlockPos{X: c.X, Y: c.Y + 1, Z: c.Z}) || r.nearWorks(c, 12) {
						continue
					}
					if r.home != nil && c.Y > r.home.y+8 {
						continue // up the mountain from home: lower, where the snow is not
					}
					// in the cold its water freezes: the pool's end, the channel's middle and far end
					if ch := fieldWater(c); r.coldAt(c) || r.coldAt(fieldPool(c)[0]) || r.coldAt(ch[len(ch)-1]) {
						continue
					}
					holes, deep, dig, dark, n := 0, 0, 0, 0, 0
					rough := false
					for _, q := range fieldGround(c) {
						if r.openAt(q) || r.fluidAt(q) {
							holes++
							if r.openAt(world.BlockPos{X: q.X, Y: q.Y - 1, Z: q.Z}) {
								deep++
							}
						} else if soilAt(q) {
							n++
						}
						for h := 1; h <= 3; h++ {
							// trees are cut as the plot is prepared: work, not rough
							if up := (world.BlockPos{X: q.X, Y: q.Y + h, Z: q.Z}); !r.openAt(up) && !r.vegetationAt(up) {
								dig++
							}
						}
						if deep > 0 || strict && (holes > 8 || dig > 12) {
							rough = true // a hole deeper than a block: never built over
							break
						}
					}
					if rough {
						continue
					}
					if strict {
						for _, q := range append(fieldSoil(c), fieldWater(c)...) {
							if r.terrainOver(q, 24) {
								dark++
							}
						}
						if dark > 6 {
							continue // under a hill's overhang: in shade for good
						}
					}
					prep := 2*holes + dig
					d := math.Hypot(float64(dx), float64(dz))
					if prep < bestPrep || prep == bestPrep && (n > plotN || n == plotN && d < plotD) {
						plot, plotN, plotD, bestPrep = c, n, d, prep
					}
				}
			}
		}
		if plotN >= 0 {
			break
		}
		plan("farm: no flat, sunny plot near %v: the flattest then (no deep hole), prepared", at)
	}
	if plotN < 0 {
		return world.BlockPos{}, fmt.Errorf("no ground for a plot near %v", at)
	}
	plan("farm: a plot at %v, %d of %d soil, %d to prepare", plot, plotN, len(fieldGround(plot)), bestPrep)
	// its ground prepared nicely before any water: flat, solid, clear —
	// and none at all where it cannot be (never built up over a hole)
	if err := r.preparePlot(plot); err != nil {
		return world.BlockPos{}, err
	}
	// its pool and channel; without a bucket yet, a dry field for now (the
	// harvest makes them later)
	if err := r.makeWater(plot); err != nil {
		plan("farm: water: %v: a dry field at %v for now", err, plot)
	}
	return plot, nil
}

// fieldWaterAt reports whether p is a cell of its field's pool or channel
// (or one being made): its own water, never sealed off.
func (r *robot) fieldWaterAt(p world.BlockPos) bool {
	if r.avoid != nil && r.avoid[p] {
		return true
	}
	if r.field != nil {
		for _, w := range append(fieldPool(*r.field), fieldWater(*r.field)...) {
			if w == p {
				return true
			}
		}
	}
	return false
}

// isSource reports whether p holds still water (a source).
func (r *robot) isSource(p world.BlockPos) bool {
	s, ok := r.world.BlockAt(p)
	if !ok {
		return false
	}
	f := block.FluidOf(s)
	return f != nil && f.Source && (f.Name == "minecraft:water" || f.Name == "minecraft:flowing_water")
}

// fieldWet reports whether the field's pool and channel are all still water.
func (r *robot) fieldWet(c world.BlockPos) bool {
	for _, p := range append(fieldPool(c), fieldWater(c)...) {
		if !r.isSource(p) {
			return false
		}
	}
	return true
}

// makeWater makes the field's water, as a person does by a dry field: the
// pool and the channel dug a block deep, their floor and sides shut; a bucket
// of water poured in two opposite corners of the pool (fetched from wherever
// there is water — the pool's first one is no source to take from: alone it
// runs dry); the other two corners fill of themselves, an endless source;
// then the channel filled from it a bucket at a time.
func (r *robot) makeWater(c world.BlockPos) error {
	r.nearTo(c)
	if r.ui.Count("minecraft:water_bucket") == 0 {
		if err := r.get("minecraft:bucket", 1, 1); err != nil {
			return fmt.Errorf("a bucket: %w", err)
		}
	}
	// dug from beside, never standing on a cell still to dig
	r.avoid = map[world.BlockPos]bool{}
	for _, w := range append(fieldPool(c), fieldWater(c)...) {
		r.avoid[w] = true
	}
	defer func() { r.avoid = nil }()
	for _, w := range append(fieldPool(c), fieldWater(c)...) {
		if r.openAt(w) || r.isSource(w) {
			continue
		}
		if s, ok := r.world.BlockAt(w); ok && block.FluidOf(s) != nil {
			continue
		}
		if err := r.reach(w); err != nil {
			return fmt.Errorf("dig %v: %w", w, err)
		}
		if err := r.clear(w); err != nil {
			return fmt.Errorf("dig %v: %w", w, err)
		}
	}
	for _, b := range fieldShut(c) {
		if !r.openAt(b) {
			continue
		}
		if err := r.reach(b); err != nil {
			return fmt.Errorf("shut %v: %w", b, err) // not poured: water let out runs away
		}
		if err := r.fillSoil(b); err != nil {
			return fmt.Errorf("shut %v: %w", b, err)
		}
	}
	// the pool's two corners, a bucket each from away from the field
	pool := fieldPool(c)
	for _, p := range pool[:2] {
		if r.isSource(p) {
			continue
		}
		if r.ui.Count("minecraft:water_bucket") == 0 {
			r.keepOut = fieldBox(c, 1)
			err := r.get("minecraft:water_bucket", 1, 0)
			r.keepOut = nil
			if err != nil {
				return fmt.Errorf("water for the pool: %w", err)
			}
		}
		if err := r.pour(p); err != nil {
			return err
		}
	}
	_ = r.ctl.WaitTicks(r.ctx, 20) // the other two fill of themselves
	n := 0
	for _, p := range pool {
		if r.isSource(p) {
			n++
		}
	}
	plan("farm: the pool at %v: %d of 4 still water", pool[0], n)
	if n < 4 {
		return fmt.Errorf("the pool at %v holds %d of 4", pool[0], n)
	}
	// the channel, from the pool, a bucket at a time
	for try := 0; try < 2*len(fieldWater(c)); try++ {
		var next *world.BlockPos
		for _, w := range fieldWater(c) {
			if !r.isSource(w) {
				w := w
				next = &w
				break
			}
		}
		if next == nil {
			break
		}
		if r.ui.Count("minecraft:water_bucket") == 0 {
			// from a cell of the pool with a place to stand beside it (a
			// trunk may stand at one's edge)
			var err error = fmt.Errorf("no cell of the pool at %v to fill a bucket at", pool[0])
			for _, p := range pool {
				if r.isSource(p) && len(r.beside(p)) > 0 {
					if err = r.fillAt(p); err == nil {
						break
					}
				}
			}
			if err != nil {
				return err
			}
		}
		if err := r.pour(*next); err != nil {
			return err
		}
		_ = r.ctl.WaitTicks(r.ctx, 10)
		if !r.isSource(*next) { // poured and gone: where it runs, to shut it
			plan("farm: %v not still after pouring: %s", *next, r.cellsRound(*next))
		}
	}
	wet := 0
	for _, w := range fieldWater(c) {
		if r.isSource(w) {
			wet++
		}
	}
	plan("farm: the channels at %v, %d of %d still water", c, wet, len(fieldWater(c)))
	return nil
}

// pour empties a water bucket into the open cell p (a block deep: the
// water lands on its floor).
func (r *robot) pour(p world.BlockPos) error {
	if err := r.standBeside(p); err != nil {
		return fmt.Errorf("pour at %v: %w", p, err)
	}
	if _, err := r.toHotbar("minecraft:water_bucket", true); err != nil {
		return fmt.Errorf("pour at %v: %w", p, err)
	}
	if _, err := waitUse(r, func(done func(act.Use)) {
		r.hands.UseItemToward(float64(p.X)+0.5, float64(p.Y)+0.05, float64(p.Z)+0.5, done)
	}); err != nil {
		return fmt.Errorf("pour at %v: %w", p, err)
	}
	_ = r.ctl.WaitTicks(r.ctx, 4)
	plan("farm: water poured at %v", p)
	return nil
}

// nearTo walks to within a few blocks of p when it is far (the water fetched
// from away: back to the field; from afar its land is not even loaded):
// home's front for a place by home, else p itself.
func (r *robot) nearTo(p world.BlockPos) {
	if r.distanceTo(p) <= 24 {
		return
	}
	to := p
	if spots := r.beside(p); len(spots) > 0 { // a place to stand by it, not in it (water)
		to = spots[0]
	}
	if m := r.home; m != nil && math.Hypot(float64(m.front.X-p.X), float64(m.front.Z-p.Z)) < 40 {
		to = m.front
	}
	plan("to %v first (%.0f off)", to, r.distanceTo(p))
	if err := r.goTo(to); err != nil {
		plan("to %v: %v", to, err)
	}
}

// beside are the places to stand right beside cell p, a side of it: solid
// ground at p's level, room for a body over it.
func (r *robot) beside(p world.BlockPos) []world.BlockPos {
	var out []world.BlockPos
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		g := world.BlockPos{X: p.X + d[0], Y: p.Y + 1, Z: p.Z + d[1]}
		if r.standable(g) {
			out = append(out, g)
		}
	}
	return out
}

// standBeside goes to stand right beside cell p, a block over its level:
// from there a bucket aimed at it goes down into it, not against a side
// block on the way (water poured from afar lands outside the pool and runs).
func (r *robot) standBeside(p world.BlockPos) error {
	r.nearTo(p)
	spots := r.beside(p)
	if len(spots) == 0 {
		return fmt.Errorf("nowhere to stand beside %v", p)
	}
	pp := r.player.Position()
	sort.Slice(spots, func(i, j int) bool {
		return math.Hypot(float64(spots[i].X)+0.5-pp.X, float64(spots[i].Z)+0.5-pp.Z) < math.Hypot(float64(spots[j].X)+0.5-pp.X, float64(spots[j].Z)+0.5-pp.Z)
	})
	var last error
	for _, g := range spots {
		if last = r.goTo(g); last == nil {
			r.centre()
			return nil
		}
	}
	return last
}

// fillAt fills an empty bucket at the still water p (the field's pool).
func (r *robot) fillAt(p world.BlockPos) error {
	if !r.isSource(p) {
		return fmt.Errorf("no still water at %v to fill a bucket at", p)
	}
	if err := r.standBeside(p); err != nil {
		return fmt.Errorf("fill at %v: %w", p, err)
	}
	if _, err := r.toHotbar("minecraft:bucket", true); err != nil {
		return fmt.Errorf("fill at %v: %w", p, err)
	}
	if _, err := waitUse(r, func(done func(act.Use)) {
		r.hands.UseItemToward(float64(p.X)+0.5, float64(p.Y)+0.8, float64(p.Z)+0.5, done)
	}); err != nil {
		return fmt.Errorf("fill at %v: %w", p, err)
	}
	for t := 0; t < 20 && r.ui.Count("minecraft:water_bucket") == 0; t++ {
		if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
			return err
		}
	}
	if r.ui.Count("minecraft:water_bucket") == 0 {
		return fmt.Errorf("the bucket did not fill at %v", p)
	}
	return nil
}

// fillSoil fills a hole in the field with dirt (it is to be soil), or what
// else it has.
func (r *robot) fillSoil(p world.BlockPos) error {
	// only on solid ground: a hole a block deep filled, a wall set on the
	// ground — never a column up from below, a structure on a bridge
	if under := (world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}); r.openAt(under) || r.fluidAt(under) {
		return fmt.Errorf("nothing solid under %v: not built up from below", p)
	}
	return r.placeSoil(p)
}

// placeSoil puts dirt at p (any fill block when it has no dirt).
func (r *robot) placeSoil(p world.BlockPos) error {
	if r.ui.Count("minecraft:dirt") > 0 {
		if _, err := r.toHotbar("minecraft:dirt", true); err == nil {
			if _, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(p, done) }); err == nil {
				return nil
			}
		}
	}
	return r.fill(p)
}

// soilAt reports whether p is soil a crop grows in: grass, dirt, farmland.
func (r *robot) soilAt(p world.BlockPos) bool {
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

// plotUnready counts the cells of the field's ground not ready: no solid
// ground at the level (the water's cells: no solid floor), a soil cell no
// soil (a field grown: its new strip not yet prepared).
func (r *robot) plotUnready(c world.BlockPos) int {
	water := map[world.BlockPos]bool{}
	for _, w := range append(fieldPool(c), fieldWater(c)...) {
		water[w] = true
	}
	soil := map[world.BlockPos]bool{}
	for _, s := range fieldSoil(c) {
		soil[s] = true
	}
	n := 0
	for _, at := range fieldGround(c) {
		switch {
		case water[at]:
			if r.openAt(world.BlockPos{X: at.X, Y: at.Y - 1, Z: at.Z}) {
				n++
			}
		case r.openAt(at), soil[at] && !r.soilAt(at) && !r.farmlandAt(at):
			n++
		}
	}
	return n
}

// farmlandAt: tilled soil (a crop on it is soil too).
func (r *robot) farmlandAt(p world.BlockPos) bool {
	s, ok := r.world.BlockAt(p)
	return ok && int(s) < len(block.StateList) && block.StateList[s].ID() == "minecraft:farmland"
}

// cellsRound names what is at p and round it (its four sides, under, over):
// where poured water runs away to.
func (r *robot) cellsRound(p world.BlockPos) string {
	var parts []string
	for _, o := range [6][3]int{{0, 0, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, -1, 0}} {
		q := world.BlockPos{X: p.X + o[0], Y: p.Y + o[1], Z: p.Z + o[2]}
		if s, ok := r.world.BlockAt(q); ok {
			parts = append(parts, fmt.Sprintf("%d,%d,%d=%s", o[0], o[1], o[2], strings.TrimPrefix(world.StateString(s), "minecraft:")))
		}
	}
	return strings.Join(parts, " ")
}

// vegetation reports whether the block named id is of a tree (log, wood,
// leaves) or a big mushroom: cut as a plot is prepared, no sign of rough
// ground. By name: the server's tags come only once it has joined.
func vegetation(id string) bool {
	for _, suf := range []string{"_log", "_wood", "_leaves", "_mushroom_block", "mushroom_stem"} {
		if strings.HasSuffix(id, suf) {
			return true
		}
	}
	return false
}

func (r *robot) vegetationAt(p world.BlockPos) bool {
	s, ok := r.world.BlockAt(p)
	return ok && int(s) < len(block.StateList) && vegetation(block.StateList[s].ID())
}

// terrainOver reports whether earth or stone (not a tree, which is cut)
// stands over p within h blocks: an overhang, a hill's shade for good.
func (r *robot) terrainOver(p world.BlockPos, h int) bool {
	for y := 1; y <= h; y++ {
		q := world.BlockPos{X: p.X, Y: p.Y + y, Z: p.Z}
		if !r.openAt(q) && !r.vegetationAt(q) && !r.fluidAt(q) {
			return true
		}
	}
	return false
}

// fellTreesAround cuts down every tree in the field and m blocks round it,
// whole, as a person clears a field's edge: crops want the sun, and a trunk
// left over a plot is a block floating in the air. The logs from the bottom up
// (standing in a felled trunk's place it reaches the logs over it); the
// leaves, without a log, fall of themselves. It answers how many logs it cut.
func (r *robot) fellTreesAround(c world.BlockPos, m int) (int, error) {
	b := fieldBox(c, m)
	var logs []world.BlockPos
	for y := c.Y - 1; y <= c.Y+24; y++ {
		for x := b[0].X; x <= b[1].X; x++ {
			for z := b[0].Z; z <= b[1].Z; z++ {
				if p := (world.BlockPos{X: x, Y: y, Z: z}); r.logAt(p) {
					logs = append(logs, p)
				}
			}
		}
	}
	sort.SliceStable(logs, func(i, j int) bool { return logs[i].Y < logs[j].Y })
	cut := 0
	for _, p := range logs {
		if !r.logAt(p) {
			continue
		}
		if err := r.dayWork(); err != nil {
			return cut, err // dusk: home, the rest tomorrow
		}
		cut += r.fellTree(p) // the whole tree, its crown from a pillar
	}
	return cut, nil
}

// logAt: a tree's log (or wood) at p.
func (r *robot) logAt(p world.BlockPos) bool {
	s, ok := r.world.BlockAt(p)
	if !ok || int(s) >= len(block.StateList) {
		return false
	}
	id := block.StateList[s].ID()
	return strings.HasSuffix(id, "_log") || strings.HasSuffix(id, "_wood")
}
