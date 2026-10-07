package main

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// A shelter is a room three by three and two high, where a person spends the
// night: room for a crafting table, a furnace and the top of a staircase down.
// Best dug into a hill — its walls and roof are the hill's — else built.

// room is a shelter's plan: the place in front of it, the way in, and the
// cells of the room, from the entrance on.
type room struct {
	front world.BlockPos // where the robot stands to dig in, facing d
	d     [2]int
	y     int
}

// cell is the shelter's block at along (1 the way in, 2–4 the room, 5–7 the
// tunnel on into the hill), side (positive to the right, going in) and height
// dy (0 feet, 1 head).
func (m room) cell(along, side, dy int) world.BlockPos {
	return world.BlockPos{
		X: m.front.X + m.d[0]*along - m.d[1]*side,
		Y: m.y + dy,
		Z: m.front.Z + m.d[1]*along + m.d[0]*side,
	}
}

func (m room) centre() world.BlockPos { return m.cell(3, 0, 0) }

// cmdShelter makes a shelter for the night: a room dug into a hill if there is
// one near, lit by torches on its walls, its entrance shut behind the robot
// with cobblestone so nothing follows it in; or, on open ground, a hut of
// cobblestone built around it, three by three inside. It answers where the
// room is and how it was made.
func cmdShelter(r *robot, _ []string) (string, error) {
	r.downToWarmLand()
	// a hill near; else one a little way off, as a person walks to one
	hills := r.hillsides(16, 6)
	if len(hills) == 0 {
		hills = r.hillsides(32, 6)
	}
	if len(hills) > 0 {
		// the nearest it can get to: a place in a hill may be a pocket
		// under the slope it stands on, with no way in from here
		for _, m := range hills {
			plan("shelter: into the hill at %v, going %v", m.front, m.d)
			err := r.digRoom(m)
			if err == nil {
				r.home = &m
				c := m.centre()
				return fmt.Sprintf("dug at %d %d %d facing %d %d", c.X, c.Y, c.Z, m.d[0], m.d[1]), nil
			}
			if !errors.Is(err, errCannotGetThere) {
				return "", err
			}
			plan("shelter: %v; the next hill", err)
		}
		plan("shelter: no hill it can get to: a hut")
	} else {
		plan("shelter: no hill near: a hut")
	}
	c, err := r.buildHut5()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("built at %d %d %d facing 1 0", c.X, c.Y, c.Z), nil
}

// downToWarmLand, the first time (no home yet) and standing in the cold — a
// snowy mountain, its slopes — walks down to the nearest green land below the
// snow before the shelter is made, as a person settles in the valley: up here
// water freezes, snow falls, and the field by home would have no water.
func (r *robot) downToWarmLand() {
	if r.home != nil {
		return
	}
	p := r.player.Position()
	here := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	if !r.coldAt(here) {
		return
	}
	r.learnRound(160, 4, 32, 96) // the look down from up here
	w, ok := r.warmLand(here, here.Y)
	if !ok {
		plan("shelter: in the cold at %v, no warm land in sight: here", here)
		return
	}
	plan("shelter: in the cold at %v: down to the warm land at %v first", here, w)
	if g, ok := r.groundAt(w.X, w.Z, w.Y); ok {
		w = g
	}
	if err := r.goTo(w); err != nil {
		plan("shelter: to the warm land: %v", err)
	}
}

// errCannotGetThere: no way to walk to a place.
var errCannotGetThere = errors.New("cannot get there")

