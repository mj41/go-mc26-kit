package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/clock"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// phase is a time of day as the robot lived it: when it began on the
// robot's own clock, and on the game's.
type phase struct {
	name  string
	start time.Time
	tick  int64
}

// watchClock notes each change of the time of day — the first day, the first
// night — and how long the last one lasted.
func (r *robot) watchClock() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(time.Second):
		}
		t, ok := r.clock.TimeOfDay()
		if !ok {
			continue
		}
		now := clock.Phase(t)
		r.seenMu.Lock()
		if n := len(r.phases); n == 0 || r.phases[n-1].name != now {
			if n > 0 {
				last := r.phases[n-1]
				log.Printf("robot: %s now (tick %d); the %s lasted %s", now, t, last.name, time.Since(last.start).Round(time.Second))
				events.emit("phase", map[string]any{"phase": now, "tick": t, "last": last.name, "lasted_s": int(time.Since(last.start).Seconds())})
			}
			r.phases = append(r.phases, phase{name: now, start: time.Now(), tick: t})
		}
		r.seenMu.Unlock()
	}
}

// night: the monsters are out, or about to be.
func (r *robot) night() bool {
	t, ok := r.clock.TimeOfDay()
	return ok && clock.Phase(t) != "day"
}

// cmdTime tells the time of day and the phases seen so far, how long each was.
func cmdTime(r *robot, _ []string) (string, error) {
	t, ok := r.clock.TimeOfDay()
	if !ok {
		return "", fmt.Errorf("the server has not said the time")
	}
	total, _ := r.clock.DayTicks()
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	var seen []string
	for i, p := range r.phases {
		end := time.Now()
		if i+1 < len(r.phases) {
			end = r.phases[i+1].start
		}
		seen = append(seen, fmt.Sprintf("%s=%s", p.name, end.Sub(p.start).Round(time.Second)))
	}
	return fmt.Sprintf("day %d tick %d %s; seen %s", total/clock.DayLength+1, t, clock.Phase(t), strings.Join(seen, " ")), nil
}

// eatIfHungry eats, as a person does in a long job, when the food bar is down
// to 14 (healing needs 18 and more), what it has that is best.
// rest heals before going into danger, as a person does not go down the
// stairs at half health: it eats what it carries while the food is short of
// what heals (18) and waits where it is — fighting what comes — till its
// health is want, two minutes at most. Without food to eat it cannot heal and
// goes on as it is.
func (r *robot) rest(want float32) {
	if r.player.Status().Health >= want {
		return
	}
	plan("rest: health %.1f, food %d: healing to %.0f", r.player.Status().Health, r.food.Load(), want)
	deadline := time.Now().Add(2 * time.Minute)
	for r.player.Status().Health < want && time.Now().Before(deadline) {
		if r.defend() {
			continue
		}
		if r.food.Load() < 18 && !r.eat() {
			plan("rest: nothing to eat, health %.1f", r.player.Status().Health)
			return
		}
		if err := r.ctl.WaitTicks(r.ctx, 20); err != nil {
			return
		}
	}
	plan("rest: health %.1f", r.player.Status().Health)
}

func (r *robot) eatIfHungry() {
	if r.food.Load() > 14 {
		return
	}
	if r.eat() {
		return
	}
	// nothing to eat and hungry enough not to heal: food from round it, a
	// try every two minutes at most (the gathering eats, digs and eats again)
	if r.food.Load() <= 8 && time.Since(r.foodSought) > 2*time.Minute {
		r.foodSought = time.Now()
		r.findFood()
	}
}

// findFood makes or gathers something to eat from what is in sight, when it
// has nothing: bread of its wheat, a stew of a cave's mushrooms (and a bowl
// of planks), berries off a bush — no leg out to look, and never the kill of
// a friendly animal.
func (r *robot) findFood() {
	// fish first: by day, at its field's channel or water it knows
	if r.canFish() {
		if ans, err := cmdFish(r, []string{"4"}); err != nil {
			plan("food: fish: %v", err)
		} else {
			plan("food: fish: %s", ans)
			if r.eat() {
				return
			}
		}
	}
	for _, f := range []string{"minecraft:bread", "minecraft:mushroom_stew", "minecraft:sweet_berries"} {
		err := r.get(f, 1, 1)
		if err == nil {
			plan("food: %s", f)
			r.eat()
			return
		}
		plan("food: %s: %v", f, err)
	}
}

