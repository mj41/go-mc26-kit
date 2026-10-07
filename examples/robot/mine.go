package main

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// errNoTool: the tools the robot has do not get the block (its pickaxe wore out).
var errNoTool = errors.New("no tool")

// cmdDescend digs a staircase down to height y, as a person goes down for the
// ores: a step forward and down at a time, two high; a torch on the wall every
// few steps; a step that would open onto water or lava, or over a drop, turned
// away from; the ores it sees on the way dug out; what attacks it fought. It
// answers where it got to and what it found.
func cmdDescend(r *robot, args []string) (string, error) {
	if why := r.unfitForMine(); why != "" {
		if r.home != nil && r.distanceTo(r.home.centre()) > 4 { // not home (down already, or out): in
			if _, err := cmdIn(r, nil); err != nil {
				plan("in: %v", err)
			}
		}
		return "staying in: " + why, nil
	}
	if len(args) != 1 {
		return "", errors.New("want: descend <y>")
	}
	target, err := strconv.Atoi(args[0])
	if err != nil {
		return "", err
	}
	dirs := [4][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	// always into the hill: the way with the most rock over the stairs —
	// not the way it happens to look
	tried := map[int]bool{}
	inward := func() (int, bool) {
		p := r.player.Position()
		x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
		best, bestScore := -1, -1
		for i, d := range dirs {
			if tried[i] {
				continue
			}
			if sc := r.rockOver(x, y, z, d); sc > bestScore {
				best, bestScore = i, sc
			}
		}
		return best, best >= 0
	}
	// from home, the stairs start at the end of the shelter's tunnel, on into
	// the hill — not from the room's floor
	if m := r.home; m != nil && r.distanceTo(m.centre()) < 8 && len(r.stairs) == 0 {
		if err := r.goTo(m.cell(7, 0, 0)); err != nil {
			plan("descend: to the tunnel's end: %v", err)
		}
	}
	dir, _ := inward()
	if m := r.home; m != nil && len(r.stairs) == 0 {
		for i, d := range dirs {
			if d == m.d {
				dir = i // on the way the tunnel goes
			}
		}
	}
	plan("descend: into the hill toward %v", dirs[dir])
	found := map[string]int{}
	turns := 0
	r.rest(16) // not down into the dark hurt
	morning, early, failed, backs := r.nightShift(), "", 0, 0
	for step := 0; ; step++ {
		// the step it stands on — over a cave, at a step's edge, its middle
		// may be over the void beside it
		c := r.standCell()
		x, y, z := c.X, c.Y, c.Z
		if y <= target {
			break
		}
		r.addStair(world.BlockPos{X: x, Y: y, Z: z})
		if morning() {
			plan("descend: morning at %d %d %d: up to the day's work", x, y, z)
			early = " (morning)"
			r.fillForField() // water for the field, carried home
			break
		}
		r.upgrade()
		r.tidy()
		if step > 400 {
			return "", fmt.Errorf("still at %d after %d steps", y, step)
		}
		r.fightIfAttacked()
		r.eatIfHungry()
		if r.player.Status().Health < 10 {
			r.rest(16) // hurt: healed before it goes on
		}
		d := dirs[dir]
		fx, fz := x+d[0], z+d[1]
		if why := r.stepDanger(fx, y, fz); why != "" {
			plan("descend: not toward %d %d (%s): turning", fx, fz, why)
			tried[dir] = true
			next, ok := inward()
			if turns++; !ok || turns > 8 {
				// near the level it was going to: that is where it stops
				if y-target <= 6 {
					plan("descend: every way down from %d %d %d is dangerous; near enough to %d", x, y, z, target)
					break
				}
				// else back up four of its steps and on another way, as a
				// person does who meets an underground lake
				if backs < 3 && len(r.stairs) > 5 {
					backs++
					plan("descend: every way down from %d %d %d is dangerous: four steps back up, another way", x, y, z)
					if err := r.alongStairs(len(r.stairs) - 5); err == nil {
						r.stairs = r.stairs[:len(r.stairs)-4]
						clear(tried)
						tried[dir] = true // not the way it met the danger
						if next, ok := inward(); ok {
							dir = next
						}
						turns = 0
						continue
					}
				}
				return "", fmt.Errorf("every way down from %d %d %d is dangerous", x, y, z)
			}
			dir = next
			continue
		}
		plan("descend: step %d from %d %d %d toward %v", step, x, y, z, d)
		danger := false
		for _, cy := range []int{y + 1, y, y - 1} {
			if err := r.clear(world.BlockPos{X: fx, Y: cy, Z: fz}); err != nil {
				if !errors.Is(err, errDanger) {
					return "", fmt.Errorf("step %d: %w", step, err)
				}
				// lava behind the step's block: another way, as at water
				plan("descend: step %d: %v: turning", step, err)
				danger = true
				break
			}
		}
		if danger {
			tried[dir] = true
			if next, ok := inward(); ok && turns < 8 {
				turns++
				dir = next
				continue
			}
			return "", fmt.Errorf("every way down from %d %d %d is dangerous", x, y, z)
		}
		// nothing to step down onto: a cave below — bridged across, level,
		// and on five blocks into the rock past it before the stairs go on
		floor := world.BlockPos{X: fx, Y: y - 2, Z: fz}
		if s, ok := r.world.BlockAt(floor); ok && len(block.CollisionShape(s)) == 0 {
			// out of the hill into the open (a hillside, a valley): no
			// bridge in the air — another way, inside the rock
			if r.openSky(world.BlockPos{X: fx, Y: y - 1, Z: fz}) {
				plan("descend: step %d: the open air ahead, out of the hill: turning", step)
				tried[dir] = true
				next, ok := inward()
				if turns++; !ok || turns > 8 {
					return "", fmt.Errorf("the open air every way down from %d %d %d", x, y, z)
				}
				dir = next
				continue
			}
			if err := r.bridge(x, y, z, d, 256); err != nil {
				if errors.Is(err, errDied) {
					return "", err
				}
				plan("descend: no bridge from %d %d %d: %v", x, y, z, err)
				tried[dir] = true
				next, ok := inward()
				if turns++; !ok || turns > 8 {
					return "", fmt.Errorf("a cave under every way down from %d %d %d", x, y, z)
				}
				dir = next
				continue
			}
			turns = 0
			clear(tried)
			continue
		}
		// down onto the step (a walk: sneaking would keep it from stepping off
		// the edge), then to its middle on its own level — not left at its
		// far edge, over a cave past it
		_, err := cmdGoto(r, []string{strconv.Itoa(fx), strconv.Itoa(y - 1), strconv.Itoa(fz)})
		if err == nil {
			// only where its middle is over the void (the step's edge, a cave
			// past it): a walk to the middle each step slowed a night's stairs
			p := r.player.Position()
			if mid := (world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}); r.standCell() != mid {
				r.centre()
			}
		}
		if err != nil {
			// back onto the last step and that step again (something moved
			// it: a fight, a mob's push); three times running is the end
			// the second time, whatever is there written down and another
			// way into the hill, as at water or lava
			if failed++; failed >= 4 {
				return "", fmt.Errorf("step %d down to %d %d %d: %w", step, fx, y-1, fz, err)
			}
			plan("descend: step %d: %v; back onto %d %d %d", step, err, x, y, z)
			if err := r.goToLevel(world.BlockPos{X: x, Y: y, Z: z}); err != nil {
				plan("descend: back: %v", err)
			}
			if failed >= 2 {
				if a, err := cmdAround(r, nil); err == nil {
					plan("descend: stuck? %s", a)
				}
				tried[dir] = true
				if next, ok := inward(); ok {
					plan("descend: another way into the hill: %v", dirs[next])
					dir = next
				}
			}
			continue
		}
		failed = 0
		turns = 0
		clear(tried)
		if step%6 == 5 {
			r.torchBehind(x, y, z, d)
		}
		// out of the hill — the sky over it, a cliff's face: back into the
		// rock, the best way from here
		if !r.underRoof() {
			tried[dir] = true
			if next, ok := inward(); ok {
				plan("descend: out under the sky at %d %d %d: back into the hill toward %v", fx, y-1, fz, dirs[next])
				dir = next
			}
			clear(tried)
		}
		for name, n := range r.digOres() {
			found[name] += n
		}
	}
	r.remember()
	p := r.player.Position()
	var got []string
	for name, n := range found {
		got = append(got, fmt.Sprintf("%s=%d", strings.TrimPrefix(name, "minecraft:"), n))
	}
	return fmt.Sprintf("at %.1f %.1f %.1f found %s%s", p.X, p.Y, p.Z, strings.Join(got, " "), early), nil
}

