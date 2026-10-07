package main

import (
	"errors"
	"fmt"
	"github.com/mj41/go-mc26/level/block"
	"math"
	"strconv"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// A mine at an ore's level, laid out as people lay them out, where the robot
// stands on its staircase facing down it (the way d, right the side r):
//
//   - a room four by eight to the right of the stairs, two high: a workshop
//     (a crafting table, a furnace, a chest for the rubble) the stairs pass;
//   - on the room's far side a main tunnel two wide and three high, on the
//     way of the stairs and the other way;
//   - off the main tunnel, to the right, branches one wide and two high with
//     five blocks of rock between them, the first five blocks past the room's
//     corner — every ore in their walls dug;
//   - torches on the right wall going out, on the left coming back.

// mineLayout is a mine's plan around its origin, the stair step it starts at.
type mineLayout struct {
	o       world.BlockPos
	d, rv   [2]int
	main    int // the main tunnel's length past the room, each way
	branch  int // a branch's length
	spacing int // the rock between two branches
}

// cell is the block at along a (the way d), side s (to the right) and height h.
func (m mineLayout) cell(a, s, h int) world.BlockPos {
	return world.BlockPos{X: m.o.X + m.d[0]*a + m.rv[0]*s, Y: m.o.Y + h, Z: m.o.Z + m.d[1]*a + m.rv[1]*s}
}

// facing is the cardinal way the robot looks.
func (r *robot) facing() [2]int {
	dirs := [4][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	yaw := float64(r.player.Position().Yaw) * math.Pi / 180
	fx, fz := -math.Sin(yaw), math.Cos(yaw)
	best, bestDot := dirs[0], -2.0
	for _, d := range dirs {
		if dot := fx*float64(d[0]) + fz*float64(d[1]); dot > bestDot {
			best, bestDot = d, dot
		}
	}
	return best
}

func rightOf(d [2]int) [2]int { return [2]int{-d[1], d[0]} }

// cmdMine lays out a mine where the robot stands (see above): mine [main]
// [branch]. It answers what ores it got.
func cmdMine(r *robot, args []string) (string, error) {
	if why := r.unfitForMine(); why != "" {
		if r.home != nil && r.distanceTo(r.home.centre()) > 4 { // not home (down already, or out): in
			if _, err := cmdIn(r, nil); err != nil {
				plan("in: %v", err)
			}
		}
		return "staying in: " + why, nil
	}
	p := r.player.Position()
	// the way its stairs went down (crafting since has turned its head), or,
	// away from them, the way with the most rock
	here := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	d := r.facing()
	if r.deep != nil && r.deep.at == here {
		d = r.deep.d
	} else {
		best := -1
		for _, c := range [4][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} {
			if sc := r.rockOver(here.X, here.Y+12, here.Z, c); sc > best {
				d, best = c, sc
			}
		}
	}
	m := mineLayout{
		o: world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))},
		d: d, rv: rightOf(d), main: 24, branch: 16, spacing: 5,
	}
	if len(args) >= 1 {
		v, err := strconv.Atoi(args[0])
		if err != nil {
			return "", err
		}
		m.main = v
	}
	if len(args) >= 2 {
		v, err := strconv.Atoi(args[1])
		if err != nil {
			return "", err
		}
		m.branch = v
	}
	found := map[string]int{}
	add := func(got map[string]int) {
		for k, v := range got {
			found[k] += v
		}
	}
	plan("mine: at %v going %v", m.o, m.d)
	r.rest(16)
	morning := r.nightShift()
	// the room: four lanes along the stairs, to the right, back and forth
	room := func() error {
		for s := 1; s <= 4; s++ {
			start, way := 0, m.d
			if s%2 == 0 {
				start, way = 7, [2]int{-m.d[0], -m.d[1]}
			}
			if err := r.stepInto(m.cell(start, s, 0), 2); err != nil {
				return fmt.Errorf("the room, lane %d: %w", s, err)
			}
			got, err := r.tunnel(way, 7, 1, 2, 0)
			add(got)
			if err != nil {
				return fmt.Errorf("the room, lane %d: %w", s, err)
			}
		}
		return nil
	}
	// water or lava in the room's rock: back to the stairs, on down them a
	// few steps, and the room there
	for try := 0; ; try++ {
		err := room()
		if err == nil {
			break
		}
		if !errors.Is(err, errDanger) || try == 3 {
			return "", err
		}
		plan("mine: %v; on down the stairs", err)
		if err := r.goToLevel(m.o); err != nil {
			return "", fmt.Errorf("back to the stairs: %w", err)
		}
		r.face(m.d)
		if _, err := cmdDescend(r, []string{strconv.Itoa(m.o.Y - 6)}); err != nil {
			return "", fmt.Errorf("on down the stairs: %w", err)
		}
		p := r.player.Position()
		m.o = world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
		if r.deep != nil && r.deep.at == m.o {
			m.d = r.deep.d
		}
		m.rv = rightOf(m.d)
		plan("mine: at %v going %v", m.o, m.d)
	}
	r.torchOn(m.cell(0, 2, 1), m.cell(-1, 2, 1))
	r.torchOn(m.cell(7, 3, 1), m.cell(8, 3, 1))
	// the workshop: a crafting table, a furnace and a chest by the stairs' side
	for _, kind := range []string{"minecraft:crafting_table", "minecraft:furnace", "minecraft:chest"} {
		if _, err := r.blockNear(kind, 0); err != nil {
			plan("mine: %s: %v", kind, err)
		}
	}
	// the main tunnel along the room's far side, out the way of the stairs,
	// then the other way — or, the night over, the stairs and up
	r.upgrade()
	if morning() {
		plan("mine: morning: up to the day's work")
		r.fillForField() // water for the field, carried home
		return r.mineDone(m, found, " (morning)")
	}
	if err := r.goToLevel(m.cell(0, 4, 0)); err != nil {
		return "", err
	}
	if err := r.stepInto(m.cell(0, 5, 0), 3); err != nil {
		return "", fmt.Errorf("the main tunnel: %w", err)
	}
	if err := r.clearColumn(m.cell(0, 6, 0), 3); err != nil {
		return "", err
	}
	got, err := r.tunnel(m.d, 7+m.main, 2, 3, 8)
	add(got)
	if err != nil {
		plan("mine: the main tunnel out: %v", err)
	}
	if err := r.goToLevel(m.cell(0, 6, 0)); err != nil {
		return "", err
	}
	got, err = r.tunnel([2]int{-m.d[0], -m.d[1]}, m.main, 2, 3, 8)
	add(got)
	if err != nil {
		plan("mine: the main tunnel back: %v", err)
	}
	// the branches, to the right of the main tunnel
	var at []int
	for a := 7 + m.spacing; a <= 7+m.main; a += m.spacing + 1 {
		at = append(at, a)
	}
	for a := -m.spacing; a >= -m.main; a -= m.spacing + 1 {
		at = append(at, a)
	}
	for _, a := range at {
		r.upgrade()
		if morning() {
			plan("mine: morning: up to the day's work")
			r.fillForField() // water for the field, carried home
			return r.mineDone(m, found, " (morning)")
		}
		if err := r.goToLevel(m.cell(a, 6, 0)); err != nil {
			plan("mine: to the branch at %d: %v", a, err)
			continue
		}
		got, err := r.tunnel(m.rv, m.branch, 1, 2, 8)
		add(got)
		if err != nil {
			plan("mine: branch at %d: %v", a, err)
		}
	}
	return r.mineDone(m, found, "")
}