// hillsides finds, within radius, up to max places to stand in front of a
// hill that a room fits into whole — every block of the room and the way in
// solid ground, a shell of solid ground all round it (floor, walls, roof), no
// fluid — the nearest first, three blocks apart at least.
func (r *robot) hillsides(radius, max int) []room {
	p := r.player.Position()
	px, py, pz := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	solid := func(pos world.BlockPos) bool {
		s, ok := r.world.BlockAt(pos)
		return ok && len(block.CollisionShape(s)) > 0 && block.FluidOf(s) == nil && !block.Replaceable(s) &&
			block.StateList[s].ID() != "minecraft:bedrock"
	}
	type cand struct {
		m    room
		dist float64
	}
	var all []cand
	for dy := -6; dy <= 6; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				front := world.BlockPos{X: px + dx, Y: py + dy, Z: pz + dz}
				if !r.standable(front) {
					continue
				}
				for _, d := range [4][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} {
					m := room{front: front, d: d, y: front.Y}
					ok := true
					// the room, the way in and the shell round them (floor,
					// walls, roof: along 1–5, side -2..2, height -1..2, the
					// corners aside) all solid ground: inside the hill, not a cave
					for along := 1; along <= 5 && ok; along++ {
						for side := -2; side <= 2 && ok; side++ {
							if (along == 1 || along == 5) && (side == -2 || side == 2) {
								continue
							}
							for h := -1; h <= 2 && ok; h++ {
								ok = solid(m.cell(along, side, h))
							}
						}
					}
					// the tunnel on into the hill and the rock round it
					for along := 6; along <= 8 && ok; along++ {
						for side := -1; side <= 1 && ok; side++ {
							for h := -1; h <= 2 && ok; h++ {
								ok = solid(m.cell(along, side, h))
							}
						}
					}
					if !ok {
						continue
					}
					all = append(all, cand{m, float64(dx*dx + dz*dz + 4*dy*dy)})
				}
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].dist < all[j].dist })
	var out []room
	for _, c := range all {
		near := false
		for _, o := range out {
			if abs(o.front.X-c.m.front.X)+abs(o.front.Y-c.m.front.Y)+abs(o.front.Z-c.m.front.Z) < 3 {
				near = true
				break
			}
		}
		if !near {
			out = append(out, c.m)
		}
		if len(out) == max {
			break
		}
	}
	return out
}

// digRoom goes to the room's front, digs the way in and the room, lights it
// and shuts the way in behind it.
func (r *robot) digRoom(m room) error {
	if _, err := cmdGoto(r, []string{strconv.Itoa(m.front.X), strconv.Itoa(m.front.Y), strconv.Itoa(m.front.Z)}); err != nil {
		return fmt.Errorf("to the hill: %w: %w", errCannotGetThere, err)
	}
	// the way in, then the room, nearest first, the robot stepping in
	order := [][3]int{{1, 0, 0}, {1, 0, 1}, {2, 0, 0}, {2, 0, 1}}
	for _, o := range order {
		if err := r.clear(m.cell(o[0], o[1], o[2])); err != nil {
			return err
		}
	}
	in := m.cell(2, 0, 0)
	if _, err := cmdGoto(r, []string{strconv.Itoa(in.X), strconv.Itoa(in.Y), strconv.Itoa(in.Z)}); err != nil {
		return fmt.Errorf("into the hill: %w", err)
	}
	for along := 2; along <= 4; along++ {
		for _, side := range []int{0, -1, 1} {
			for dy := 0; dy <= 1; dy++ {
				if err := r.clear(m.cell(along, side, dy)); err != nil {
					return err
				}
			}
		}
		if along == 3 {
			c := m.centre()
			if _, err := cmdGoto(r, []string{strconv.Itoa(c.X), strconv.Itoa(c.Y), strconv.Itoa(c.Z)}); err != nil {
				return fmt.Errorf("into the room: %w", err)
			}
		}
	}
	// the tunnel on into the hill, three blocks, where the stairs down start
	// (dug now: the room and the tunnel are the stone the furnace is made of)
	for along := 5; along <= 7; along++ {
		for dy := 0; dy <= 1; dy++ {
			if err := r.clear(m.cell(along, 0, dy)); err != nil {
				return err
			}
		}
	}
	if _, err := cmdCollect(r, []string{"4"}); err != nil {
		plan("shelter: collect: %v", err)
	}
	c := m.centre()
	if _, err := cmdGoto(r, []string{strconv.Itoa(c.X), strconv.Itoa(c.Y), strconv.Itoa(c.Z)}); err != nil {
		return fmt.Errorf("to the room's middle: %w", err)
	}
	// what furnishes it, made in it of what it carries and what it dug
	// (the table last: making the others may put one down)
	for _, f := range []string{"minecraft:furnace", "minecraft:chest", "minecraft:crafting_table"} {
		if r.ui.Count(f) > 0 {
			continue
		}
		if err := r.get(f, 1, 0); err != nil {
			plan("shelter: %s: %v", f, err)
		}
	}
	if _, err := cmdGoto(r, []string{strconv.Itoa(c.X), strconv.Itoa(c.Y), strconv.Itoa(c.Z)}); err != nil {
		return fmt.Errorf("to the room's middle: %w", err)
	}
	r.lightRoom(m)
	// the way in shut: nothing follows it in
	for _, dy := range []int{0, 1} {
		if err := r.fill(m.cell(1, 0, dy)); err != nil {
			return fmt.Errorf("shut the way in: %w", err)
		}
	}
	// along the right wall, going in: the crafting table, the chest, the furnace
	r.furnish(m.cell(2, 1, 0), m.cell(4, 1, 0), m.cell(3, 1, 0))
	r.stash()
	end := m.cell(7, 0, 0)
	if _, err := cmdGoto(r, []string{strconv.Itoa(end.X), strconv.Itoa(end.Y), strconv.Itoa(end.Z)}); err != nil {
		return fmt.Errorf("to the tunnel's end: %w", err)
	}
	r.face(m.d)
	return nil
}