// stepDanger is why the step toward (fx, fz) from height y must not be dug:
// a fluid in or next to a block it would open, "" when none.
func (r *robot) stepDanger(fx, y, fz int) string {
	if s := r.sculkNear(world.BlockPos{X: fx, Y: y, Z: fz}, sculkKeep); s != "" {
		return s
	}
	for _, cy := range []int{y + 1, y, y - 1} {
		c := world.BlockPos{X: fx, Y: cy, Z: fz}
		for _, o := range [7][3]int{{0, 0, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
			s, ok := r.world.BlockAt(world.BlockPos{X: c.X + o[0], Y: c.Y + o[1], Z: c.Z + o[2]})
			if !ok {
				return "not loaded"
			}
			if f := block.FluidOf(s); f != nil {
				return strings.TrimPrefix(f.Name, "minecraft:")
			}
		}
	}
	return ""
}

// isLava reports whether pos holds lava, still or flowing.
func (r *robot) isLava(pos world.BlockPos) bool {
	s, ok := r.world.BlockAt(pos)
	if !ok {
		return false
	}
	f := block.FluidOf(s)
	return f != nil && strings.Contains(f.Name, "lava")
}

var faces = [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}

// lavaBeside finds lava against one of pos's six faces.
func (r *robot) lavaBeside(pos world.BlockPos) (world.BlockPos, bool) {
	for _, f := range faces {
		if c := (world.BlockPos{X: pos.X + f[0], Y: pos.Y + f[1], Z: pos.Z + f[2]}); r.isLava(c) {
			return c, true
		}
	}
	return world.BlockPos{}, false
}

// lavaNear reports lava within n blocks of the robot.
func (r *robot) lavaNear(n int) bool {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	for dy := -n; dy <= n+1; dy++ {
		for dx := -n; dx <= n; dx++ {
			for dz := -n; dz <= n; dz++ {
				if r.isLava(world.BlockPos{X: x + dx, Y: y + dy, Z: z + dz}) {
					return true
				}
			}
		}
	}
	return false
}

// wayBack reports whether the robot has a free block beside it to step back
// to — one to stand in, no fluid, not the column it is about to dig.
func (r *robot) wayBack(dig world.BlockPos) bool {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		for dy := -1; dy <= 1; dy++ { // a stair step up or down is a way back too
			c := world.BlockPos{X: x + d[0], Y: y + dy, Z: z + d[1]}
			if c.X == dig.X && c.Z == dig.Z {
				continue
			}
			if r.standable(c) {
				return true
			}
		}
	}
	return false
}