// eat eats the first food it carries, reporting whether it ate.
func (r *robot) eat() bool {
	list := foods
	if r.food.Load() <= 6 {
		// starving (no healing, the hunger taking health): a zombie's flesh
		// too — its hunger effect costs less than it gives
		list = append(append([]string(nil), foods...), "minecraft:rotten_flesh")
	}
	for _, f := range list {
		if r.ui.Count(f) == 0 {
			continue
		}
		if _, err := r.toHotbar(f, true); err != nil {
			return false
		}
		ans, err := cmdEat(r, nil)
		if err != nil {
			log.Printf("robot: eat %s: %v", f, err)
			return false
		}
		log.Printf("robot: ate %s (%s), food now %d", f, ans, r.food.Load())
		events.emit("ate", map[string]any{"food": f, "level": r.food.Load()})
		return true
	}
	return false
}

// raw are the meats the robot cooks, with what they become.
var raw = map[string]string{
	"minecraft:beef": "minecraft:cooked_beef", "minecraft:porkchop": "minecraft:cooked_porkchop",
	"minecraft:mutton": "minecraft:cooked_mutton", "minecraft:chicken": "minecraft:cooked_chicken",
	"minecraft:rabbit": "minecraft:cooked_rabbit", "minecraft:cod": "minecraft:cooked_cod",
	"minecraft:salmon": "minecraft:cooked_salmon", "minecraft:potato": "minecraft:baked_potato",
}

// cmdCook cooks the raw meat the robot carries in the furnace near it (one
// put down if none is): it answers what it has cooked.
func cmdCook(r *robot, _ []string) (string, error) {
	var out []string
	for in, cooked := range raw {
		n := r.ui.Count(in)
		if n == 0 {
			continue
		}
		if err := r.get(cooked, r.ui.Count(cooked)+n, 0); err != nil {
			return strings.Join(out, " "), fmt.Errorf("cook %s: %w", in, err)
		}
		out = append(out, fmt.Sprintf("%s=%d", strings.TrimPrefix(cooked, "minecraft:"), r.ui.Count(cooked)))
	}
	if len(out) == 0 {
		return "nothing to cook", nil
	}
	return strings.Join(out, " "), nil
}

// spot is a place and the way to face there.
type spot struct {
	at world.BlockPos
	d  [2]int
}

// remember notes where the robot stands and faces: the end of its stairs.
func (r *robot) remember() {
	p := r.player.Position()
	r.deep = &spot{at: world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}, d: r.facing()}
}

// nightShift is for work begun in the dark, underground: it reports when the
// morning has come — time to go up to the day's work (the field, the wood).
// Work begun by day it never stops.
func (r *robot) nightShift() func() bool {
	if !r.night() {
		if r.home != nil {
			// by day, with a home: a night's command again after a pause —
			// the day's work now, the mine tonight
			return func() bool { return true }
		}
		return func() bool { return false }
	}
	return func() bool { return !r.night() }
}

// upgrade makes the best tools it can from what it carries, as a person does
// as soon as they can: the raw iron smelted, an iron pickaxe (diamonds want
// one, and it digs faster) and an iron sword, a diamond pickaxe from three
// diamonds. What it cannot make now it tries again two minutes on.
func (r *robot) upgrade() {
	// back where it stood after, a stair step or a tunnel's end: making
	// things puts a table and a furnace down beside it and may move it
	p := r.player.Position()
	at := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	made := false
	defer func() {
		q := r.player.Position()
		if made && (int(math.Floor(q.X)) != at.X || int(math.Floor(q.Y+1e-6)) != at.Y || int(math.Floor(q.Z)) != at.Z) {
			if err := r.goToLevel(at); err != nil {
				plan("upgrade: back to %v: %v", at, err)
			}
		}
	}()
	has := func(item string) bool { return r.ui.Count(item) > 0 }
	iron := r.ui.Count("minecraft:iron_ingot") + r.ui.Count("minecraft:raw_iron")
	for _, t := range []struct {
		item string
		ok   bool
	}{
		{"minecraft:iron_pickaxe", !has("minecraft:iron_pickaxe") && !has("minecraft:diamond_pickaxe") && iron >= 3},
		{"minecraft:diamond_pickaxe", !has("minecraft:diamond_pickaxe") && r.ui.Count("minecraft:diamond") >= 3},
		// the field's water before a better sword: bread is what heals it
		// two: the pool's two corners from one trip to the water
		{"minecraft:bucket", r.needsWater() && r.ui.Count("minecraft:bucket")+r.ui.Count("minecraft:water_bucket") < 2 &&
			(has("minecraft:iron_pickaxe") || has("minecraft:diamond_pickaxe")) && iron >= 3},
		{"minecraft:iron_sword", !has("minecraft:iron_sword") && !has("minecraft:diamond_sword") &&
			(has("minecraft:iron_pickaxe") || has("minecraft:diamond_pickaxe")) &&
			(!r.needsWater() || has("minecraft:bucket") || has("minecraft:water_bucket")) && iron >= 2},
	} {
		if !t.ok || time.Since(r.upgraded[t.item]) < 2*time.Minute {
			continue
		}
		plan("upgrade: %s", t.item)
		made = true
		// of what it carries and what is at hand: a better tool is not worth
		// a search (depth 1: get does not go exploring)
		want := 1
		if t.item == "minecraft:bucket" { // one more than it has
			want = r.ui.Count("minecraft:bucket") + 1
		}
		if err := r.get(t.item, want, 1); err != nil {
			plan("upgrade: %s: %v", t.item, err)
			if r.upgraded == nil {
				r.upgraded = map[string]time.Time{}
			}
			r.upgraded[t.item] = time.Now()
			continue
		}
		events.emit("upgraded", map[string]any{"tool": t.item})
		iron = r.ui.Count("minecraft:iron_ingot") + r.ui.Count("minecraft:raw_iron")
	}
}

