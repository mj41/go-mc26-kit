package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/path"
	"github.com/mj41/go-mc26-kit/bot/recipes"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/loot"
	"github.com/mj41/go-mc26/data/recipe"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/protocol/types"
)

// The goals: what the robot does with what it can do. `get` makes sure it
// has some of an item — from its inventory, by crafting it from the recipe
// book (getting the ingredients first, a crafting table when the recipe needs
// one), or by digging the blocks that drop it; `build hut` walls itself in.

// dropSources are the blocks whose loot table gives item with a plain hand
// or tool (no silk touch, no shears, not by chance): what to dig for it.
func dropSources(item string) []string {
	if src := sureSources(item); len(src) > 0 {
		return src
	}
	return loot.DroppedByChance(item) // seeds from grass: many broken for a few
}

// sureSources are the blocks that give item for sure, crops aside.
func sureSources(item string) []string {
	var out []string
	for _, b := range loot.DroppedBy(item) {
		if !isCrop(b) {
			out = append(out, b)
		}
	}
	return out
}

// byChance: the blocks give item only now and then.
func byChance(item string) bool { return len(sureSources(item)) == 0 }

func plan(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	log.Printf("plan: %s", line)
	events.emit("plan", map[string]any{"line": strings.TrimLeft(line, " ")})
}

// errNoWay: neither the book nor the blocks in sight give the item.
var errNoWay = errors.New("no way")

// get makes sure the robot has at least n of item; when nothing in sight
// gives it, the robot goes looking — leg after leg one way, as a person does
// who spawned where there is nothing to dig (explore).
func (r *robot) get(item string, n, depth int) error {
	if err := r.diedInOrder(); err != nil {
		return err
	}
	err := r.getHere(item, n, depth)
	// none in sight: where it knows some to be, first (its map)
	if has := areaHas(item); errors.Is(err, errNoWay) && depth == 0 && has != nil && r.goToKnown(item, has) {
		err = r.getHere(item, n, depth)
	}
	for tries := 0; errors.Is(err, errNoWay) && depth == 0 && tries < 24; tries++ {
		if e := r.explore(tries); errors.Is(e, errLate) {
			return e // home for the night: tomorrow
		} else if e != nil {
			plan("explore: %v", e)
			continue
		}
		err = r.getHere(item, n, depth)
	}
	return err
}

// explore walks one leg of its search: to the next place of rings round
// home (or round where the search began, without a home), eight places a ring
// — north, north-east, east… — the first ring 32 blocks off, each next 32
// further: all four ways covered, nearest first, never round in a circle.
// A place whose area its map knows already is passed over; one in water (no
// ground) is too. The caller looks again after each leg.
func (r *robot) explore(try int) error {
	if err := r.dayWork(); err != nil { // no leg out with the light going
		return err
	}
	// a leg of the search starts out under the sky: from in its shelter or
	// down its mine, a goal far over the land is reached by digging through
	// rock (or down the stairs, the nearest it gets)
	r.outFirst()                          // in its room by day: out by the door, then the leg
	if !r.underSky() && !r.skyWithin(8) { // under trees, an overhang: out still
		return fmt.Errorf("%w: not out under the sky to look round", errNoWay)
	}
	if h := r.player.Status().Health; h < 10 && r.countAll(foods) == 0 {
		return fmt.Errorf("%w: health %.1f and nothing to eat: not looking far", errNoWay, h)
	}
	if !r.search.on {
		p := r.player.Position()
		r.search = searchState{on: true, center: world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))}}
		if r.home != nil {
			r.search.center = r.home.front
		}
		plan("explore: rings round %v", r.search.center)
	}
	failed := 0
	for skip := 0; skip < 64 && failed < 2; skip++ {
		c := r.search.center
		radius := 32 * (r.search.ring + 1)
		// a place every 64 blocks round the ring (eight at least): the land
		// between two in sight from one or the other
		n := max(8, int(math.Round(2*math.Pi*float64(radius)/64)))
		a := float64(r.search.point)*2*math.Pi/float64(n) - math.Pi/2 // north first, then clockwise
		x, z := c.X+int(math.Round(math.Cos(a)*float64(radius))), c.Z+int(math.Round(math.Sin(a)*float64(radius)))
		if r.search.point++; r.search.point >= n {
			r.search.point, r.search.ring = 0, r.search.ring+1
		}
		if _, known := r.atlas.get(keyOf(x, z)); known && skip < 63 {
			continue // seen already: the next
		}
		ground, ok := r.groundAt(x, z, c.Y)
		if !ok && !r.world.HasChunk(world.BlockPos{X: x, Z: z}.Chunk()) {
			// out of sight yet: walked toward, legs at a time (the ground
			// is found on the way); skipped, every far ring would be
			ground, ok = world.BlockPos{X: x, Y: c.Y, Z: z}, true
		}
		if !ok {
			continue // water: the next
		}
		plan("explore: ring %d, %s, to %d %d %d", r.search.ring+1, compass(a), ground.X, ground.Y, ground.Z)
		if _, err := cmdGoto(r, []string{strconv.Itoa(ground.X), strconv.Itoa(ground.Y), strconv.Itoa(ground.Z)}); err != nil {
			plan("explore: %v", err)
			failed++ // two legs that went nowhere: not a third (the rings run away)
			continue
		}
		r.learn()
		return nil
	}
	return fmt.Errorf("no place to go on the rings round %v", r.search.center)
}

// searchState is the robot's search for what is not in sight: rings round
// its centre, the ring and the place on it to go to next.
type searchState struct {
	on     bool
	center world.BlockPos
	ring   int
	point  int
}

// compass names a heading: "east", "south-west", …
func compass(a float64) string {
	names := []string{"east", "south-east", "south", "south-west", "west", "north-west", "north", "north-east"}
	k := int(math.Round(a/(math.Pi/4))) % 8
	if k < 0 {
		k += 8
	}
	return names[k]
}

// inBounds reports whether p is within the bounds set for the work at hand
// (r.bounds: seeds round the field), true without any.
func (r *robot) inBounds(p world.BlockPos) bool {
	b := r.bounds
	return b == nil || math.Hypot(float64(p.X-b.at.X), float64(p.Z-b.at.Z)) <= b.r
}

// groundNear is a place to stand at column (x, z) near height y — the same
// level first, then a step or two up or down: in a tunnel the way along it,
// not the hilltop over it (groundAt's highest).
func (r *robot) groundNear(x, z, y int) (world.BlockPos, bool) {
	for _, dy := range []int{0, 1, -1, 2, -2, 3, -3} {
		if pos := (world.BlockPos{X: x, Y: y + dy, Z: z}); r.standable(pos) {
			return pos, true
		}
	}
	return world.BlockPos{}, false
}

// groundAt is where a player stands at column (x, z): the highest place
// with something solid under it, no fluid and room above, within reach of y.
func (r *robot) groundAt(x, z, y int) (world.BlockPos, bool) {
	for dy := 32; dy >= -48; dy-- {
		pos := world.BlockPos{X: x, Y: y + dy, Z: z}
		if r.standable(pos) {
			return pos, true
		}
	}
	return world.BlockPos{}, false
}