// awayFromLava: lava at the robot or beside it — it steps back to a free
// block away from it and puts cobblestone where it stood, between itself
// and the lava, as a person stops lava spreading. It reports whether it
// met lava.
func (r *robot) awayFromLava() bool {
	p := r.player.Position()
	feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	head := world.BlockPos{X: feet.X, Y: feet.Y + 1, Z: feet.Z}
	var lava world.BlockPos
	found := false
	for _, c := range []world.BlockPos{feet, head, {X: head.X, Y: head.Y + 1, Z: head.Z}} {
		if r.isLava(c) {
			lava, found = c, true
		}
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if n := (world.BlockPos{X: c.X + d[0], Y: c.Y, Z: c.Z + d[1]}); !found && r.isLava(n) {
				lava, found = n, true
			}
		}
	}
	if !found {
		return false
	}
	// the free block beside it farthest from the lava
	best, bestD := world.BlockPos{}, -1.0
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		c := world.BlockPos{X: feet.X + d[0], Y: feet.Y, Z: feet.Z + d[1]}
		if !r.standable(c) || r.isLava(c) || r.isLava(world.BlockPos{X: c.X, Y: c.Y + 1, Z: c.Z}) {
			continue
		}
		if dd := math.Hypot(float64(c.X-lava.X), float64(c.Z-lava.Z)); dd > bestD {
			best, bestD = c, dd
		}
	}
	plan("lava at %v: away from it", lava)
	events.emit("lava", map[string]any{"x": lava.X, "y": lava.Y, "z": lava.Z})
	if bestD >= 0 {
		r.walker.Precise = true // wholly out of the place it seals
		err := r.walkTo(best)
		r.walker.Precise = false
		if err != nil {
			plan("lava: back to %v: %v", best, err)
		} else {
			// where it stood, now between it and the lava: shut
			for _, c := range []world.BlockPos{feet, head} {
				if err := r.fill(c); err != nil {
					plan("lava: seal %v: %v", c, err)
				}
			}
			return true
		}
	}
	// nowhere to step: the lava itself covered, if it is in reach
	if err := r.fill(lava); err != nil {
		plan("lava: cover %v: %v", lava, err)
	}
	return true
}