// face turns the robot to look the way d, level.
func (r *robot) face(d [2]int) {
	r.ctl.Look(float32(-math.Atan2(float64(d[0]), float64(d[1]))*180/math.Pi), 0)
}

// junk is what the robot keeps in its chest, not on it: the stones and earth
// its digging gives besides cobblestone (kept, to bridge and shut doors with),
// and what falls off trees and plants.
var junk = []string{
	"minecraft:dirt", "minecraft:granite", "minecraft:diorite", "minecraft:andesite", "minecraft:tuff",
	"minecraft:gravel", "minecraft:sand", "minecraft:flint", "minecraft:cobbled_deepslate",
	"minecraft:wheat_seeds",
}

// clearWalkway takes up whatever stands on the shelter's walkway (the
// room's middle row and the tunnel, the door aside), reached from where the
// robot is.
func (r *robot) clearWalkway(m room) {
	for along := 2; along <= 7; along++ {
		for dy := 0; dy <= 1; dy++ {
			c := m.cell(along, 0, dy)
			if r.openAt(c) {
				continue
			}
			if r.distanceTo(c) > 4.5 {
				if err := r.reach(c); err != nil {
					continue
				}
			}
			plan("in: %v is in the way: taken up", c)
			if err := r.clear(c); err != nil {
				plan("in: %v: %v", c, err)
			}
		}
	}
	if _, err := cmdCollect(r, []string{"4"}); err != nil {
		plan("in: collect: %v", err)
	}
}

// lightRoom puts the shelter's torches up on its left wall, at head height
// (those up already left): dug before it had torches, it is lit coming in.
func (r *robot) lightRoom(m room) {
	r.torchOn(m.cell(2, -1, 1), m.cell(2, -2, 1))
	r.torchOn(m.cell(4, -1, 1), m.cell(4, -2, 1))
}

// tidy keeps room in the pack for what it digs, as a miner does: with fewer
// than six slots free, the rubble — and cobblestone past three stacks — into
// a chest near (the mine's workshop's, or one put down where it is), then
// back where it stood.
func (r *robot) tidy() {
	if r.freeSlots() >= 6 {
		return
	}
	p := r.player.Position()
	at := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	plan("tidy: %d slots free", r.freeSlots())
	pos, err := r.blockNear("minecraft:chest", 0)
	if err != nil {
		plan("tidy: %v", err)
		return
	}
	if err := r.reach(pos); err != nil {
		plan("tidy: to the chest: %v", err)
		return
	}
	var extra []string
	if r.ui.Count("minecraft:cobblestone") > 192 {
		extra = append(extra, "minecraft:cobblestone")
	}
	if _, err := r.stash(extra...); err != nil {
		plan("tidy: %v", err)
	}
	if err := r.goToLevel(at); err != nil {
		plan("tidy: back to %v: %v", at, err)
	}
}

