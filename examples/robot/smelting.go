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
	"github.com/mj41/go-mc26/data/recipe"
	"github.com/mj41/go-mc26/level/block"
)

// maxFurnaces is how many furnaces it works at once, at a place (home, a
// mine's workshop): a row of nine, eight things each — a night's ore in a
// few minutes, not an hour at one.
const maxFurnaces = 9

// smeltables are the items it carries that smelt into ingots (the game's
// smelting recipes: raw iron, raw gold, raw copper), each with its ingot.
func (r *robot) smeltables() map[string]string {
	out := map[string]string{}
	for _, rec := range recipe.All() {
		if rec.Kind != "minecraft:smelting" || len(rec.Ingredients) != 1 || !strings.HasSuffix(rec.Result, "_ingot") {
			continue
		}
		for _, in := range rec.Ingredients[0] {
			if r.ui.Count(in) > 0 {
				out[in] = rec.Result
			}
		}
	}
	return out
}

// cmdSmeltAll smelts what it carries that makes ingots, as a person does at
// the end of a night's mining or home: in as many furnaces as it takes (one
// for eight, nine at most) — those round it, and more made and set into the
// rock walls round it where there are not enough — eight in each with their
// fuel, then the ingots taken out, round after round. It answers what it made.
func cmdSmeltAll(r *robot, _ []string) (string, error) { return r.smeltAll(false) }

// smeltAll is smeltall; tried is set once it went home for a furnace.
func (r *robot) smeltAll(tried bool) (string, error) {
	inputs := r.smeltables()
	total := 0
	for in := range inputs {
		total += r.ui.Count(in)
	}
	if total == 0 {
		return "nothing to smelt", nil
	}
	want := min(maxFurnaces, (total+7)/8)
	furnaces := r.furnacesNear(6)
	for len(furnaces) < want {
		pos, err := r.furnaceInWall()
		if err != nil {
			plan("smelt: another furnace: %v", err)
			break
		}
		furnaces = append(furnaces, pos)
	}
	if len(furnaces) == 0 {
		// nothing to make one of here (down its stairs at morning, no wood
		// underground): home first, where the trees are, and again there
		if !tried && r.home != nil && r.distanceTo(r.home.centre()) > 8 {
			plan("smelt: no furnace here and none to make: home first")
			_, err := cmdIn(r, nil)
			if err == nil {
				return r.smeltAll(true)
			}
			plan("smelt: home: %v", err)
		}
		return "", errors.New("no furnace near, none could be made")
	}
	plan("smelt: %d to smelt in %d furnaces", total, len(furnaces))
	before := map[string]int{}
	for _, out := range inputs {
		before[out] = r.ui.Count(out)
	}
	r.waiting.Store(true) // standing at its furnaces is not being stuck
	defer r.waiting.Store(false)
	for round := 0; round < 10; round++ {
		loaded, longest := []world.BlockPos{}, 0
		for _, f := range furnaces {
			in := ""
			for i := range inputs {
				if r.ui.Count(i) > 0 {
					in = i
					break
				}
			}
			if in == "" {
				break
			}
			k := min(8, r.ui.Count(in))
			fuel, err := r.getFuel(k, in, 1)
			if err != nil {
				plan("smelt: %v", err)
				break
			}
			fuelN := int(math.Ceil(float64(k) / fuelValue(fuel)))
			if err := r.reach(f); err != nil {
				plan("smelt: to the furnace at %v: %v", f, err)
				continue
			}
			if _, err := cmdOpen(r, []string{strconv.Itoa(f.X), strconv.Itoa(f.Y), strconv.Itoa(f.Z)}); err != nil {
				plan("smelt: open %v: %v", f, err)
				continue
			}
			err = r.ui.Load(r.ctx, in, k, fuel, fuelN)
			r.closeScreen()
			if err != nil {
				plan("smelt: load %v: %v", f, err)
				continue
			}
			loaded = append(loaded, f)
			longest = max(longest, k)
		}
		if len(loaded) == 0 {
			break
		}
		// ten seconds an item; the furnaces work side by side
		if err := r.ctl.WaitTicks(r.ctx, 200*longest+40); err != nil {
			return "", err
		}
		for _, f := range loaded {
			if err := r.reach(f); err != nil {
				continue
			}
			if _, err := cmdOpen(r, []string{strconv.Itoa(f.X), strconv.Itoa(f.Y), strconv.Itoa(f.Z)}); err != nil {
				continue
			}
			if _, err := r.ui.TakeOutput(r.ctx); err != nil {
				plan("smelt: take from %v: %v", f, err)
			}
			r.closeScreen()
		}
	}
	var made []string
	for _, out := range inputs {
		if n := r.ui.Count(out) - before[out]; n > 0 {
			made = append(made, fmt.Sprintf("%s=%d", strings.TrimPrefix(out, "minecraft:"), n))
			before[out] += n // one output for several inputs (raw iron and an ore): once
		}
	}
	sort.Strings(made)
	r.upgrade() // the ingots: a better tool, if one is to be had
	return fmt.Sprintf("made %s in %d furnaces", strings.Join(made, " "), len(furnaces)), nil
}