// underOrOver reports whether pos is lower than the robot's feet or higher
// than its head (in its column: under it or over it, not its body's room).
func (r *robot) underOrOver(pos world.BlockPos) bool {
	p := r.player.Position()
	return pos.Y < int(math.Floor(p.Y+1e-6)) || pos.Y > int(math.Floor(p.Y+1.62))
}

// standCell is the cell it stands in: its middle's — or, at the edge of a
// drop with nothing under that one, the cell beside whose block holds it up
// (it is 0.6 wide: a block under a corner of it is a floor).
func (r *robot) standCell() world.BlockPos {
	p := r.player.Position()
	y := int(math.Floor(p.Y + 1e-6))
	here := world.BlockPos{X: int(math.Floor(p.X)), Y: y, Z: int(math.Floor(p.Z))}
	holds := func(c world.BlockPos) bool {
		s, ok := r.world.BlockAt(world.BlockPos{X: c.X, Y: c.Y - 1, Z: c.Z})
		return ok && len(block.CollisionShape(s)) > 0
	}
	if p.Y-math.Floor(p.Y+1e-6) > 1e-3 || holds(here) {
		return here // on something in its own cell (a slab), or on the block under it
	}
	best, bestD := here, math.Inf(1)
	for _, x := range []int{int(math.Floor(p.X - 0.3)), int(math.Floor(p.X + 0.3 - 1e-9))} {
		for _, z := range []int{int(math.Floor(p.Z - 0.3)), int(math.Floor(p.Z + 0.3 - 1e-9))} {
			c := world.BlockPos{X: x, Y: y, Z: z}
			if c == here || !holds(c) {
				continue
			}
			if d := math.Hypot(float64(x)+0.5-p.X, float64(z)+0.5-p.Z); d < bestD {
				best, bestD = c, d
			}
		}
	}
	return best
}

// centre walks to the middle of the block it stands in, so its body stands
// in that column alone.
func (r *robot) centre() {
	here := r.standCell()
	r.walker.Precise = true
	defer func() { r.walker.Precise = false }()
	if err := r.walkTo(here); err != nil {
		plan("to the middle of %v: %v", here, err)
	}
}

// errOwnColumn: a block under the robot's feet or over its head, not dug.
var errOwnColumn = errors.New("not under itself nor over its head")

// ownColumn reports whether pos is in a column the robot's body stands in
// (it is 0.6 wide: one column, or two or four at their edges).
func (r *robot) ownColumn(pos world.BlockPos) bool {
	p := r.player.Position()
	return pos.X >= int(math.Floor(p.X-0.3)) && pos.X <= int(math.Floor(p.X+0.3-1e-9)) &&
		pos.Z >= int(math.Floor(p.Z-0.3)) && pos.Z <= int(math.Floor(p.Z+0.3-1e-9))
}

// hurts reports whether a block with no collision hurts who walks into it
// (a berry bush pricks and slows, a cobweb holds): such a block is in the
// way, dug as a wall is.
func hurts(s block.StateID) bool {
	if int(s) >= len(block.StateList) {
		return false
	}
	switch block.StateList[s].ID() {
	case "minecraft:sweet_berry_bush", "minecraft:cobweb", "minecraft:fire", "minecraft:soul_fire", "minecraft:wither_rose":
		return true
	}
	return false
}