// mineDone goes back to the mine's stairs, facing down them, and tells what
// the mine gave.
func (r *robot) mineDone(m mineLayout, found map[string]int, note string) (string, error) {
	if err := r.goToLevel(m.o); err != nil {
		return "", fmt.Errorf("back to the stairs: %w", err)
	}
	r.face(m.d)
	r.remember()
	r.upgrade()
	var parts []string
	for k, v := range found {
		parts = append(parts, fmt.Sprintf("%s=%d", strings.TrimPrefix(k, "minecraft:"), v))
	}
	return "found " + strings.Join(parts, " ") + note, nil
}

func (r *robot) goTo(p world.BlockPos) error {
	_, err := cmdGoto(r, []string{strconv.Itoa(p.X), strconv.Itoa(p.Y), strconv.Itoa(p.Z)})
	return err
}

// goToLevel is goTo in a mine: a robot below the place's level — fallen into
// a cave under the tunnel — pillars up to it first, as a person climbs out.
func (r *robot) goToLevel(p world.BlockPos) error {
	err := r.goTo(p)
	if err == nil {
		return nil
	}
	y := int(math.Floor(r.player.Position().Y + 1e-6))
	if y >= p.Y {
		return err
	}
	plan("mine: below the level (%d, the mine %d): stairs up to it", y, p.Y)
	if cerr := r.climbOut(p); cerr != nil {
		return fmt.Errorf("%v; stairs up: %w", err, cerr)
	}
	return r.goTo(p)
}