// freeSlots counts the empty slots of its pack (not the armour, the offhand).
func (r *robot) freeSlots() int {
	r.screens.Lock()
	defer r.screens.Unlock()
	n := 0
	for _, s := range r.screens.Inventory.Slots[9:45] {
		if s.Count <= 0 {
			n++
		}
	}
	return n
}

// stash puts what the robot does not need into the chest near it, and the
// extra kinds too.
func (r *robot) stash(extra ...string) (int, error) {
	pos, ok := r.nearest([]string{"minecraft:chest"}, nil)
	if !ok || r.distanceTo(pos) > 4 {
		return 0, errors.New("no chest near")
	}
	kinds := append(append(append([]string(nil), junk...), tagItems("minecraft:saplings")...), extra...)
	have := false
	for _, j := range kinds {
		if r.ui.Count(j) > 0 {
			have = true
		}
	}
	if !have {
		return 0, nil
	}
	if _, err := cmdOpen(r, []string{strconv.Itoa(pos.X), strconv.Itoa(pos.Y), strconv.Itoa(pos.Z)}); err != nil {
		return 0, fmt.Errorf("open the chest: %w", err)
	}
	moved := 0
	for _, j := range kinds {
		if r.ui.Count(j) == 0 {
			continue
		}
		n, err := r.ui.Move(r.ctx, j, true)
		if err != nil {
			plan("stash %s: %v", j, err)
		}
		moved += n
	}
	if m, ok := r.screens.Open(); ok {
		_ = r.screens.Close(m.ID)
	}
	_ = r.ctl.WaitTicks(r.ctx, 4)
	plan("stashed %d", moved)
	return moved, nil
}

// the room's furniture, by kind
const kindTable, kindFurnace, kindChest = "minecraft:crafting_table", "minecraft:furnace", "minecraft:chest"

// furnish puts the crafting table, the furnace and the chest the robot
// carries at their places in the shelter (one of a kind put down elsewhere
// in the room taken up and put there).
func (r *robot) furnish(table, furnace, chest world.BlockPos) {
	places := []struct {
		kind string
		at   world.BlockPos
	}{{"minecraft:crafting_table", table}, {"minecraft:furnace", furnace}, {"minecraft:chest", chest}}
	idAt := func(p world.BlockPos) string {
		if st, ok := r.world.BlockAt(p); ok && int(st) < len(block.StateList) {
			return block.StateList[st].ID()
		}
		return ""
	}
	// a place taken by another piece (a table put down there to make the
	// chest): that one taken up first
	for _, f := range places {
		if id := idAt(f.at); id != f.kind && (id == kindTable || id == kindFurnace || id == kindChest) {
			if _, err := cmdDig(r, []string{strconv.Itoa(f.at.X), strconv.Itoa(f.at.Y), strconv.Itoa(f.at.Z)}); err == nil {
				_, _ = cmdCollect(r, []string{"4"})
			}
		}
	}
	for _, f := range places {
		if idAt(f.at) == f.kind {
			continue // at its place already
		}
		// one put down elsewhere in the room (a table set beside it to make
		// the furnace): taken up, to go to its place
		if pos, ok := r.nearest([]string{f.kind}, nil); ok && r.distanceTo(pos) < 3 && r.ui.Count(f.kind) == 0 {
			if _, err := cmdDig(r, []string{strconv.Itoa(pos.X), strconv.Itoa(pos.Y), strconv.Itoa(pos.Z)}); err == nil {
				_, _ = cmdCollect(r, []string{"4"})
			}
		}
		if r.ui.Count(f.kind) == 0 {
			continue
		}
		if _, err := r.toHotbar(f.kind, true); err != nil {
			continue
		}
		at := f.at
		if _, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(at, done) }); err != nil {
			plan("shelter: %s at %v: %v", f.kind, at, err)
		}
	}
}