// clear digs the block at pos until it is open — gravel and sand fall into
// the hole again — with the best tool in the hotbar.
func (r *robot) clear(pos world.BlockPos) error {
	if err := r.diedInOrder(); err != nil {
		return err
	}
	// never under itself or over its head: the floor gives way, or what is
	// over the block falls on it — a person digs the block ahead
	if r.ownColumn(pos) && r.underOrOver(pos) {
		// standing off the middle, half over the block's column: to the
		// middle of its own block first, as a person steps back from the
		// edge before digging the block ahead
		r.centre()
		if r.ownColumn(pos) && r.underOrOver(pos) {
			return fmt.Errorf("%w: %v", errOwnColumn, pos)
		}
	}
	for i := 0; i < 16; i++ {
		s, ok := r.world.BlockAt(pos)
		if !ok {
			return fmt.Errorf("%v not loaded", pos)
		}
		if len(block.CollisionShape(s)) == 0 && block.FluidOf(s) == nil && !hurts(s) {
			return nil
		}
		// lava against the block: dug, it pours in — another way
		if l, ok := r.lavaBeside(pos); ok {
			return fmt.Errorf("%w: lava at %v behind %v", errDanger, l, pos)
		}
		// lava near and no free block to step back to: not dug, so a way
		// back is there when lava comes
		if r.lavaNear(4) && !r.wayBack(pos) {
			return fmt.Errorf("%w: lava near and no way back from %v", errDanger, pos)
		}
		if err := r.holdBestTool(s); err != nil {
			return err
		}
		// stone by hand takes ages and drops nothing: a worn-out pickaxe
		// ends the work, it is not done by hand. A block soft enough to
		// break in a moment by hand (snow: its snowballs want a shovel) is
		// dug as it is, its drop not needed
		if block.RequiresCorrectTool(s) && block.DestroySpeed(s) > 0.5 && !r.diggingOut {
			if _, correct := act.ToolSpeed(&r.client.Tags, r.heldStack(), s); !correct {
				// worn out: a new stone pickaxe, made at the table near (the
				// mine's workshop), as a person goes back for one
				// once: making one digs stone, which wants a pickaxe — not
				// round again (none at all, after a death: a wooden one first)
				if r.newPick {
					return fmt.Errorf("%w for %s at %v", errNoTool, world.StateString(s), pos)
				}
				plan("the pickaxe wore out at %v: a new one", pos)
				r.newPick = true
				// stone first (the cobblestone it carries); none at all and no
				// stone on it either: a wooden one, then the stone one
				err := r.get("minecraft:stone_pickaxe", 1, 0)
				if err != nil && r.countAll(tagItems("minecraft:pickaxes")) == 0 {
					if err = r.get("minecraft:wooden_pickaxe", 1, 0); err == nil {
						err = r.get("minecraft:stone_pickaxe", 1, 0)
					}
				}
				r.newPick = false
				if err != nil {
					return fmt.Errorf("%w for %s at %v: %v", errNoTool, world.StateString(s), pos, err)
				}
				if err := r.holdBestTool(s); err != nil {
					return err
				}
				if _, correct := act.ToolSpeed(&r.client.Tags, r.heldStack(), s); !correct {
					return fmt.Errorf("%w for %s at %v", errNoTool, world.StateString(s), pos)
				}
			}
		}
		if _, err := cmdDig(r, []string{strconv.Itoa(pos.X), strconv.Itoa(pos.Y), strconv.Itoa(pos.Z)}); err != nil {
			return fmt.Errorf("dig %v: %w", pos, err)
		}
		r.awayFromLava()
		wait := 2
		// gravel or sand over it falls in — for a few ticks a falling
		// thing, not a block, so the hole looks open: it is waited for and
		// dug too, or it lands on the robot's head
		if a, ok := r.world.BlockAt(world.BlockPos{X: pos.X, Y: pos.Y + 1, Z: pos.Z}); ok && falls(a) {
			wait = 20
		}
		if err := r.ctl.WaitTicks(r.ctx, wait); err != nil {
			return err
		}
	}
	return fmt.Errorf("%v keeps filling", pos)
}

// falls reports whether a block falls when nothing is under it.
func falls(s block.StateID) bool {
	if int(s) >= len(block.StateList) {
		return false
	}
	id := block.StateList[s].ID()
	switch id {
	case "minecraft:gravel", "minecraft:sand", "minecraft:red_sand", "minecraft:suspicious_sand", "minecraft:suspicious_gravel":
		return true
	}
	return strings.HasSuffix(id, "_concrete_powder")
}

// fill puts a block of cobblestone (or dirt) at pos.
// fillBlocks is what it fills a hole or walls itself in with, the cheapest
// first: stone and earth, then its logs and planks (an archer's arrows stop
// at wood as well).
func fillBlocks() []string {
	out := []string{"minecraft:cobblestone", "minecraft:cobbled_deepslate", "minecraft:dirt"}
	out = append(out, tagItems("minecraft:planks")...)
	return append(out, logItems()...)
}