func (r *robot) getHere(item string, n, depth int) error {
	if item == "minecraft:water_bucket" && r.ui.Count(item) < n {
		return r.fillBucket(depth)
	}
	have := r.ui.Count(item)
	if have >= n {
		return nil
	}
	if depth > 8 {
		return fmt.Errorf("%s: too deep", item)
	}
	plan("%sget %s %d (have %d)", indent(depth), item, n, have)
	if rec, ok := r.recipeFor(item, depth); ok {
		return r.craftGoal(item, n, rec, depth)
	}
	// dig it where a block that drops it is in sight; make it otherwise (planks
	// drop from planks, but a person makes them from logs)
	src := dropSources(item)
	if len(src) > 0 {
		if _, ok := r.nearestWhere(src, nil, r.exposed); ok {
			return r.digGoal(item, n, src, depth)
		}
	}
	if err, ok := r.unlockGoal(item, n, depth); ok {
		return err
	}
	if err, ok := r.smeltGoal(item, n, depth); ok {
		return err
	}
	// deep down, an ore not in sight is mined for: a tunnel until it shows
	if len(src) > 0 && r.player.Position().Y < 40 {
		if err := r.branchMine(src, 4); err != nil {
			return err
		}
		return r.digGoal(item, n, src, depth)
	}
	if len(src) > 0 {
		return fmt.Errorf("%w: no %v in sight", errNoWay, src)
	}
	return fmt.Errorf("%w to get %s", errNoWay, item)
}

// unlockGoal gets an item the recipe book does not have yet. The server puts
// a recipe in the book once the player has its ingredients (a log picked up
// unlocks planks; planks, sticks and the crafting table): from the game's own
// recipes (data/recipe) it gets those first, then crafts from the book. ok is
// false when no recipe of the item can be had here.
func (r *robot) unlockGoal(item string, n, depth int) (error, bool) {
	for _, rec := range recipe.For(item) {
		if rec.Kind != "minecraft:crafting_shaped" && rec.Kind != "minecraft:crafting_shapeless" {
			continue
		}
		crafts := (n + rec.Count - 1) / rec.Count
		// the distinct slots, with how many crafts need of each
		type slotNeed struct {
			slot []string
			n    int
		}
		var slots []slotNeed
		ok := true
		for _, slot := range rec.Ingredients {
			if slot == nil {
				continue
			}
			// (ingots from a block made of the ingots: round in a circle)
			if o := r.knownOption(slot, depth); o == "" || r.ui.Count(o) == 0 && r.madeFrom(o, item) {
				ok = false
				break
			}
			found := false
			for i := range slots {
				if slices.Equal(slots[i].slot, slot) {
					slots[i].n += crafts
					found = true
				}
			}
			if !found {
				slots = append(slots, slotNeed{slot, crafts})
			}
		}
		if !ok {
			continue
		}
		plan("%s%s is not in the book yet: its ingredients first", indent(depth), item)
		for _, sn := range slots {
			// one option after another: coal ore on a cliff out of reach, then
			// charcoal from the logs around
			var last error
			tried := map[string]bool{}
			for {
				o := r.knownOptionExcept(sn.slot, depth, tried)
				if o == "" {
					break
				}
				if last = r.get(o, sn.n, depth+1); last == nil {
					break
				}
				plan("%s%s: %v; another way", indent(depth), o, last)
				tried[o] = true
			}
			if last != nil {
				return last, true
			}
		}
		if rec, ok := r.recipeFor(item, depth); ok {
			return r.craftGoal(item, n, rec, depth), true
		}
		// some recipes come to the book late (a torch with the stone
		// pickaxe): laid into the grid by hand, as a person who knows them does
		return r.craftByHand(item, rec, crafts, depth), true
	}
	return nil, false
}

// knownOption picks the item for a slot of a recipe the book may not have:
// one the robot has, else one it can get — dig, or make by a recipe of the
// book or of the game.
func (r *robot) knownOption(slot []string, depth int) string {
	return r.knownOptionExcept(slot, depth, nil)
}

// knownOptionExcept is knownOption without the items in skip.
func (r *robot) knownOptionExcept(slot []string, depth int, skip map[string]bool) string {
	ing := recipes.Ingredient{}
	for _, s := range slot {
		if strings.HasPrefix(s, "#") {
			ing.Tags = append(ing.Tags, strings.TrimPrefix(s, "#"))
		} else {
			ing.Items = append(ing.Items, s)
		}
	}
	opts := r.book.Options(ing)
	for _, o := range opts {
		if r.ui.Count(o) > 0 && !skip[o] {
			return o
		}
	}
	for _, o := range opts {
		if !skip[o] && r.knownObtainable(o, depth+1) {
			return o
		}
	}
	return ""
}