// torchOn puts a torch at pos on the block against, if it has one.
func (r *robot) torchOn(pos, against world.BlockPos) {
	if s, ok := r.world.BlockAt(pos); ok && int(s) < len(block.StateList) && strings.HasSuffix(block.StateList[s].ID(), "torch") {
		return // lit already
	}
	if r.ui.Count("minecraft:torch") == 0 {
		return
	}
	if _, err := r.toHotbar("minecraft:torch", true); err != nil {
		return
	}
	if _, err := waitUse(r, func(done func(act.Use)) { r.hands.PlaceOn(pos, against, done) }); err != nil {
		plan("shelter: torch at %v: %v", pos, err)
	}
}

// buildHut5 builds a hut of cobblestone around the robot on level ground:
// walls two high round a room three by three, a roof over it, torches.
func (r *robot) buildHut5() (world.BlockPos, error) {
	if spot, ok := r.levelSpot5(12); ok {
		if _, err := cmdGoto(r, []string{strconv.Itoa(spot.X), strconv.Itoa(spot.Y), strconv.Itoa(spot.Z)}); err != nil {
			plan("hut: to level ground at %v: %v", spot, err)
		}
	}
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	var spots []world.BlockPos
	for dy := 0; dy <= 1; dy++ {
		for i := -2; i <= 2; i++ {
			for _, c := range [][2]int{{i, -2}, {i, 2}, {-2, i}, {2, i}} {
				pos := world.BlockPos{X: x + c[0], Y: y + dy, Z: z + c[1]}
				if dy == 0 || !contains(spots, pos) {
					spots = append(spots, pos)
				}
			}
		}
	}
	// the roof from the walls in, each block against one placed before
	for ring := 2; ring >= 0; ring-- {
		for i := -ring; i <= ring; i++ {
			for _, c := range [][2]int{{i, -ring}, {i, ring}, {-ring, i}, {ring, i}} {
				pos := world.BlockPos{X: x + c[0], Y: y + 2, Z: z + c[1]}
				if !contains(spots, pos) {
					spots = append(spots, pos)
				}
			}
		}
	}
	var need []world.BlockPos
	for _, s := range spots {
		if st, ok := r.world.BlockAt(s); ok && block.Replaceable(st) && !contains(need, s) {
			need = append(need, s)
		}
	}
	if err := r.get("minecraft:cobblestone", len(need), 0); err != nil {
		return world.BlockPos{}, err
	}
	// back to the hut's middle: the cobblestone may have been dug a way off
	if err := r.goToLevel(world.BlockPos{X: x, Y: y, Z: z}); err != nil {
		return world.BlockPos{}, fmt.Errorf("hut: back to %d %d %d: %w", x, y, z, err)
	}
	for _, s := range need {
		if _, err := r.toHotbar("minecraft:cobblestone", true); err != nil {
			return world.BlockPos{}, err
		}
		if _, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(s, done) }); err != nil {
			return world.BlockPos{}, fmt.Errorf("place %v: %w", s, err)
		}
	}
	// torches on the left wall, the furniture along the right (facing east)
	r.torchOn(world.BlockPos{X: x - 1, Y: y + 1, Z: z - 1}, world.BlockPos{X: x - 1, Y: y + 1, Z: z - 2})
	r.torchOn(world.BlockPos{X: x + 1, Y: y + 1, Z: z - 1}, world.BlockPos{X: x + 1, Y: y + 1, Z: z - 2})
	r.furnish(world.BlockPos{X: x - 1, Y: y, Z: z + 1}, world.BlockPos{X: x + 1, Y: y, Z: z + 1}, world.BlockPos{X: x, Y: y, Z: z + 1})
	r.stash()
	r.face([2]int{1, 0})
	return world.BlockPos{X: x, Y: y, Z: z}, nil
}

func contains(l []world.BlockPos, p world.BlockPos) bool {
	for _, q := range l {
		if q == p {
			return true
		}
	}
	return false
}