// addStair notes a step of its staircase (where its feet are on it).
func (r *robot) addStair(p world.BlockPos) {
	if n := len(r.stairs); n > 0 && r.stairs[n-1] == p {
		return
	}
	if r.onStairs == nil {
		r.onStairs = map[world.BlockPos]bool{}
	}
	r.stairs = append(r.stairs, p)
	r.onStairs[p] = true
}

// onTheWay reports whether pos is in its staircase's way — a step, or one of
// the three blocks over it a person needs to walk it up and down: nothing is
// put down there.
func (r *robot) onTheWay(pos world.BlockPos) bool {
	for dy := -1; dy <= 2; dy++ { // -1: the floor under a step (a chest there is no step)
		if r.onStairs[world.BlockPos{X: pos.X, Y: pos.Y - dy, Z: pos.Z}] {
			return true
		}
	}
	// the shelter's walkway: in at the door, through the room's middle, the
	// tunnel to the stairs
	if m := r.home; m != nil {
		for along := 1; along <= 7; along++ {
			for dy := 0; dy <= 1; dy++ {
				if m.cell(along, 0, dy) == pos {
					return true
				}
			}
		}
	}
	return false
}

// fullFloor reports whether pos is a whole block to stand on (not a chest,
// a slab, air).
func (r *robot) fullFloor(pos world.BlockPos) bool {
	s, ok := r.world.BlockAt(pos)
	if !ok {
		return false
	}
	top := 0.0
	for _, b := range block.CollisionShape(s) {
		top = math.Max(top, b.MaxY)
	}
	return top >= 1-1e-9
}

// alongStairs walks its staircase, step by step, from the step nearest it to
// step to (0: the top), clearing what has come into the way (a block put
// down, gravel fallen in) — as a person climbs back the way they came.
func (r *robot) alongStairs(to int) error {
	if len(r.stairs) == 0 {
		return errors.New("no stairs dug yet")
	}
	p := r.player.Position()
	at, best := 0, math.MaxFloat64
	for i, s := range r.stairs {
		if d := math.Pow(float64(s.X)+0.5-p.X, 2) + math.Pow(float64(s.Y)-p.Y, 2) + math.Pow(float64(s.Z)+0.5-p.Z, 2); d < best {
			at, best = i, d
		}
	}
	step := 1
	if to < at {
		step = -1
	}
	for i := at; ; i += step {
		s := r.stairs[i]
		// a step's floor a whole block again (a chest put there, a slab: no
		// step up the path can take)
		if f := (world.BlockPos{X: s.X, Y: s.Y - 1, Z: s.Z}); !r.ownColumn(f) && !r.fullFloor(f) {
			// the step may be a way off (the nearest step, from off the
			// stairs): there first
			if err := r.reach(f); err != nil {
				plan("the stairs at %v: %v", s, err)
			}
			if !r.openAt(f) {
				plan("the stairs at %v: their floor %v is no floor: dug, filled", s, f)
				if err := r.clear(f); err != nil {
					plan("the stairs: %v", err)
				}
			}
			if err := r.fill(f); err != nil {
				plan("the stairs: %v", err)
			}
		}
		for dy := 0; dy <= 2; dy++ {
			c := world.BlockPos{X: s.X, Y: s.Y + dy, Z: s.Z}
			if r.ownColumn(c) {
				continue // the step it is on: nothing dug over its head
			}
			if st, ok := r.world.BlockAt(c); ok && len(block.CollisionShape(st)) > 0 && block.FluidOf(st) == nil {
				if err := r.clear(c); err != nil {
					return fmt.Errorf("the stairs at %v: %w", s, err)
				}
			}
		}
		// a turn the stairs took corner-wise (a step diagonal to the one
		// before): the corner opened, feet and head high, so the walk goes
		// round it
		{
			// where it stands, not the step before on the list (the list
			// has jumps: a cave crossed, a turn)
			pp := r.player.Position()
			prev := world.BlockPos{X: int(math.Floor(pp.X)), Y: int(math.Floor(pp.Y + 1e-6)), Z: int(math.Floor(pp.Z))}
			if abs(s.X-prev.X) == 1 && abs(s.Z-prev.Z) == 1 && abs(s.Y-prev.Y) <= 1 {
				corner := world.BlockPos{X: s.X, Y: max(s.Y, prev.Y), Z: prev.Z}
				for dy := 0; dy <= 1; dy++ {
					c := world.BlockPos{X: corner.X, Y: corner.Y + dy, Z: corner.Z}
					if st, ok := r.world.BlockAt(c); ok && len(block.CollisionShape(st)) > 0 && block.FluidOf(st) == nil && !r.ownColumn(c) {
						if err := r.clear(c); err != nil {
							plan("the stairs' corner at %v: %v", c, err)
						}
					}
				}
				if f := (world.BlockPos{X: corner.X, Y: corner.Y - 1, Z: corner.Z}); r.openAt(f) {
					if err := r.fill(f); err != nil {
						plan("the stairs' corner floor at %v: %v", f, err)
					}
				}
			}
		}
		if err := r.goToLevel(s); err != nil {
			return fmt.Errorf("the stairs at %v: %w", s, err)
		}
		if i == to {
			return nil
		}
	}
}