// knownObtainable is obtainable with the game's recipes as well as the book's.
func (r *robot) knownObtainable(item string, depth int) bool {
	if depth > 6 {
		return false
	}
	if r.ui.Count(item) > 0 || r.obtainable(item, depth) {
		return true
	}
	for _, rec := range recipe.For(item) {
		if rec.Kind != "minecraft:crafting_shaped" && rec.Kind != "minecraft:crafting_shapeless" && rec.Kind != "minecraft:smelting" {
			continue
		}
		ok := true
		for _, slot := range rec.Ingredients {
			if slot != nil && r.knownOption(slot, depth+1) == "" {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func indent(depth int) string {
	return fmt.Sprintf("%*s", depth*2, "")
}

// recipeFor picks the book's recipe for item whose ingredients can be had,
// the two-by-two ones first.
func (r *robot) recipeFor(item string, depth int) (recipes.Recipe, bool) {
	rs := r.book.For(item)
	sort.Slice(rs, func(i, j int) bool { return fitsInventory(rs[i]) && !fitsInventory(rs[j]) })
	for _, rec := range rs {
		if rec.Kind != "minecraft:crafting_shaped" && rec.Kind != "minecraft:crafting_shapeless" {
			continue
		}
		ok := true
		for _, ing := range rec.Ingredients {
			o := r.chooseOption(ing, depth)
			// ingots from a block it would make of the ingots: round in a
			// circle, the item never got (smelted, dug) at all
			if o == "" || r.ui.Count(o) == 0 && r.madeFrom(o, item) {
				ok = false
				break
			}
		}
		if ok {
			return rec, true
		}
	}
	return recipes.Recipe{}, false
}

// madeFrom reports whether item is made of other: by a recipe of its book,
// or of the game's (one it has not unlocked yet).
func (r *robot) madeFrom(item, other string) bool {
	for _, rec := range r.book.For(item) {
		for _, ing := range rec.Ingredients {
			if slices.Contains(r.book.Options(ing), other) {
				return true
			}
		}
	}
	for _, rec := range recipe.For(item) {
		for _, slot := range rec.Ingredients {
			if slices.Contains(slot, other) {
				return true
			}
		}
	}
	return false
}

func fitsInventory(rec recipes.Recipe) bool {
	if rec.Kind == "minecraft:crafting_shaped" {
		return rec.Width <= 2 && rec.Height <= 2
	}
	return len(rec.Ingredients) <= 4
}

// chooseOption picks the item to use for an ingredient: the one the robot
// has most of, else one it can get (dig, or craft from what it can dig).
func (r *robot) chooseOption(ing recipes.Ingredient, depth int) string {
	opts := r.book.Options(ing)
	best, bestCount := "", 0
	for _, o := range opts {
		if c := r.ui.Count(o); c > bestCount {
			best, bestCount = o, c
		}
	}
	if best != "" {
		return best
	}
	for _, o := range opts {
		if r.obtainable(o, depth+1) {
			return o
		}
	}
	return ""
}

// obtainable: the robot can dig what drops it nearby, or craft it from
// something obtainable.
func (r *robot) obtainable(item string, depth int) bool {
	if depth > 6 {
		return false
	}
	if src := dropSources(item); len(src) > 0 {
		if _, ok := r.nearestWhere(src, nil, r.exposed); ok {
			return true
		}
	}
	for _, rec := range r.book.For(item) {
		ok := len(rec.Ingredients) > 0
		for _, ing := range rec.Ingredients {
			found := false
			for _, o := range r.book.Options(ing) {
				if r.ui.Count(o) > 0 || r.obtainable(o, depth+1) {
					found = true
					break
				}
			}
			if !found {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// craftGoal crafts item from rec until there are n, getting the ingredients first.
func (r *robot) craftGoal(item string, n int, rec recipes.Recipe, depth int) error {
	need := n - r.ui.Count(item)
	times := (need + rec.Count - 1) / rec.Count
	type want struct {
		item string
		n    int
	}
	wants := map[string]int{}
	for _, ing := range rec.Ingredients {
		wants[r.chooseOption(ing, depth)] += times
	}
	var list []want
	for it, k := range wants {
		list = append(list, want{it, k})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].item < list[j].item })
	// a crafting table first, so what it costs is not taken from the ingredients
	var table *world.BlockPos
	if !fitsInventory(rec) {
		pos, err := r.tableNear(depth)
		if err != nil {
			return fmt.Errorf("%s: %w", item, err)
		}
		table = &pos
	}
	// getting one ingredient may use up another (sticks are made of planks):
	// check them all again until they are all there at once
	for round := 0; ; round++ {
		short := false
		for _, w := range list {
			if r.ui.Count(w.item) < w.n {
				short = true
				if err := r.get(w.item, w.n, depth+1); err != nil {
					return fmt.Errorf("%s: %w", item, err)
				}
			}
		}
		if !short {
			break
		}
		if round == 3 {
			return fmt.Errorf("%s: the ingredients keep running short", item)
		}
	}
	opened := false
	if table != nil {
		if err := r.reach(*table); err != nil {
			return fmt.Errorf("%s: %w", item, err)
		}
		if _, err := cmdOpen(r, []string{strconv.Itoa(table.X), strconv.Itoa(table.Y), strconv.Itoa(table.Z)}); err != nil {
			return fmt.Errorf("%s: open the crafting table: %w", item, err)
		}
		opened = true
	}
	plan("%scraft %s %d", indent(depth), item, need)
	made, err := r.ui.Craft(r.ctx, item, need)
	if opened {
		if m, ok := r.screens.Open(); ok {
			_ = r.screens.Close(m.ID)
		}
		_ = r.ctl.WaitTicks(r.ctx, 4)
	}
	if err != nil {
		return fmt.Errorf("craft %s: made %d: %w", item, made, err)
	}
	return nil
}

// tableNear finds a crafting table near, or gets one and places it beside
// the robot.
func (r *robot) tableNear(depth int) (world.BlockPos, error) {
	if pos, ok, err := r.homeBlock("minecraft:crafting_table", depth); ok || err != nil {
		return pos, err
	}
	if pos, ok := r.nearest([]string{"minecraft:crafting_table"}, nil); ok && r.distanceTo(pos) < 12 {
		return pos, nil
	}
	if err := r.get("minecraft:crafting_table", 1, depth+1); err != nil {
		return world.BlockPos{}, err
	}
	if _, err := r.toHotbar("minecraft:crafting_table", true); err != nil {
		return world.BlockPos{}, err
	}
	pos, err := r.placeNextTo()
	if err != nil {
		return world.BlockPos{}, err
	}
	plan("%splaced a crafting table at %v", indent(depth), pos)
	return pos, nil
}

// homeSpot is where the room's plan puts a kind of furniture: the table, the
// chest and the furnace along its right wall (shelter.go).
func (m room) homeSpot(kind string) (world.BlockPos, bool) {
	switch kind {
	case "minecraft:crafting_table":
		return m.cell(2, 1, 0), true
	case "minecraft:chest":
		return m.cell(3, 1, 0), true
	case "minecraft:furnace":
		return m.cell(4, 1, 0), true
	}
	return world.BlockPos{}, false
}

// homeBlock is the room's own furniture of a kind, when the robot is by its
// home: there, and nowhere else (not on the path outside) — the one at its
// place in the room's plan, put back there if it is gone. ok is false away
// from home, or for a kind the plan has no place for.
func (r *robot) homeBlock(kind string, depth int) (world.BlockPos, bool, error) {
	m := r.home
	if m == nil || r.distanceTo(m.centre()) > 24 {
		return world.BlockPos{}, false, nil
	}
	at, ok := m.homeSpot(kind)
	if !ok {
		return world.BlockPos{}, false, nil
	}
	if s, ok := r.world.BlockAt(at); ok && int(s) < len(block.StateList) && block.StateList[s].ID() == kind {
		return at, true, nil
	}
	if !r.openAt(at) {
		return world.BlockPos{}, false, nil // something else at its place: as away from home
	}
	if err := r.get(kind, 1, depth+1); err != nil {
		return world.BlockPos{}, true, err
	}
	if err := r.goTo(m.centre()); err != nil {
		return world.BlockPos{}, true, fmt.Errorf("into the room for its %s: %w", kind, err)
	}
	if _, err := r.toHotbar(kind, true); err != nil {
		return world.BlockPos{}, true, err
	}
	if _, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(at, done) }); err != nil {
		return world.BlockPos{}, true, fmt.Errorf("its %s at %v: %w", kind, at, err)
	}
	plan("%s%s put back at its place in the room, %v", indent(depth), kind, at)
	return at, true, nil
}

// placeNextTo places the held block on the ground beside the robot — at its
// feet's height, a step up or down on a slope — or, among leaves and trunks
// with no room, on level ground a few steps away.
func (r *robot) placeNextTo() (world.BlockPos, error) {
	for try := 0; try < 2; try++ {
		p := r.player.Position()
		x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
		for _, dy := range []int{0, 1, -1} {
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {-1, -1}, {1, -1}, {-1, 1}} {
				pos := world.BlockPos{X: x + d[0], Y: y + dy, Z: z + d[1]}
				if s, ok := r.world.BlockAt(pos); !ok || !block.Replaceable(s) || r.onTheWay(pos) {
					continue
				}
				if _, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(pos, done) }); err == nil {
					return pos, nil
				}
			}
		}
		// walled in (a staircase in rock, its steps kept clear): a niche dug
		// in the wall beside it, and the thing put there
		if try == 0 {
			p := r.player.Position()
			x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				pos := world.BlockPos{X: x + d[0], Y: y, Z: z + d[1]}
				// rock only: not a table put here before, not its chest
				if r.openAt(pos) || r.onTheWay(pos) || r.ownColumn(pos) || !r.isRock(pos) {
					continue
				}
				held := itemName(int32(r.heldStack().Item)) // the pickaxe digs; then this again
				if err := r.clear(pos); err != nil {
					continue
				}
				if _, err := r.toHotbar(held, true); err != nil {
					continue
				}
				if _, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(pos, done) }); err == nil {
					plan("a niche dug at %v for it", pos)
					return pos, nil
				}
			}
		}
		// in rock it cannot dig (no pickaxe): on a step of its own stairs,
		// for now — the walk along them takes it up again
		if try == 0 {
			p := r.player.Position()
			x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
			for _, dy := range []int{0, 1, -1} {
				for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					pos := world.BlockPos{X: x + d[0], Y: y + dy, Z: z + d[1]}
					if s, ok := r.world.BlockAt(pos); !ok || !block.Replaceable(s) || r.ownColumn(pos) {
						continue
					}
					if _, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(pos, done) }); err == nil {
						plan("on its stairs at %v for now", pos)
						return pos, nil
					}
				}
			}
		}
		spot, ok := r.levelSpot(8)
		if !ok {
			break
		}
		plan("no room here: to level ground at %v", spot)
		if _, err := cmdGoto(r, []string{strconv.Itoa(spot.X), strconv.Itoa(spot.Y), strconv.Itoa(spot.Z)}); err != nil {
			break
		}
	}
	return world.BlockPos{}, errors.New("no place to put it")
}

