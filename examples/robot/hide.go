package main

import (
	"fmt"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"log"
	"math"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Night under the open sky is where a player dies: zombies, skeletons, a
// creeper in the dark. Near home the way is in (the shelter); far from it, a
// player walls themself in where they stand — a block on every side at the
// feet and head, one over the head — and waits for the morning.

// outOfStream steps out of moving water to the nearest dry place beside it,
// as a person does who walked into a stream: it pushes, and drowns. It
// reports whether it was in one.
func (r *robot) outOfStream() bool {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	moving := func(pos world.BlockPos) bool {
		s, ok := r.world.BlockAt(pos)
		f := block.FluidOf(s)
		return ok && f != nil && !f.Source && f.Name != "minecraft:lava" && f.Name != "minecraft:flowing_lava"
	}
	if !moving(world.BlockPos{X: x, Y: y, Z: z}) && !moving(world.BlockPos{X: x, Y: y + 1, Z: z}) {
		return false
	}
	if r.avoid != nil {
		return false // its own field's water, being made: it runs a moment, then stands still
	}
	// a mob at it: the fight first (a step out of the water takes long, and
	// a zombie hits all the while)
	if time.Since(time.UnixMilli(r.lastHurt.Load())) < 5*time.Second {
		return false
	}
	for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, 6) {
		if hostileType(e.Type) {
			return false
		}
	}
	// stepped out of the same water again and again (it runs down its
	// stairs, sealing did not stop it): through it for two minutes
	if time.Now().Before(r.streamQuiet) {
		return false
	}
	if time.Since(r.streamFirst) > 3*time.Minute {
		r.streamFirst, r.streamN = time.Now(), 0
	}
	if r.streamN++; r.streamN > 5 {
		plan("in a stream at %d %d %d again: through it", x, y, z)
		r.streamQuiet = time.Now().Add(2 * time.Minute)
		return false
	}
	// sealed only underground (down its stairs, in a mine): the land's
	// streams and ponds are left as they are — they are water it wants
	stream := world.BlockPos{X: x, Y: y, Z: z}
	if !r.underSky() && !r.skyWithin(8) {
		defer r.sealWater(stream)
	}
	for d := 1; d <= 4; d++ {
		for dx := -d; dx <= d; dx++ {
			for dz := -d; dz <= d; dz++ {
				for dy := -1; dy <= 1; dy++ {
					c := world.BlockPos{X: x + dx, Y: y + dy, Z: z + dz}
					if !r.standable(c) {
						continue
					}
					plan("in a stream at %d %d %d: out to %v", x, y, z, c)
					if err := r.walkTo(c); err == nil {
						return true
					}
				}
			}
		}
	}
	return true
}

// sealWater stops the water at a stream it stepped out of, as lava is
// sealed: a block on each source within four blocks that is no lower than
// it, and on the moving water beside it that is not its own way.
func (r *robot) sealWater(at world.BlockPos) {
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			for dy := 0; dy <= 3; dy++ {
				c := world.BlockPos{X: at.X + dx, Y: at.Y + dy, Z: at.Z + dz}
				s, ok := r.world.BlockAt(c)
				f := block.FluidOf(s)
				if !ok || f == nil || f.Name != "minecraft:water" && f.Name != "minecraft:flowing_water" {
					continue
				}
				if r.onTheWay(c) || r.fieldWaterAt(c) || !f.Source && abs(dx)+abs(dz) > 1 {
					continue // its stairs stay open; far moving water stops with its source
				}
				if r.distanceTo(c) > 4.5 || r.ownColumn(c) {
					continue
				}
				if err := r.fill(c); err == nil {
					plan("water at %v sealed", c)
				}
			}
		}
	}
}

// wallInFromMob: hurt under rock with a mob after it, running is no use (it
// follows down the tunnel): blocks between, as a person puts them — walled
// in with what it carries, till nothing hostile is near for ten seconds (two
// minutes at most). It reports whether it walled itself in.
func (r *robot) wallInFromMob() bool {
	// an archer shoots a back that runs, down a tunnel or over open land:
	// blocks stop its arrows, at any health
	archer := false
	p := r.player.Position()
	for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, 24) {
		if rangedType(e.Type) {
			archer = true
		}
	}
	// run from again and again (it keeps up): blocks between, anywhere
	chased := time.Since(r.retreatsSince) < 30*time.Second && r.retreats >= 3
	if r.hiding || !archer && !chased && (r.underSky() || r.player.Status().Health >= 8) {
		return false
	}
	if r.countAll(fillBlocks()) < 4 {
		return false
	}
	r.hiding = true
	defer func() { r.hiding = false }()
	plan("a mob after it (archer %v): walled in till it is gone", archer)
	until := time.Now().Add(2 * time.Minute)
	var clear time.Time
	ans, err := r.wallInUntil(false, "till the mob was gone", func() bool {
		p := r.player.Position()
		reach := 8.0
		if archer {
			reach = 16 // it shoots from there
		}
		for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, reach) {
			if hostileType(e.Type) {
				clear = time.Time{}
				return time.Now().After(until)
			}
		}
		if clear.IsZero() {
			clear = time.Now()
		}
		return time.Since(clear) > 10*time.Second || time.Now().After(until)
	})
	if err != nil {
		plan("walled in: %v", err)
		return false
	}
	plan("%s", ans)
	return true
}

// hideFar is how far from home it hides where it is rather than going in.
const hideFar = 48