func (r *robot) fill(pos world.BlockPos) error {
	if err := r.diedInOrder(); err != nil {
		return err
	}
	if st, ok := r.world.BlockAt(pos); ok && len(block.CollisionShape(st)) > 0 {
		return nil // filled already (a way in shut before)
	} else if ok && !block.Replaceable(st) && block.FluidOf(st) == nil {
		// a torch (its own), a flower: taken up first, as a block cannot go
		// where it is
		if err := r.clear(pos); err != nil {
			return fmt.Errorf("%s in the way: %w", world.StateString(st), err)
		}
	}
	for _, m := range fillBlocks() {
		if r.ui.Count(m) == 0 {
			continue
		}
		if _, err := r.toHotbar(m, true); err != nil {
			return err
		}
		_, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(pos, done) })
		return err
	}
	return errors.New("nothing to fill with")
}

// torchBehind puts a torch on the right wall of the step the robot just left
// (x, y, z), at head height, going the way d (the left, if the right is open).
func (r *robot) torchBehind(x, y, z int, d [2]int) {
	if r.ui.Count("minecraft:torch") == 0 {
		return
	}
	at := world.BlockPos{X: x, Y: y + 1, Z: z}
	if s, ok := r.world.BlockAt(at); !ok || !block.Replaceable(s) {
		return
	}
	// the right wall going down, so the torches are on the left going back up
	for _, side := range [2][2]int{{-d[1], d[0]}, {d[1], -d[0]}} {
		wall := world.BlockPos{X: x + side[0], Y: y + 1, Z: z + side[1]}
		s, ok := r.world.BlockAt(wall)
		if !ok || block.SturdyFaces(s) == 0 {
			continue
		}
		if _, err := r.toHotbar("minecraft:torch", true); err != nil {
			return
		}
		if _, err := waitUse(r, func(done func(act.Use)) { r.hands.PlaceOn(at, wall, done) }); err != nil {
			plan("descend: torch at %v: %v", at, err)
		}
		return
	}
}

// digOres digs the ores within reach that are open to the air — the walls of
// its own staircase, a cave beside it — with a tool that gets their drop.
func (r *robot) digOres() map[string]int {
	got := map[string]int{}
	skip := map[world.BlockPos]bool{}
	p := r.player.Position()
	feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	var floorHoles []world.BlockPos
	targets := oreBlocks()
	// coal enough (torches, a night's smelting): its ore passed by, the
	// night's stairs not spent on it (80 dug on the way, the morning came
	// halfway down)
	if r.ui.Count("minecraft:coal") >= 32 {
		targets = slices.DeleteFunc(slices.Clone(targets), func(b string) bool { return strings.HasSuffix(b, "coal_ore") })
	}
	if r.wantsString() {
		targets = append(targets, "minecraft:cobweb") // cut with the sword: string for a rod
	}
	for i := 0; i < 8; i++ {
		pos, ok := r.nearestWhere(targets, skip, func(p world.BlockPos) bool {
			// never under it (a cave may be there) nor over its head (what
			// is over that falls on it), nor in the floor beside it (the
			// hole is a step down it walks into later): the ores beside it
			// and ahead, its feet's level and up
			return p.Y >= feet.Y && !r.ownColumn(p) && r.exposed(p) && r.withinReach(p)
		})
		if !ok {
			break
		}
		skip[pos] = true
		s, _ := r.world.BlockAt(pos)
		if err := r.holdBestTool(s); err != nil {
			break
		}
		if _, correct := act.ToolSpeed(&r.client.Tags, r.heldStack(), s); !correct {
			continue // iron by a wooden pickaxe drops nothing: left for later
		}
		name := block.StateList[s].ID()
		if _, err := cmdDig(r, []string{strconv.Itoa(pos.X), strconv.Itoa(pos.Y), strconv.Itoa(pos.Z)}); err != nil {
			plan("descend: ore at %v: %v", pos, err)
			continue
		}
		plan("descend: dug %s at %v", name, pos)
		got[name]++
		if pos.Y < feet.Y {
			floorHoles = append(floorHoles, pos)
		}
	}
	if len(got) > 0 {
		if _, err := cmdCollect(r, []string{"4"}); err != nil {
			plan("descend: collect: %v", err)
		}
	}
	// a hole an ore left in the floor: filled, as a person does, or a step
	// into it later may drop into a cave
	for _, h := range floorHoles {
		if r.openAt(h) {
			if err := r.fill(h); err != nil {
				plan("descend: refill %v: %v", h, err)
			}
		}
	}
	return got
}