// toHotbar finds item in the hotbar, moving it there from the inventory with
// a swap when it is not; hold selects it.
func (r *robot) toHotbar(item string, hold bool) (int, error) {
	slot := -1
	r.screens.Lock()
	for i := 0; i < 9; i++ {
		s := r.screens.Inventory.Slots[36+i]
		if s.Count > 0 && itemName(int32(s.Item)) == item {
			slot = i
			break
		}
	}
	inv := -1
	if slot < 0 {
		for i := 9; i < 36; i++ {
			s := r.screens.Inventory.Slots[i]
			if s.Count > 0 && itemName(int32(s.Item)) == item {
				inv = i
				break
			}
		}
	}
	r.screens.Unlock()
	if slot < 0 {
		if inv < 0 {
			return 0, fmt.Errorf("no %s", item)
		}
		slot = 8
		if err := r.screens.Click(0, inv, slot, types.ContainerInputSwap); err != nil {
			return 0, err
		}
		if err := r.ctl.WaitTicks(r.ctx, 4); err != nil {
			return 0, err
		}
	}
	if hold {
		if err := r.screens.SelectHotbar(slot); err != nil {
			return 0, err
		}
	}
	return slot, nil
}

// nearest finds the nearest block of one of names around the robot — 64
// blocks out, 24 down and 16 up, what a person sees from a hill — skipping
// those in skip.
func (r *robot) nearest(names []string, skip map[world.BlockPos]bool) (world.BlockPos, bool) {
	return r.nearestWhere(names, skip, nil)
}

// nearestWhere is nearest among the blocks ok accepts (nil: all).
func (r *robot) nearestWhere(names []string, skip map[world.BlockPos]bool, ok func(world.BlockPos) bool) (world.BlockPos, bool) {
	accept := ok
	want := map[block.StateID]bool{}
	for _, n := range names {
		b, ok := block.FromID[n]
		if !ok {
			continue
		}
		for id, s := range block.StateList {
			if s.ID() == b.ID() {
				want[block.StateID(id)] = true
			}
		}
	}
	p := r.player.Position()
	cx, cy, cz := int(math.Floor(p.X)), int(math.Floor(p.Y)), int(math.Floor(p.Z))
	best, bestD, found := world.BlockPos{}, math.MaxFloat64, false
	for dy := -24; dy <= 16; dy++ {
		for dx := -64; dx <= 64; dx++ {
			for dz := -64; dz <= 64; dz++ {
				pos := world.BlockPos{X: cx + dx, Y: cy + dy, Z: cz + dz}
				s, ok := r.world.BlockAt(pos)
				if !ok || !want[s] || skip[pos] || accept != nil && !accept(pos) {
					continue
				}
				if pos.X == cx && pos.Z == cz && pos.Y == cy-1 {
					continue // not the block under its own feet
				}
				if d := float64(dx*dx + dy*dy*4 + dz*dz); d < bestD {
					best, bestD, found = pos, d, true
				}
			}
		}
	}
	return best, found
}

func (r *robot) distanceTo(pos world.BlockPos) float64 {
	p := r.player.Position()
	return math.Hypot(float64(pos.X)+0.5-p.X, float64(pos.Z)+0.5-p.Z)
}

// reach walks to a place from which the block at pos is within reach (4.2
// from the eye to its centre), unless it is within reach already.
func (r *robot) reach(pos world.BlockPos) error { return r.reachFrom(pos, true) }

// reachGround is reach from the ground only, never a place on a tree (its
// crown, a trunk): a tree's log high up is cut from a pillar, not from the
// tree's own leaves.
func (r *robot) reachGround(pos world.BlockPos) error { return r.reachFrom(pos, false) }

func (r *robot) reachFrom(pos world.BlockPos, onTree bool) error {
	within := func(x, y, z float64) bool {
		ex, ey, ez := x, y+1.62, z
		cx, cy, cz := float64(pos.X)+0.5, float64(pos.Y)+0.5, float64(pos.Z)+0.5
		return math.Sqrt((cx-ex)*(cx-ex)+(cy-ey)*(cy-ey)+(cz-ez)*(cz-ez)) <= 4.2
	}
	// not on a cell it is to dig (the field's pool and channel): the robot
	// digs nothing under itself
	over := func(x, y, z float64) bool {
		feet := world.BlockPos{X: int(math.Floor(x)), Y: int(math.Floor(y + 1e-6)), Z: int(math.Floor(z))}
		// over its field's water (r.avoid with it), or in it: no place to stand
		return r.fieldWaterAt(feet) || r.fieldWaterAt(world.BlockPos{X: feet.X, Y: feet.Y - 1, Z: feet.Z})
	}
	p := r.player.Position()
	g := r.walker.Graph
	// where it stands, when not on a tree it is to keep off
	here := func() bool {
		if onTree {
			return true
		}
		n, ok := g.Start(p.X, p.Y, p.Z)
		return !ok || !g.OnTree(n)
	}
	if within(p.X, p.Y, p.Z) && !over(p.X, p.Y, p.Z) && here() {
		return nil
	}
	var cells []path.Node
	for dy := -2; dy <= 1; dy++ {
		for dx := -2; dx <= 2; dx++ {
			for dz := -2; dz <= 2; dz++ {
				c := world.BlockPos{X: pos.X + dx, Y: pos.Y + dy, Z: pos.Z + dz}
				if dx == 0 && dz == 0 {
					continue
				}
				if n, ok := g.Start(float64(c.X)+0.5, float64(c.Y), float64(c.Z)+0.5); ok && n.Pos == c && within(float64(c.X)+0.5, n.Floor, float64(c.Z)+0.5) && !over(float64(c.X)+0.5, n.Floor, float64(c.Z)+0.5) && (onTree || !g.OnTree(n)) {
					cells = append(cells, n)
				}
			}
		}
	}
	// the ground's places first, the nearest; one on a tree (its crown, a
	// trunk) only when the ground has none
	sort.Slice(cells, func(i, j int) bool {
		if ti, tj := g.OnTree(cells[i]), g.OnTree(cells[j]); ti != tj {
			return tj
		}
		di := math.Hypot(float64(cells[i].Pos.X)+0.5-p.X, float64(cells[i].Pos.Z)+0.5-p.Z)
		dj := math.Hypot(float64(cells[j].Pos.X)+0.5-p.X, float64(cells[j].Pos.Z)+0.5-p.Z)
		return di < dj
	})
	var last error
	if len(cells) > 4 {
		cells = cells[:4] // the nearest: each try is a whole search
	}
	for _, c := range cells {
		_, err := cmdGoto(r, []string{strconv.Itoa(c.Pos.X), strconv.Itoa(c.Pos.Y), strconv.Itoa(c.Pos.Z)})
		if err == nil {
			return nil
		}
		last = err
	}
	if last == nil {
		return fmt.Errorf("cannot get within reach of %v: no place to stand near it", pos)
	}
	if a, err := cmdAround(r, nil); err == nil {
		plan("stuck? %s", a)
	}
	return fmt.Errorf("cannot get within reach of %v: %d places near it, the last: %v", pos, len(cells), last)
}