// cmdDown goes back down its own stairs to where they got to, facing on.
func cmdDown(r *robot, _ []string) (string, error) {
	if why := r.unfitForMine(); why != "" {
		if r.home != nil && r.distanceTo(r.home.centre()) > 4 { // not home (down already, or out): in
			if _, err := cmdIn(r, nil); err != nil {
				plan("in: %v", err)
			}
		}
		return "staying in: " + why, nil
	}
	if r.deep == nil && len(r.stairs) > 0 {
		// where its stairs got to, from the steps it remembers (a memory
		// written before the end was noted)
		last := r.stairs[len(r.stairs)-1]
		d := [2]int{}
		if len(r.stairs) > 1 {
			prev := r.stairs[len(r.stairs)-2]
			d = [2]int{sign(last.X - prev.X), sign(last.Z - prev.Z)}
		}
		r.deep = &spot{at: last, d: d}
	}
	if r.deep == nil {
		return "", errors.New("no stairs dug yet")
	}
	// out on the land (a retreat, a hideout): home first, then down its
	// stairs — not a search to the mine over the land and through rock
	if r.home != nil && !r.nearStairs(24) && r.distanceTo(r.deep.at) > 24 {
		if _, err := cmdIn(r, nil); err != nil {
			return "", fmt.Errorf("down: home first: %w", err)
		}
	}
	// its own stairs, step by step: one search to the mine's end may find a
	// cheaper way over the land at night
	if len(r.stairs) > 0 {
		if err := r.alongStairs(len(r.stairs) - 1); err != nil {
			plan("down: %v", err)
		}
	}
	if err := r.goTo(r.deep.at); err != nil {
		return "", fmt.Errorf("down the stairs: %w", err)
	}
	r.face(r.deep.d)
	a := r.deep.at
	return fmt.Sprintf("at %d %d %d", a.X, a.Y, a.Z), nil
}

// makePickaxe makes a pickaxe of what the robot carries — stone when it has
// the cobblestone, else wood — as a person at home with their table does
// before going down; nothing fetched for it (it is night). It reports whether
// it has one now.
func (r *robot) makePickaxe() bool {
	if r.newPick {
		return false // making one already (a worn-out one in the mine)
	}
	wood := r.countAll(tagItems("minecraft:planks")) + 4*r.countAll(logItems())
	sticks := r.ui.Count("minecraft:stick")
	for _, try := range []struct {
		item string
		has  bool
	}{
		{"minecraft:stone_pickaxe", r.countAll(tagItems("minecraft:stone_tool_materials")) >= 3 && (sticks >= 2 || wood >= 2)},
		{"minecraft:wooden_pickaxe", wood >= 5 || wood >= 3 && sticks >= 2},
	} {
		if !try.has {
			continue
		}
		r.newPick = true
		err := r.get(try.item, 1, 0)
		r.newPick = false
		if err == nil {
			plan("no pickaxe: made a %s of what it carries", try.item)
			return true
		}
		plan("no pickaxe: %s: %v", try.item, err)
	}
	return false
}

// unfitForMine says why it does not go down its mine now, "" when it does:
// no pickaxe (lost with a death: the day's trees give one), or hurt with
// nothing to eat to heal on — a person stays in, alive, for the morning.
func (r *robot) unfitForMine() string {
	if r.countAll(tagItems("minecraft:pickaxes")) == 0 && !r.makePickaxe() {
		return "no pickaxe"
	}
	if r.player.Status().Health < 10 && r.countAll(foods) == 0 {
		return fmt.Sprintf("health %.1f and nothing to eat", r.player.Status().Health)
	}
	if !r.wardenAt.IsZero() && time.Since(r.wardenAt) < 15*time.Minute {
		return "a warden down there tonight"
	}
	return ""
}