// clearColumn clears the blocks from p up, height of them, and gives it a floor.
func (r *robot) clearColumn(p world.BlockPos, height int) error {
	for h := 0; h < height; h++ {
		if c := (world.BlockPos{X: p.X, Y: p.Y + h, Z: p.Z}); r.floorAhead(c) {
			continue // the floor ahead (farmland), not in the way
		}
		if err := r.clear(world.BlockPos{X: p.X, Y: p.Y + h, Z: p.Z}); err != nil {
			return err
		}
	}
	if floor := (world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}); r.openAt(floor) {
		return r.fill(floor)
	}
	return nil
}

// stepInto clears the column at p, height high, and steps into it.
func (r *robot) stepInto(p world.BlockPos, height int) error {
	if why := r.levelDanger(p.X, p.Y, p.Z); why != "" {
		return fmt.Errorf("%s at %v", why, p)
	}
	if err := r.clearColumn(p, height); err != nil {
		return err
	}
	// a cave under it: no floor to step onto — a block put in it (under
	// ground, where a bridge over a cave is how a mine goes on)
	if below := (world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}); r.openAt(below) && !r.openSky(p) {
		if err := r.fill(below); err != nil {
			plan("mine: the floor at %v: %v", below, err)
		}
	}
	return r.goTo(p)
}

// tunnelToward digs its way toward goal through what shuts it in, as a
// person digs out of a hole: a step at a time along the longer way to it,
// two high, a step down too while the goal is lower (three cleared, the head
// room of the step) — never under nor over itself. It stops after steps, at
// danger, or once a walk reaches the goal.
func (r *robot) tunnelToward(goal world.BlockPos, steps int) error {
	solid := func(b world.BlockPos) bool {
		st, ok := r.world.BlockAt(b)
		return ok && len(block.CollisionShape(st)) > 0
	}
	// a floor there, or one a block can be put on (something beside it) —
	// under ground only: on the land under the sky, no block over the air
	floored := func(f world.BlockPos) bool {
		if solid(f) {
			return true
		}
		if r.openSky(world.BlockPos{X: f.X, Y: f.Y + 1, Z: f.Z}) {
			return false
		}
		for _, n := range []world.BlockPos{{X: f.X + 1, Y: f.Y, Z: f.Z}, {X: f.X - 1, Y: f.Y, Z: f.Z}, {X: f.X, Y: f.Y, Z: f.Z + 1}, {X: f.X, Y: f.Y, Z: f.Z - 1}, {X: f.X, Y: f.Y - 1, Z: f.Z}} {
			if solid(n) {
				return true
			}
		}
		return false
	}
	// shut in, a pickaxe worn out on the way: by hand, slow, as a person
	// digs out of a trap with nothing else
	r.diggingOut = true
	defer func() { r.diggingOut = false }()
	var last [2]int
	for k := 0; k < steps; k++ {
		r.defend() // its tunnel may open into a cave with a zombie in it
		p := r.player.Position()
		cur := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
		dx, dz := goal.X-cur.X, goal.Z-cur.Z
		// toward the goal first (the longer way, then the other), then the
		// two others; never straight back
		dirs := [][2]int{{sign(dx), 0}, {0, sign(dz)}, {0, -sign(dz)}, {-sign(dx), 0}}
		if abs(dz) > abs(dx) {
			dirs = [][2]int{{0, sign(dz)}, {sign(dx), 0}, {-sign(dx), 0}, {0, -sign(dz)}}
		}
		moved, broke := false, false
		for _, d := range dirs {
			if d == ([2]int{}) || d == [2]int{-last[0], -last[1]} {
				continue
			}
			dys := []int{0}
			if goal.Y < cur.Y {
				dys = []int{-1, 0}
			}
			for _, dy := range dys {
				next := world.BlockPos{X: cur.X + d[0], Y: cur.Y + dy, Z: cur.Z + d[1]}
				if !floored(world.BlockPos{X: next.X, Y: next.Y - 1, Z: next.Z}) {
					continue // a drop (a ravine under it): not that way
				}
				height := 2
				if dy < 0 {
					height = 3 // the step's head room
				}
				open := r.openAt(next) && r.openAt(world.BlockPos{X: next.X, Y: next.Y + 1, Z: next.Z})
				if err := r.stepInto(next, height); err != nil {
					plan("dig out: %v", err)
					continue
				}
				last, moved, broke = d, true, open
				break
			}
			if moved {
				break
			}
		}
		if !moved {
			return fmt.Errorf("dig out toward %v: no way on from %v", goal, cur)
		}
		// into open space (a cave, the outside): a walk may go on from here
		// (a search from inside rock is long and finds nothing)
		if broke && r.goTo(goal) == nil {
			return nil
		}
	}
	return nil
}