// holdBestTool selects the hotbar item that mines state fastest and gets its drop.
func (r *robot) holdBestTool(state block.StateID) error {
	r.screens.Lock()
	speedOf := func(s screen.Slot) float32 {
		if s.Count <= 0 {
			s = screen.Slot{}
		}
		speed, correct := act.ToolSpeed(&r.client.Tags, s, state)
		if !correct {
			speed /= 10
		}
		return speed
	}
	// the hotbar, then the rest of the inventory (a spare pickaxe)
	best, bestSpeed := -1, float32(0)
	for i := 0; i < 9; i++ {
		if sp := speedOf(r.screens.Inventory.Slots[36+i]); best < 0 || sp > bestSpeed {
			best, bestSpeed = i, sp
		}
	}
	spare, spareSpeed := "", bestSpeed
	for i := 9; i < 36; i++ {
		s := r.screens.Inventory.Slots[i]
		if sp := speedOf(s); s.Count > 0 && sp > spareSpeed {
			spare, spareSpeed = itemName(int32(s.Item)), sp
		}
	}
	held := r.screens.HeldSlot
	// no tool digs it faster than a hand (grass, a flower, leaf litter,
	// wheat): the hand — an empty slot, else one with no tool in it — a
	// tool's wear is not spent on it
	hand := speedOf(screen.Slot{})
	if bestSpeed <= hand*1.01 && (spare == "" || spareSpeed <= hand*1.01) {
		pick := -1
		for i := 0; i < 9 && pick < 0; i++ {
			if r.screens.Inventory.Slots[36+i].Count <= 0 {
				pick = i
			}
		}
		for i := 0; i < 9 && pick < 0; i++ {
			if !isTool(itemName(int32(r.screens.Inventory.Slots[36+i].Item))) {
				pick = i
			}
		}
		r.screens.Unlock()
		if pick >= 0 {
			if pick != held {
				return r.screens.SelectHotbar(pick)
			}
			return nil
		}
		r.screens.Lock()
	}
	r.screens.Unlock()
	if spare != "" {
		_, err := r.toHotbar(spare, true)
		return err
	}
	if best >= 0 && best != held {
		return r.screens.SelectHotbar(best)
	}
	return nil
}

// isTool reports whether an item is a tool that wears with use (a pickaxe,
// an axe, a shovel, a hoe, a sword, shears).
func isTool(item string) bool {
	for _, suf := range []string{"_pickaxe", "_axe", "_shovel", "_hoe", "_sword", ":shears"} {
		if strings.HasSuffix(item, suf) {
			return true
		}
	}
	return false
}

// isRock reports whether pos is natural stone (#minecraft:base_stone_overworld:
// stone, granite, diorite, andesite, tuff, deepslate) — a wall to dig a niche
// in, nothing the robot built.
func (r *robot) isRock(pos world.BlockPos) bool {
	s, ok := r.world.BlockAt(pos)
	return ok && int(s) < len(block.StateList) && slices.Contains(tagBlocks("minecraft:base_stone_overworld"), block.StateList[s].ID())
}

// digToStone digs a short stair down through the soil where it stands till
// stone shows in what it dug (five steps at most): the stone a hill hides
// under its dirt. The steps are not its mine's stairs (not remembered).
func (r *robot) digToStone() error {
	stone := func(pos world.BlockPos) bool {
		s, ok := r.world.BlockAt(pos)
		if !ok || int(s) >= len(block.StateList) {
			return false
		}
		id := block.StateList[s].ID()
		return id == "minecraft:stone" || id == "minecraft:deepslate" || id == "minecraft:cobblestone"
	}
	for _, d := range [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} {
		for step := 0; step < 5; step++ {
			p := r.player.Position()
			x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
			fx, fz := x+d[0], z+d[1]
			if why := r.stepDanger(fx, y, fz); why != "" {
				break
			}
			cells := []world.BlockPos{{X: fx, Y: y + 1, Z: fz}, {X: fx, Y: y, Z: fz}, {X: fx, Y: y - 1, Z: fz}}
			found := false
			for _, c := range cells {
				if stone(c) {
					found = true
				}
			}
			if found {
				plan("stone under the soil at %d %d", fx, fz)
				return nil // in reach now, exposed: the dig takes it
			}
			if !r.fullFloor(world.BlockPos{X: fx, Y: y - 2, Z: fz}) {
				break // a drop under the next step: another way
			}
			ok := true
			for _, c := range cells {
				if err := r.clear(c); err != nil {
					ok = false
					break
				}
			}
			if !ok || r.walkTo(world.BlockPos{X: fx, Y: y - 1, Z: fz}) != nil {
				break
			}
			for _, c := range []world.BlockPos{{X: fx + d[0], Y: y - 1, Z: fz + d[1]}, {X: fx, Y: y - 2, Z: fz}, {X: fx + d[1], Y: y - 1, Z: fz + d[0]}, {X: fx - d[1], Y: y - 1, Z: fz - d[0]}} {
				if stone(c) {
					plan("stone under the soil at %v", c)
					return nil
				}
			}
		}
	}
	return errors.New("no stone within five steps down")
}

// keptOut reports whether p is in the box it keeps out of while it digs.
func (r *robot) keptOut(p world.BlockPos) bool {
	b := r.keepOut
	return b != nil && p.X >= b[0].X && p.X <= b[1].X && p.Y >= b[0].Y && p.Y <= b[1].Y && p.Z >= b[0].Z && p.Z <= b[1].Z
}