// levelSpot5 is levelSpot for a hut five by five: solid ground under all of
// it at one height, room above for its walls and roof.
func (r *robot) levelSpot5(radius int) (world.BlockPos, bool) {
	p := r.player.Position()
	px, py, pz := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	best, bestD, found := world.BlockPos{}, math.MaxFloat64, false
	for dy := -4; dy <= 4; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				x, y, z := px+dx, py+dy, pz+dz
				ok := true
				for i := -2; i <= 2 && ok; i++ {
					for j := -2; j <= 2 && ok; j++ {
						below, ok1 := r.world.BlockAt(world.BlockPos{X: x + i, Y: y - 1, Z: z + j})
						ok = ok1 && len(block.CollisionShape(below)) > 0 && block.FluidOf(below) == nil
						for h := 0; h <= 2 && ok; h++ {
							s, ok2 := r.world.BlockAt(world.BlockPos{X: x + i, Y: y + h, Z: z + j})
							ok = ok2 && block.Replaceable(s) && block.FluidOf(s) == nil
						}
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

// cmdOut leaves the shelter for the day: the way in opened (its door, or
// the cobblestone dug), a step outside, the door shut behind.
func cmdOut(r *robot, _ []string) (string, error) {
	m := r.home
	if m == nil {
		return "", errors.New("no shelter")
	}
	// too little light left for a trip: in, the day's work left (waited out
	// in the shelter till dusk, not stood at the door)
	if r.night() {
		return "staying in: night", nil
	}
	if left := r.lightLeft(); left > 0 && left < 90*time.Second {
		plan("out: only %s of light left: staying in", left.Round(time.Second))
		r.waiting.Store(true)
		for r.lightLeft() > 0 {
			if err := r.ctl.WaitTicks(r.ctx, 40); err != nil {
				break
			}
		}
		r.waiting.Store(false)
		return "staying in: dusk", nil
	}
	if err := r.goTo(m.cell(2, 0, 0)); err != nil {
		return "", fmt.Errorf("to the way in: %w", err)
	}
	way := m.cell(1, 0, 0)
	door, _ := r.doorAt(way)
	if door {
		if err := r.setDoor(way, true); err != nil {
			return "", fmt.Errorf("the door: %w", err)
		}
	} else {
		for _, dy := range []int{0, 1} {
			if err := r.clear(m.cell(1, 0, dy)); err != nil {
				return "", err
			}
		}
	}
	if err := r.goTo(m.front); err != nil {
		return "", fmt.Errorf("out: %w", err)
	}
	if door { // shut behind it: nothing walks in while it is out
		if err := r.setDoor(way, false); err != nil {
			plan("out: the door not shut: %v", err)
		}
	}
	return fmt.Sprintf("out at %d %d %d", m.front.X, m.front.Y, m.front.Z), nil
}

// cmdIn goes back into the shelter for the night: from wherever it is to the
// way in, through it, the way in shut behind, to the room's middle.
// shutIn reports whether a walk failed for having almost nowhere to go: a
// hideout, a hole (fewer than twenty places reachable).
func shutIn(err error) bool {
	var n int
	st := err.Error()
	if i := strings.Index(st, "every place reachable searched ("); i >= 0 {
		fmt.Sscanf(st[i+len("every place reachable searched ("):], "%d", &n)
	}
	return n > 0 && n < 20
}

// enclosed reports whether a walk failed in a closed place (every place
// reachable searched), not for the search's limit.
func enclosed(err error) bool {
	return strings.Contains(err.Error(), "every place reachable searched (")
}

// nearStairs reports whether a cell of its stairs is within d blocks.
func (r *robot) nearStairs(d float64) bool {
	for _, c := range r.stairs {
		if r.distanceTo(c) <= d {
			return true
		}
	}
	return false
}

func cmdIn(r *robot, _ []string) (string, error) {
	m := r.home
	if m == nil {
		return "", errors.New("no shelter")
	}
	r.eatIfHungry() // a long way up on an empty stomach heals nothing
	// from down its stairs: up them, step by step, the way it came
	if len(r.stairs) > 0 && r.player.Position().Y < float64(m.y)-3 && r.nearStairs(24) { // down its mine, not out on low ground
		if err := r.alongStairs(0); err != nil {
			plan("in: %v", err)
		}
	}
	// the walkway (the room's middle, the tunnel): what was put down on it
	// taken up — a table set there blocks the way from the stairs
	r.clearWalkway(*m)
	// to the room's middle, then the way in shut from there: from the step
	// inside it, the robot still stands half in the way, and the server
	// puts no block inside a player
	c := m.centre()
	if r.distanceTo(c) > 48 && !r.legging { // far (back from where it died): in legs, over the land
		if err := r.legsToward(c); err != nil {
			plan("in: %v", err)
		}
	}
	err := r.goTo(c)
	// a search that found no way from one place often finds one from where
	// the try left it (higher up its stairs, out of a cave): again, twice
	// back from outside (the land, where it died) to a door it shut from
	// within: to the front, the door opened, in
	if err != nil {
		if s, ok := r.world.BlockAt(m.cell(1, 0, 0)); ok && len(block.CollisionShape(s)) > 0 {
			plan("in: %v: the door is shut, in by the front", err)
			if ferr := r.goTo(m.cell(0, 0, 0)); ferr == nil {
				if door, _ := r.doorAt(m.cell(1, 0, 0)); door {
					if derr := r.setDoor(m.cell(1, 0, 0), true); derr != nil { // its own door: opened, not dug
						plan("in: the door: %v", derr)
					}
				} else {
					for _, dy := range []int{0, 1} {
						if cerr := r.clear(m.cell(1, 0, dy)); cerr != nil {
							plan("in: the door: %v", cerr)
						}
					}
				}
				err = r.goTo(c)
			} else {
				plan("in: to the front: %v", ferr)
			}
		}
	}
	// shut in at night out here (its hideout, walled before): the night
	// waited out in it, then home by the side it opens toward home
	if err != nil && r.night() && shutIn(err) && r.underSky() {
		plan("in: walled in out here: till morning")
		if ans, herr := r.wallInUntil(false, "till morning", func() bool { return !r.night() }); herr != nil {
			plan("in: %v", herr)
		} else {
			plan("in: %s", ans)
		}
		err = r.goTo(c)
	}
	// shut in by day (a hideout in rock, a pit): dug out toward home
	// shut in (a hideout in rock, a pit; in rock by night too): dug out
	// toward home, round after round while each gets nearer
	for round := 0; round < 12 && err != nil && enclosed(err) && (!r.night() || !r.underSky()); round++ {
		plan("in: shut in: digging toward home")
		before := r.distanceTo(c)
		if terr := r.tunnelToward(c, 12); terr != nil {
			plan("in: %v", terr)
		}
		err = r.goTo(c)
		if r.distanceTo(c) > before-2 {
			break // no nearer
		}
	}
	for try := 0; err != nil && try < 2; try++ {
		plan("in: %v: again from here", err)
		r.eatIfHungry()
		if len(r.stairs) > 0 && r.player.Position().Y < float64(m.y)-3 && r.nearStairs(24) { // down its mine, not out on low ground
			if e := r.alongStairs(0); e != nil {
				plan("in: %v", e)
			}
		}
		err = r.goTo(c)
	}
	if err != nil {
		return "", fmt.Errorf("home: %w", err)
	}
	// the end of the first night (its stairs dug, the morning come): a door
	// in the way in from now on
	way := m.cell(1, 0, 0)
	if door, _ := r.doorAt(way); !door && r.deep != nil && !r.night() {
		if err := r.hangDoor(*m); err != nil {
			plan("in: the door: %v", err)
		}
		if err := r.goTo(c); err != nil {
			plan("in: %v", err)
		}
	}
	if door, _ := r.doorAt(way); door {
		if err := r.setDoor(way, false); err != nil {
			plan("in: the door not shut: %v", err)
		}
	} else {
		for _, dy := range []int{0, 1} {
			// something in the way (an item, a mob at the door): once more,
			// then in all the same — the night's watch is the defend reflex
			if err := r.fill(m.cell(1, 0, dy)); err != nil {
				_ = r.ctl.WaitTicks(r.ctx, 20)
				if err := r.fill(m.cell(1, 0, dy)); err != nil {
					plan("in: the way in not shut at %v: %v", m.cell(1, 0, dy), err)
				}
			}
		}
	}
	r.lightRoom(*m)
	r.storeValuables() // banked: a death outside loses little
	return fmt.Sprintf("in at %d %d %d", c.X, c.Y, c.Z), nil
}