// fuelValue is how many things one of fuel smelts.
func fuelValue(fuel string) float64 {
	for _, f := range fuels {
		if f.item == fuel {
			return f.smelt
		}
	}
	return 1
}

// closeScreen closes the open menu, if one is.
func (r *robot) closeScreen() {
	if m, ok := r.screens.Open(); ok {
		_ = r.screens.Close(m.ID)
	}
	_ = r.ctl.WaitTicks(r.ctx, 4)
}

// furnacesNear are the furnaces within radius of it.
func (r *robot) furnacesNear(radius int) []world.BlockPos {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	var out []world.BlockPos
	for dy := -2; dy <= 2; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				pos := world.BlockPos{X: x + dx, Y: y + dy, Z: z + dz}
				if s, ok := r.world.BlockAt(pos); ok && int(s) < len(block.StateList) && block.StateList[s].ID() == "minecraft:furnace" {
					out = append(out, pos)
				}
			}
		}
	}
	return out
}

// furnaceInWall makes a furnace and sets it into the rock wall near it, at
// its feet's or head's height, beside a place it can stand — out of the way:
// a room's floor stays free, its stairs clear.
func (r *robot) furnaceInWall() (world.BlockPos, error) {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	type spot struct {
		pos world.BlockPos
		d   float64
	}
	var spots []spot
	for dy := 0; dy <= 1; dy++ {
		for dx := -3; dx <= 3; dx++ {
			for dz := -3; dz <= 3; dz++ {
				pos := world.BlockPos{X: x + dx, Y: y + dy, Z: z + dz}
				if !r.isRock(pos) || r.onTheWay(pos) || r.ownColumn(pos) {
					continue
				}
				open := false // a wall: open room beside it, at its feet's level
				for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					if r.standable(world.BlockPos{X: pos.X + d[0], Y: y, Z: pos.Z + d[1]}) {
						open = true
					}
				}
				if open {
					spots = append(spots, spot{pos, math.Hypot(float64(dx), float64(dz)) + float64(dy)*0.1})
				}
			}
		}
	}
	sort.Slice(spots, func(i, j int) bool { return spots[i].d < spots[j].d })
	for _, s := range spots {
		if err := r.get("minecraft:furnace", r.ui.Count("minecraft:furnace")+1, 1); err != nil {
			return world.BlockPos{}, err
		}
		if err := r.reach(s.pos); err != nil {
			continue
		}
		if err := r.clear(s.pos); err != nil {
			continue
		}
		if _, err := r.toHotbar("minecraft:furnace", true); err != nil {
			return world.BlockPos{}, err
		}
		if _, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(s.pos, done) }); err != nil {
			plan("smelt: furnace into %v: %v", s.pos, err)
			continue
		}
		plan("smelt: a furnace set into the wall at %v", s.pos)
		return s.pos, nil
	}
	return world.BlockPos{}, errors.New("no rock wall near to set a furnace into")
}