// digGoal digs the nearest blocks that drop item until the robot has n.
func (r *robot) digGoal(item string, n int, sources []string, depth int) error {
	skip := map[world.BlockPos]bool{}
	unreached := 0 // in a row
	dugDown := false
	limit := 4*n + 8
	if byChance(item) {
		limit = 16*n + 32
	}
	r.outFirst()
	// the drops picked up in a batch, as a person does: not a walk after
	// each block, but once the blocks dug would make up what is missing, a
	// handful lie about, or the next block is away from them (left behind)
	dug := 0
	var lastDug world.BlockPos
	collect := func() {
		if dug == 0 {
			return
		}
		if _, err := cmdCollect(r, []string{"10"}); err != nil {
			plan("%scollect: %v", indent(depth), err)
		}
		dug = 0
	}
	defer collect()
	for tries := 0; r.ui.Count(item) < n; tries++ {
		r.defend()
		r.eatIfHungry()
		if r.lateOut() { // the day's light gone: home (or walled in), the rest tomorrow
			collect()
			return r.headHome()
		}
		if dug > 0 && (dug >= n-r.ui.Count(item) || dug >= 8) {
			collect()
			continue // counted again with what was picked up
		}
		if tries > limit {
			return fmt.Errorf("dig for %s: gave up after %d tries", item, tries)
		}
		// what is in front, at the feet or above: its drop lands where the
		// robot can walk; one dug below the feet leaves it in a pit
		// and a face of it open — a person digs what they see, not into the
		// rock past it, where the drop would be shut in
		feet := int(math.Floor(r.player.Position().Y + 1e-6))
		// never under it nor over its head: what is beside it or ahead —
		// and nothing in a place it is making (a plot it levels)
		pos, ok := r.nearestWhere(sources, skip, func(p world.BlockPos) bool {
			return p.Y >= feet && r.exposed(p) && !r.ownColumn(p) && !r.keptOut(p) && r.inBounds(p)
		})
		if !ok {
			pos, ok = r.nearestWhere(sources, skip, func(p world.BlockPos) bool { return r.exposed(p) && !r.ownColumn(p) && !r.keptOut(p) && r.inBounds(p) })
		}
		if !ok {
			// the chunks around may still be arriving: look again, once
			if err := r.ctl.WaitTicks(r.ctx, 40); err != nil {
				return err
			}
			if pos, ok = r.nearest(sources, skip); !ok {
				return fmt.Errorf("no %v in sight", sources)
			}
		}
		if dug > 0 && math.Abs(float64(pos.X-lastDug.X))+math.Abs(float64(pos.Z-lastDug.Z)) > 6 {
			collect() // on to a block away from the drops: them first
		}
		// a source it cannot walk to is not dug its way to (a tunnel forty
		// blocks long for coal): the next one, or another way
		climbing := r.climbing
		r.climbing = true
		err := r.reach(pos)
		r.climbing = climbing
		if err != nil {
			// a log high in a trunk cut below it: the tree felled whole,
			// from a pillar in the trunk's place
			if r.logAt(pos) {
				if cut := r.fellTree(pos); cut > 0 {
					plan("%sthe tree out of reach felled from a pillar: %d logs", indent(depth), cut)
					continue
				}
			}
			plan("%s%v out of reach: %v", indent(depth), pos, err)
			skip[pos] = true
			// three in sight and none reached: boxed in where it is (its own
			// tunnel) — another way, or out and looking elsewhere, not the
			// next one it sees and cannot get to
			if unreached++; unreached >= 3 {
				// stone under the soil of a hill: dug down to, once, as a
				// person digs through the dirt for it
				if !dugDown && slices.Contains(sources, "minecraft:stone") {
					dugDown, unreached = true, 0
					if err := r.digToStone(); err != nil {
						plan("%sstone under the soil: %v", indent(depth), err)
					}
					clear(skip)
					continue
				}
				return fmt.Errorf("%w: %d %v in sight, none in reach", errNoWay, unreached, sources)
			}
			continue
		}
		unreached = 0
		state, _ := r.world.BlockAt(pos)
		if err := r.holdBestTool(state); err != nil {
			return err
		}
		if _, correct := act.ToolSpeed(&r.client.Tags, r.heldStack(), state); !correct {
			// stone by hand drops nothing: a pickaxe first
			if err := r.get("minecraft:wooden_pickaxe", 1, depth+1); err != nil {
				return fmt.Errorf("dig %s: %w", world.StateString(state), err)
			}
			continue
		}
		plan("%sdig %s at %v", indent(depth), world.StateString(state), pos)
		if _, err := cmdDig(r, []string{strconv.Itoa(pos.X), strconv.Itoa(pos.Y), strconv.Itoa(pos.Z)}); err != nil {
			skip[pos] = true
			continue
		}
		dug++
		lastDug = pos
		// into a hillside: the block above too, of the same kind, so the hole
		// is high enough to step into for the drops (and gives more)
		// (at the feet's height whatever it is: a tunnel a person walks is two high)
		up := world.BlockPos{X: pos.X, Y: pos.Y + 1, Z: pos.Z}
		if s, ok := r.world.BlockAt(up); ok && r.ui.Count(item) < n && int(s) < len(block.StateList) &&
			(slices.Contains(sources, block.StateList[s].ID()) || pos.Y == feet && len(block.CollisionShape(s)) > 0 && block.FluidOf(s) == nil) {
			if _, correct := act.ToolSpeed(&r.client.Tags, r.heldStack(), s); correct {
				plan("%sdig %s above it", indent(depth), world.StateString(s))
				if _, err := cmdDig(r, []string{strconv.Itoa(up.X), strconv.Itoa(up.Y), strconv.Itoa(up.Z)}); err != nil {
					plan("%sdig above: %v", indent(depth), err)
				} else {
					dug++
				}
			}
		}
		// a tree's log: the tree felled whole, no crown left floating
		if strings.HasSuffix(block.StateList[state].ID(), "_log") && r.ui.Count(item) < n+16 {
			if cut := r.fellTree(up); cut > 0 {
				plan("%sthe tree felled whole: %d logs more", indent(depth), cut)
				dug = 0 // the felling picked up its drops, and these with them
			}
		}
	}
	return nil
}

func (r *robot) heldStack() screen.Slot {
	r.screens.Lock()
	defer r.screens.Unlock()
	return r.screens.Inventory.Slots[36+r.screens.HeldSlot]
}

// cmdGet is `get <item> [n]`.
func cmdGet(r *robot, args []string) (string, error) {
	if len(args) < 1 || len(args) > 2 {
		return "", fmt.Errorf("want: get <item> [n]")
	}
	n, more := 1, false
	if len(args) == 2 {
		// "+16": sixteen more than it has
		more = strings.HasPrefix(args[1], "+")
		v, err := strconv.Atoi(strings.TrimPrefix(args[1], "+"))
		if err != nil {
			return "", err
		}
		n = v
	}
	start := time.Now()
	// "logs": of any tree, as a person takes the trees there are
	if args[0] == "logs" {
		if more {
			n += r.countAll(logItems())
		}
		if err := r.getAny(logItems(), n); err != nil {
			return "", err
		}
		return fmt.Sprintf("have=%d logs in %s", r.countAll(logItems()), time.Since(start).Round(time.Second)), nil
	}
	item := fullName(args[0])
	if more {
		n += r.ui.Count(item)
	}
	if err := r.get(item, n, 0); err != nil {
		return "", err
	}
	return fmt.Sprintf("have=%d in %s", r.ui.Count(item), time.Since(start).Round(time.Second)), nil
}

func (r *robot) countAll(kinds []string) int {
	n := 0
	for _, k := range kinds {
		n += r.ui.Count(k)
	}
	return n
}

// getAny gets n of the kinds together: the nearest kind in sight each time
// (the trees that are there), else a leg of the search and a look again.
func (r *robot) getAny(kinds []string, n int) error {
	// hurt: the trees near home only, not tree after tree away from it
	if r.home != nil && r.player.Status().Health < 10 && r.bounds == nil {
		r.bounds = &bounds{at: r.home.front, r: 48}
		defer func() { r.bounds = nil }()
	}
	for try := 0; try < 24; try++ {
		if err := r.dayWork(); err != nil {
			return err
		}
		have := r.countAll(kinds)
		if have >= n {
			return nil
		}
		best, bestD := "", math.MaxFloat64
		for _, k := range kinds {
			if pos, ok := r.nearestWhere([]string{k}, nil, func(p world.BlockPos) bool { return r.exposed(p) && r.inBounds(p) }); ok {
				if d := r.distanceTo(pos); d < bestD {
					best, bestD = k, d
				}
			}
		}
		if best == "" {
			// none in sight: a wood it knows, first; else the search
			if try > 0 || !r.goToKnown("logs", areaHas("logs")) {
				if err := r.explore(try); err != nil {
					plan("explore: %v", err)
				}
			}
			continue
		}
		plan("get %d more logs: %s, the nearest", n-have, best)
		if err := r.get(best, r.ui.Count(best)+n-have, 0); err != nil {
			plan("logs: %s: %v", best, err)
		}
	}
	if have := r.countAll(kinds); have < n {
		return fmt.Errorf("%w: %d of %d logs", errNoWay, have, n)
	}
	return nil
}