// tunnel digs on from where the robot stands the way d, n blocks: width lanes
// (the robot's and those to its right), height high; a torch on the right wall
// every torchEvery blocks (0: none); the ores in reach dug, attackers fought.
// Water or lava ahead ends it early, as an error.
func (r *robot) tunnel(d [2]int, n, width, height, torchEvery int) (map[string]int, error) {
	rv := rightOf(d)
	found := map[string]int{}
	level := int(math.Floor(r.player.Position().Y + 1e-6))
	for i := 0; i < n; i++ {
		p := r.player.Position()
		x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
		if y != level {
			// it fell (or climbed): the tunnel is not dug on at another height
			return found, fmt.Errorf("off the tunnel's level %d: at %d %d %d", level, x, y, z)
		}
		next := world.BlockPos{X: x + d[0], Y: y, Z: z + d[1]}
		for j := 0; j < width; j++ {
			c := world.BlockPos{X: next.X + rv[0]*j, Y: y, Z: next.Z + rv[1]*j}
			if why := r.levelDanger(c.X, c.Y, c.Z); why != "" {
				return found, fmt.Errorf("%w: %s at %v", errDanger, why, c)
			}
			if err := r.clearColumn(c, height); err != nil {
				return found, err
			}
			// the floor ahead: a cave under it is a block put, as on a
			// bridge — not a drop it walks into
			if floor := (world.BlockPos{X: c.X, Y: y - 1, Z: c.Z}); r.openAt(floor) {
				if why := r.levelDanger(floor.X, floor.Y-1, floor.Z); why != "" {
					return found, fmt.Errorf("%w: %s under %v", errDanger, why, c)
				}
				if err := r.fill(floor); err != nil {
					return found, fmt.Errorf("%w: no floor at %v: %v", errDanger, floor, err)
				}
			}
		}
		if err := r.goTo(next); err != nil {
			return found, err
		}
		if torchEvery > 0 && i%torchEvery == torchEvery-1 {
			wall := world.BlockPos{X: x + rv[0]*width, Y: y + 1, Z: z + rv[1]*width}
			at := world.BlockPos{X: x + rv[0]*(width-1), Y: y + 1, Z: z + rv[1]*(width-1)}
			r.torchOn(at, wall)
		}
		r.fightIfAttacked()
		r.eatIfHungry()
		if r.player.Status().Health < 10 {
			r.rest(16) // hurt: healed before it goes on
		}
		r.tidy()
		for k, v := range r.digOres() {
			found[k] += v
		}
	}
	return found, nil
}

// errDanger: water or lava where the robot would dig.
var errDanger = errors.New("danger")