// hideIfNight hides it, from whatever it is doing, at night under the sky
// far from home; the work goes on in the morning. It reports whether it hid.
func (r *robot) hideIfNight() bool {
	if r.hiding || !r.underSky() {
		return false
	}
	if t, ok := r.clock.TimeOfDay(); !ok || t < 12000 || t >= 23000 { // dusk and night
		return false
	}
	if r.home != nil && (r.distanceTo(r.home.front) < hideFar || r.wayHome() < nightWalk) {
		return false // home is near: the work stops and it goes in (lateOut)
	}
	r.hiding = true
	defer func() { r.hiding = false }()
	plan("night under the sky, far from home: walled in till morning")
	if ans, err := cmdHide(r, nil); err != nil {
		plan("hide: %v", err)
		return false
	} else {
		plan("hide: %s", ans)
	}
	return true
}

// cmdHide walls the robot in where it stands — cobblestone (or dirt) round its
// feet and head and over its head — waits for the day, and opens the side
// toward home (any side, without one).
func cmdHide(r *robot, _ []string) (string, error) {
	return r.wallInUntil(true, "till morning", func() bool { return !r.night() })
}

// wallInUntil walls the robot in where it stands, waits till done, then
// opens the side toward home. fetch: blocks short, it gets more first (a
// night's hideout); without, it walls with what it has (a mob is at it).
func (r *robot) wallInUntil(fetch bool, why string, done func() bool) (string, error) {
	// to the middle of its block first, then the walls round it: standing
	// half over a side, the server puts no block where its body is
	r.walker.Stop() // whatever walk it was on, first
	var x, y, z int
	var walls []world.BlockPos
	need := 0
	spot := func() {
		for try := 0; try < 3; try++ {
			r.centre()
			p := r.player.Position()
			x, y, z = int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
			if !r.ownColumn(world.BlockPos{X: x + 1, Y: y, Z: z}) && !r.ownColumn(world.BlockPos{X: x - 1, Y: y, Z: z}) &&
				!r.ownColumn(world.BlockPos{X: x, Y: y, Z: z + 1}) && !r.ownColumn(world.BlockPos{X: x, Y: y, Z: z - 1}) {
				break // in its own column alone: the walls can go round it
			}
		}
		// built from the bottom up, each block against one under or beside it:
		// a side over a drop gets a block under it first (against its floor)
		walls = walls[:0]
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			for dy := -1; dy <= 1; dy++ {
				w := world.BlockPos{X: x + d[0], Y: y + dy, Z: z + d[1]}
				if dy == -1 && !r.openAt(w) {
					continue // solid under the side already
				}
				walls = append(walls, w)
			}
		}
		// the roof: against a block on one side wall's top (over its head
		// nothing touches but air)
		walls = append(walls, world.BlockPos{X: x + 1, Y: y + 2, Z: z}, world.BlockPos{X: x, Y: y + 2, Z: z})
		need = 0
		for _, w := range walls {
			if r.openAt(w) {
				need++
			}
		}
	}
	spot()
	// a night's hideout keeps eight over for a wall-in from a mob later
	if have := r.countAll(fillBlocks()); have < need+8 && fetch {
		// the blocks from round it, not from its own walls (a hideout of a
		// night before, standing)
		r.keepOut = &[2]world.BlockPos{{X: x - 2, Y: y - 2, Z: z - 2}, {X: x + 2, Y: y + 3, Z: z + 2}}
		err := r.get("minecraft:cobblestone", r.ui.Count("minecraft:cobblestone")+need+8-have, 1)
		if err != nil { // no stone in reach: dirt walls a night as well
			plan("hide: %v: dirt then", err)
			err = r.get("minecraft:dirt", r.ui.Count("minecraft:dirt")+need+8-have, 1)
		}
		r.keepOut = nil
		if err != nil {
			plan("hide: blocks to wall in with: %v (what it has, then)", err)
		}
		// fetching them took it elsewhere: the walls go round where it is now
		r.walker.Stop()
		spot()
	}
	for _, w := range walls {
		if err := r.fill(w); err != nil {
			plan("hide: wall at %v: %v", w, err)
		}
	}
	// walled in, it looks ahead, level, as a person waits (the roof put
	// over its head left it looking up the whole night)
	r.player.UpdatePosition(func(p basic.Position) basic.Position { p.Pitch = 0; return p })
	r.waiting.Store(true)
	if why == "till morning" {
		// for a watcher (a test harness may move the clock on: nothing
		// happens here till dawn)
		log.Printf("robot: waiting for the morning at %d %d %d", x, y, z)
	}
	for !done() {
		if err := r.ctl.WaitTicks(r.ctx, 40); err != nil {
			r.waiting.Store(false)
			return "", err
		}
		r.eatIfHungry()
	}
	r.waiting.Store(false)
	// out: the side toward home
	out := [2]int{1, 0}
	if r.home != nil {
		dx, dz := r.home.front.X-x, r.home.front.Z-z
		if abs(dx) >= abs(dz) {
			out = [2]int{sign(dx), 0}
		} else {
			out = [2]int{0, sign(dz)}
		}
		if out == ([2]int{}) {
			out = [2]int{1, 0}
		}
	}
	for dy := 0; dy <= 1; dy++ {
		c := world.BlockPos{X: x + out[0], Y: y + dy, Z: z + out[1]}
		if s, ok := r.world.BlockAt(c); ok && len(block.CollisionShape(s)) > 0 {
			if err := r.clear(c); err != nil {
				plan("hide: out at %v: %v", c, err)
			}
		}
	}
	return fmt.Sprintf("walled in at %d %d %d %s", x, y, z, why), nil
}