// cmdBuild is `build hut [block]`: a hut around the robot — a ring of eight
// blocks two high and a roof of nine — from what it gets of the block.
func cmdBuild(r *robot, args []string) (string, error) {
	if len(args) < 1 || args[0] != "hut" {
		return "", fmt.Errorf("want: build hut [block]")
	}
	material := "minecraft:cobblestone"
	if len(args) == 2 {
		material = fullName(args[1])
	}
	if err := r.get(material, 25, 0); err != nil {
		return "", err
	}
	// level ground first, as a person picks where to build
	if spot, ok := r.levelSpot(12); ok {
		if _, err := cmdGoto(r, []string{strconv.Itoa(spot.X), strconv.Itoa(spot.Y), strconv.Itoa(spot.Z)}); err != nil {
			plan("hut: to level ground at %v: %v", spot, err)
		}
	}
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	var spots []world.BlockPos
	ring := [][2]int{{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}
	for dy := 0; dy < 2; dy++ {
		for _, d := range ring {
			spots = append(spots, world.BlockPos{X: x + d[0], Y: y + dy, Z: z + d[1]})
		}
	}
	for _, d := range ring {
		spots = append(spots, world.BlockPos{X: x + d[0], Y: y + 2, Z: z + d[1]})
	}
	spots = append(spots, world.BlockPos{X: x, Y: y + 2, Z: z}) // the middle of the roof last
	placed := 0
	for _, s := range spots {
		// what stands there already is wall enough (the material, or a hillside)
		if st, ok := r.world.BlockAt(s); ok && !block.Replaceable(st) {
			continue
		}
		if _, err := r.toHotbar(material, true); err != nil {
			return "", err
		}
		if _, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(s, done) }); err != nil {
			return "", fmt.Errorf("place %v: %w", s, err)
		}
		placed++
	}
	return fmt.Sprintf("placed=%d at %d %d %d", placed, x, y, z), nil
}

// levelSpot is the nearest place within radius where a hut fits: solid
// ground under all nine cells of three by three, at one height, and the
// middle free to stand in, with room for the roof.
func (r *robot) levelSpot(radius int) (world.BlockPos, bool) {
	p := r.player.Position()
	px, py, pz := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	solid := func(x, y, z int) bool {
		s, ok := r.world.BlockAt(world.BlockPos{X: x, Y: y, Z: z})
		return ok && !block.Replaceable(s) && len(block.CollisionShape(s)) > 0 && block.FluidOf(s) == nil
	}
	open := func(x, y, z int) bool {
		s, ok := r.world.BlockAt(world.BlockPos{X: x, Y: y, Z: z})
		return ok && block.Replaceable(s) && block.FluidOf(s) == nil
	}
	best, bestD, found := world.BlockPos{}, math.MaxFloat64, false
	for dy := -4; dy <= 4; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				x, y, z := px+dx, py+dy, pz+dz
				ok := true
				for i := -1; i <= 1 && ok; i++ {
					for j := -1; j <= 1 && ok; j++ {
						ok = solid(x+i, y-1, z+j) && open(x+i, y, z+j) && open(x+i, y+1, z+j) && open(x+i, y+2, z+j)
					}
				}
				if !ok {
					continue
				}
				if d := float64(dx*dx + dz*dz + 4*dy*dy); d < bestD {
					best, bestD, found = world.BlockPos{X: x, Y: y, Z: z}, d, true
				}
			}
		}
	}
	return best, found
}

// exposed: a side of the block at p is open to the air (no collision, no fluid).
func (r *robot) exposed(p world.BlockPos) bool {
	for _, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		s, ok := r.world.BlockAt(world.BlockPos{X: p.X + d[0], Y: p.Y + d[1], Z: p.Z + d[2]})
		if ok && len(block.CollisionShape(s)) == 0 && block.FluidOf(s) == nil {
			return true
		}
	}
	return false
}

// smeltGoal gets an item a furnace makes (charcoal from logs, iron from raw
// iron): the input and the fuel first, a furnace near — crafted from
// cobblestone and put down if none is — then the furnace's work. ok is false
// when no smelting recipe of the item can be had here.
func (r *robot) smeltGoal(item string, n, depth int) (error, bool) {
	for _, rec := range recipe.For(item) {
		if rec.Kind != "minecraft:smelting" || len(rec.Ingredients) != 1 {
			continue
		}
		input := r.knownOption(rec.Ingredients[0], depth)
		if input == "" {
			continue
		}
		need := (n - r.ui.Count(item) + rec.Count - 1) / rec.Count
		plan("%ssmelt %s from %s %d", indent(depth), item, input, need)
		// the furnace first: making it (a crafting table on the way) may use
		// up what was got for the furnace's work
		pos, err := r.blockNear("minecraft:furnace", depth)
		if err != nil {
			return err, true
		}
		var fuel string
		for round := 0; ; round++ {
			if err := r.get(input, need, depth+1); err != nil {
				return err, true
			}
			if fuel, err = r.getFuel(need, input, depth); err != nil {
				return err, true
			}
			if r.ui.Count(input) >= need {
				break
			}
			if round == 3 {
				return fmt.Errorf("smelt %s: the input keeps running short", item), true
			}
		}
		if err := r.reach(pos); err != nil {
			return err, true
		}
		if _, err := cmdOpen(r, []string{strconv.Itoa(pos.X), strconv.Itoa(pos.Y), strconv.Itoa(pos.Z)}); err != nil {
			return fmt.Errorf("open the furnace: %w", err), true
		}
		_, err = r.ui.Smelt(r.ctx, input, fuel, need)
		if m, ok := r.screens.Open(); ok {
			_ = r.screens.Close(m.ID)
		}
		_ = r.ctl.WaitTicks(r.ctx, 4)
		if err != nil {
			return fmt.Errorf("smelt %s: %w", item, err), true
		}
		return nil, true
	}
	return nil, false
}

// fuels are what the robot burns, best first, with how many items each smelts
// (AbstractFurnaceBlockEntity's burn times over the 200 ticks of a smelt).
var fuels = []struct {
	item  string
	smelt float64
}{
	{"minecraft:coal", 8}, {"minecraft:charcoal", 8}, {"minecraft:coal_block", 80},
	{"minecraft:oak_planks", 1.5}, {"minecraft:spruce_planks", 1.5}, {"minecraft:birch_planks", 1.5},
	{"minecraft:jungle_planks", 1.5}, {"minecraft:acacia_planks", 1.5}, {"minecraft:dark_oak_planks", 1.5},
	{"minecraft:cherry_planks", 1.5}, {"minecraft:mangrove_planks", 1.5}, {"minecraft:pale_oak_planks", 1.5},
}

// getFuel has enough of one fuel to smelt n items — what it has, else planks
// (the input itself is not burnt) — and returns which.
func (r *robot) getFuel(n int, input string, depth int) (string, error) {
	for _, f := range fuels {
		if f.item != input && float64(r.ui.Count(f.item))*f.smelt >= float64(n) {
			return f.item, nil
		}
	}
	need := int(math.Ceil(float64(n) / 1.5))
	for _, f := range fuels[3:] {
		if r.knownObtainable(f.item, depth+1) {
			if err := r.get(f.item, need, depth+1); err != nil {
				return "", err
			}
			return f.item, nil
		}
	}
	return "", fmt.Errorf("no fuel for %d", n)
}