// withinReach: the centre of the block at pos is within 4.2 of the robot's eye.
func (r *robot) withinReach(pos world.BlockPos) bool {
	p := r.player.Position()
	dx, dy, dz := float64(pos.X)+0.5-p.X, float64(pos.Y)+0.5-(p.Y+1.62), float64(pos.Z)+0.5-p.Z
	return math.Sqrt(dx*dx+dy*dy+dz*dz) <= 4.2
}

// fightIfAttacked is defend, in the mine.
func (r *robot) fightIfAttacked() { r.defend() }

// bridge goes level from (x, y, z) the way d across a cave, as a person does:
// the way ahead cleared two high, a block of cobblestone put under each step
// where the ground is missing; once there is ground under the step and under
// the one below it again, five steps on into the rock, so the stairs that go
// on down do not open into the cave. At most max steps across.
func (r *robot) bridge(x, y, z int, d [2]int, max int) error {
	open := func(p world.BlockPos) bool {
		s, ok := r.world.BlockAt(p)
		return ok && len(block.CollisionShape(s)) == 0
	}
	// shift held all the way across, standing still and fighting too: a
	// mob's hit does not push it off; and no digging round a step that fails
	r.walker.Sneak, r.onBridge = true, true
	climbing := r.climbing
	r.climbing = true
	defer func() { r.walker.Sneak, r.onBridge, r.climbing = false, false, climbing }()
	cx, cz := x, z
	beyond := -1 // steps since the far side
	for i := 0; i < max+5; i++ {
		nx, nz := cx+d[0], cz+d[1]
		for _, cy := range []int{y, y + 1} {
			if err := r.clear(world.BlockPos{X: nx, Y: cy, Z: nz}); err != nil {
				return err
			}
		}
		under := world.BlockPos{X: nx, Y: y - 1, Z: nz}
		if open(under) {
			if why := r.stepDanger(nx, y, nz); why != "" {
				return fmt.Errorf("the bridge would meet %s at %d %d", why, nx, nz)
			}
			if err := r.fill(under); err != nil {
				return fmt.Errorf("bridge at %v: %w", under, err)
			}
		}
		if beyond < 0 && !open(under) && !open(world.BlockPos{X: nx, Y: y - 2, Z: nz}) && i > 0 {
			beyond = 0
			plan("descend: across the cave at %d %d %d", nx, y, nz)
		}
		_, err := cmdGoto(r, []string{strconv.Itoa(nx), strconv.Itoa(y), strconv.Itoa(nz)})
		if err != nil {
			return fmt.Errorf("bridge step to %d %d %d: %w", nx, y, nz, err)
		}
		cx, cz = nx, nz
		if i%8 == 7 {
			plan("descend: bridging, at %d %d %d", cx, y, cz)
		}
		if beyond >= 0 {
			if beyond++; beyond > 5 {
				return nil
			}
		}
	}
	return fmt.Errorf("the cave is wider than %d", max)
}