// blockNear finds a block of the kind near (a furnace), or gets one and puts
// it down beside the robot.
func (r *robot) blockNear(kind string, depth int) (world.BlockPos, error) {
	if pos, ok, err := r.homeBlock(kind, depth); ok || err != nil {
		return pos, err
	}
	if pos, ok := r.nearest([]string{kind}, nil); ok && r.distanceTo(pos) < 12 {
		return pos, nil
	}
	if err := r.get(kind, 1, depth+1); err != nil {
		return world.BlockPos{}, err
	}
	if _, err := r.toHotbar(kind, true); err != nil {
		return world.BlockPos{}, err
	}
	pos, err := r.placeNextTo()
	if err != nil {
		return world.BlockPos{}, err
	}
	plan("%splaced %s at %v", indent(depth), kind, pos)
	return pos, nil
}

// cmdLight lights the robot's place, as a person does who stops for the night:
// torches got first (crafted from coal or charcoal and a stick), then one on
// the wall beside its head where there is a wall — inside a hut — or else
// on the ground around it. It answers where they went.
func cmdLight(r *robot, args []string) (string, error) {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	head := world.BlockPos{X: x, Y: y + 1, Z: z}
	type spot struct{ at, against world.BlockPos }
	var spots []spot
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		wall := world.BlockPos{X: x + d[0], Y: y + 1, Z: z + d[1]}
		if s, ok := r.world.BlockAt(wall); ok && block.SturdyFaces(s) != 0 {
			spots = append(spots, spot{head, wall})
			break
		}
	}
	if len(spots) == 0 {
		for _, d := range [4][2]int{{2, 0}, {-2, 0}, {0, 2}, {0, -2}} {
			if g, ok := r.groundAt(x+d[0], z+d[1], y); ok && math.Abs(float64(g.Y-y)) <= 2 {
				spots = append(spots, spot{g, world.BlockPos{X: g.X, Y: g.Y - 1, Z: g.Z}})
			}
		}
	}
	if len(spots) == 0 {
		return "", errors.New("nowhere to put a torch")
	}
	if err := r.get("minecraft:torch", len(spots), 0); err != nil {
		return "", err
	}
	var placed []string
	for _, s := range spots {
		if _, err := r.toHotbar("minecraft:torch", true); err != nil {
			return "", err
		}
		u, err := waitUse(r, func(done func(act.Use)) { r.hands.PlaceOn(s.at, s.against, done) })
		if err != nil {
			plan("light: %v: %v", s.at, err)
			continue
		}
		placed = append(placed, fmt.Sprintf("%d %d %d %s", s.at.X, s.at.Y, s.at.Z, world.StateString(u.State)))
	}
	if len(placed) == 0 {
		return "", errors.New("no torch would go")
	}
	return strings.Join(placed, "; "), nil
}

// craftByHand crafts a recipe of the game the book does not show, laying the
// ingredients the robot has into the grid itself: the inventory's two by two,
// or a crafting table for a bigger one.
func (r *robot) craftByHand(item string, rec recipe.Recipe, crafts, depth int) error {
	width := rec.Width
	cells := make([]string, len(rec.Ingredients))
	for i, slot := range rec.Ingredients {
		if slot == nil {
			continue
		}
		o := r.knownOption(slot, depth)
		if o == "" || r.ui.Count(o) == 0 {
			return fmt.Errorf("craft %s by hand: no %v", item, slot)
		}
		cells[i] = o
	}
	if width == 0 { // shapeless: the cells in order
		width = 2
		if len(cells) > 4 {
			width = 3
		}
	}
	height := (len(cells) + width - 1) / width
	opened := false
	if width > 2 || height > 2 {
		pos, err := r.tableNear(depth)
		if err != nil {
			return err
		}
		if err := r.reach(pos); err != nil {
			return err
		}
		if _, err := cmdOpen(r, []string{strconv.Itoa(pos.X), strconv.Itoa(pos.Y), strconv.Itoa(pos.Z)}); err != nil {
			return fmt.Errorf("open the crafting table: %w", err)
		}
		opened = true
	}
	plan("%scraft %s by hand %dx", indent(depth), item, crafts)
	made, err := r.ui.CraftByHand(r.ctx, cells, width, crafts)
	if opened {
		if m, ok := r.screens.Open(); ok {
			_ = r.screens.Close(m.ID)
		}
		_ = r.ctl.WaitTicks(r.ctx, 4)
	}
	if err != nil {
		return fmt.Errorf("craft %s by hand: made %d: %w", item, made, err)
	}
	return nil
}

// fillBucket gets a water bucket as a person does: a bucket (three iron
// ingots), filled at the nearest still water in sight.
func (r *robot) fillBucket(depth int) error {
	if err := r.get("minecraft:bucket", 1, depth+1); err != nil {
		return err
	}
	src, ok := r.nearestWhere([]string{"minecraft:water"}, nil, func(p world.BlockPos) bool {
		s, ok := r.world.BlockAt(p)
		f := block.FluidOf(s)
		return ok && f != nil && f.Source && r.openAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) && !r.keptOut(p) && len(r.beside(p)) > 0
	})
	if !ok && r.goToKnown("water", areaHas("minecraft:water_bucket")) {
		src, ok = r.nearestWhere([]string{"minecraft:water"}, nil, func(p world.BlockPos) bool {
			s, ok := r.world.BlockAt(p)
			f := block.FluidOf(s)
			return ok && f != nil && f.Source && r.openAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) && !r.keptOut(p) && len(r.beside(p)) > 0
		})
	}
	// none in sight: to the water it found (cmdWater; else what it saw
	// under ground), and look again there
	known := r.water
	if known == nil {
		known = r.bucketWater
	}
	if !ok && known != nil && !r.keptOut(*known) {
		plan("%sto the water it found at %v", indent(depth), *known)
		if err := r.reach(*known); err != nil {
			plan("%sthe water at %v: %v", indent(depth), *known, err)
		}
		src, ok = r.nearestWhere([]string{"minecraft:water"}, nil, func(p world.BlockPos) bool {
			s, ok := r.world.BlockAt(p)
			f := block.FluidOf(s)
			return ok && f != nil && f.Source && r.openAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) && !r.keptOut(p) && len(r.beside(p)) > 0
		})
	}
	if !ok {
		return fmt.Errorf("%w: no still water in sight to fill a bucket at", errNoWay)
	}
	if err := r.standBeside(src); err != nil { // aimed down into it, from its edge
		return err
	}
	if _, err := r.toHotbar("minecraft:bucket", true); err != nil {
		return err
	}
	plan("%sfill a bucket at %v", indent(depth), src)
	if _, err := waitUse(r, func(done func(act.Use)) {
		r.hands.UseItemToward(float64(src.X)+0.5, float64(src.Y)+0.8, float64(src.Z)+0.5, done)
	}); err != nil {
		return err
	}
	for t := 0; t < 20 && r.ui.Count("minecraft:water_bucket") == 0; t++ {
		if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
			return err
		}
	}
	if r.ui.Count("minecraft:water_bucket") == 0 {
		return fmt.Errorf("the bucket did not fill at %v", src)
	}
	return nil
}