// branchMine tunnels for an ore that is not in sight, as a person strip-mines
// deep down: a tunnel two high straight on, 32 blocks a leg, a turn at each
// leg's end, a torch every eight blocks, the walls looked over at every step
// for the blocks of src (and the ores passed dug). It stops when one of src
// shows, open to the tunnel.
func (r *robot) branchMine(src []string, legs int) error {
	dirs := [4][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	yaw := float64(r.player.Position().Yaw) * math.Pi / 180
	fx, fz := -math.Sin(yaw), math.Cos(yaw)
	dir, bestDot := 0, -2.0
	for i, d := range dirs {
		if dot := fx*float64(d[0]) + fz*float64(d[1]); dot > bestDot {
			dir, bestDot = i, dot
		}
	}
	for leg := 0; leg < legs; leg++ {
		d := dirs[dir]
		plan("branch mine: leg %d toward %v", leg, d)
		for i := 0; i < 32; i++ {
			if pos, ok := r.nearestWhere(src, nil, r.exposed); ok && r.distanceTo(pos) < 24 {
				plan("branch mine: %v in sight at %v", src, pos)
				return nil
			}
			p := r.player.Position()
			x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
			nx, nz := x+d[0], z+d[1]
			if why := r.levelDanger(nx, y, nz); why != "" {
				plan("branch mine: %s ahead at %d %d %d: turning", why, nx, y, nz)
				break
			}
			for _, cy := range []int{y, y + 1} {
				if err := r.clear(world.BlockPos{X: nx, Y: cy, Z: nz}); err != nil {
					return err
				}
			}
			if floor := (world.BlockPos{X: nx, Y: y - 1, Z: nz}); r.openAt(floor) {
				if err := r.fill(floor); err != nil {
					break
				}
			}
			if _, err := cmdGoto(r, []string{strconv.Itoa(nx), strconv.Itoa(y), strconv.Itoa(nz)}); err != nil {
				break
			}
			if i%8 == 7 {
				r.torchBehind(x, y, z, d)
			}
			r.fightIfAttacked()
			r.eatIfHungry()
			r.digOres()
		}
		dir = (dir + 1) % 4
	}
	return fmt.Errorf("%w: no %v found mining %d legs", errNoWay, src, legs)
}

// levelDanger is why the tunnel must not go on into (x, y..y+1, z): a fluid in
// or next to the two blocks it would open.
func (r *robot) levelDanger(x, y, z int) string {
	if s := r.sculkNear(world.BlockPos{X: x, Y: y, Z: z}, sculkKeep); s != "" {
		return s
	}
	for _, cy := range []int{y, y + 1} {
		for _, o := range [7][3]int{{0, 0, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
			st, ok := r.world.BlockAt(world.BlockPos{X: x + o[0], Y: cy + o[1], Z: z + o[2]})
			if !ok {
				return "not loaded"
			}
			if f := block.FluidOf(st); f != nil {
				return strings.TrimPrefix(f.Name, "minecraft:")
			}
		}
	}
	return ""
}

// openAt: nothing to stand on at pos.
func (r *robot) openAt(pos world.BlockPos) bool {
	st, ok := r.world.BlockAt(pos)
	return ok && len(block.CollisionShape(st)) == 0
}

// rockOver is how much rock lies over a staircase going down from (x, y, z)
// the way d: the solid blocks above its first twelve steps, up to twenty
// over each — the way into a hill scores high, the way out to its side low.
func (r *robot) rockOver(x, y, z int, d [2]int) int {
	n := 0
	for k := 1; k <= 12; k++ {
		for h := 2; h <= 20; h++ {
			s, ok := r.world.BlockAt(world.BlockPos{X: x + d[0]*k, Y: y - k + h, Z: z + d[1]*k})
			if ok && len(block.CollisionShape(s)) > 0 && block.FluidOf(s) == nil {
				n++
			}
		}
	}
	return n
}

// The deep dark: a sculk sensor hears every step and every block broken
// within eight blocks, and a shrieker it tells (or one stepped on) calls a
// warden at the fourth time — five hundred health, and a sonic boom through
// rock when it cannot reach. A person keeps their work and their steps away
// from the sculk; the robot keeps ten blocks from every sensor and shrieker.
const sculkKeep = 10

var sculkListeners = map[string]bool{
	"minecraft:sculk_sensor": true, "minecraft:calibrated_sculk_sensor": true, "minecraft:sculk_shrieker": true,
}

// sculkNear names a sculk sensor or shrieker within d blocks of p ("" for
// none).
func (r *robot) sculkNear(p world.BlockPos, d int) string {
	for dy := -d; dy <= d; dy++ {
		for dx := -d; dx <= d; dx++ {
			for dz := -d; dz <= d; dz++ {
				q := world.BlockPos{X: p.X + dx, Y: p.Y + dy, Z: p.Z + dz}
				s, ok := r.world.BlockAt(q)
				if !ok || int(s) >= len(block.StateList) {
					continue
				}
				if id := block.StateList[s].ID(); sculkListeners[id] {
					return fmt.Sprintf("%s at %v (the deep dark)", strings.TrimPrefix(id, "minecraft:"), q)
				}
			}
		}
	}
	return ""
}

// floorAhead reports whether pos, in the row the robot's feet are in, is a
// block it would stand on rather than walk into: its top at or below the
// feet (on farmland, 15/16 high, the feet are in the farmland's row and the
// next farmland is the floor ahead — dug as "in the way", the field's water
// ran into it).
func (r *robot) floorAhead(pos world.BlockPos) bool {
	p := r.player.Position()
	if pos.Y != int(math.Floor(p.Y+1e-6)) {
		return false
	}
	s, ok := r.world.BlockAt(pos)
	if !ok {
		return false
	}
	shape := block.CollisionShape(s)
	if len(shape) == 0 {
		return false
	}
	top := 0.0
	for _, b := range shape {
		top = math.Max(top, b.MaxY)
	}
	return float64(pos.Y)+top <= p.Y+1e-3
}
