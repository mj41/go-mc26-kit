package main

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/level/component"
)

// weaponScore is what an item does to a mob a second: its damage (the
// fist's one and the attack_damage modifiers) times its swings a second (four
// and the attack_speed modifiers). An iron sword (6 x 1.6) over a stone axe
// (9 x 0.8): the axe hits harder, half as often, and a zombie gets its hits in
// between (twelve health lost to one, 2026-10-06).
func weaponScore(s screen.Slot) float64 {
	dmg, speed := 1.0, 4.0
	if s.Count == 0 {
		return dmg * speed
	}
	if mods := act.Component[*component.AttributeModifiers](s); mods != nil {
		for _, m := range mods.Modifiers {
			if int(m.Attribute) >= len(registryid.Attribute) {
				continue
			}
			switch registryid.Attribute[m.Attribute] {
			case "minecraft:attack_damage":
				dmg += float64(m.Modifier.Amount)
			case "minecraft:attack_speed":
				speed += float64(m.Modifier.Amount)
			}
		}
	}
	return dmg * speed
}

// holdBestWeapon takes in hand what does the most damage a second, from
// anywhere in its inventory (a sword in the pack is no use there).
func (r *robot) holdBestWeapon() error {
	r.screens.Lock()
	held := weaponScore(r.screens.Inventory.Slots[36+r.screens.HeldSlot])
	best, bestScore := "", held
	for i := 9; i < 45; i++ {
		s := r.screens.Inventory.Slots[i]
		if sc := weaponScore(s); s.Count > 0 && sc > bestScore+1e-9 {
			best, bestScore = itemName(int32(s.Item)), sc
		}
	}
	r.screens.Unlock()
	if best == "" {
		return nil // the best in hand already
	}
	_, err := r.toHotbar(best, true)
	return err
}

// cmdGuard fights the hostile mobs within the radius until none is left for
// three seconds, or the time is up: it goes after the nearest with its best
// weapon and hits it whenever the attack is charged.
func cmdGuard(r *robot, args []string) (string, error) {
	radius, limit := 12.0, 60*time.Second
	if len(args) >= 1 {
		v, err := strconv.ParseFloat(args[0], 64)
		if err != nil {
			return "", err
		}
		radius = v
	}
	if len(args) >= 2 {
		v, err := strconv.Atoi(args[1])
		if err != nil {
			return "", err
		}
		limit = time.Duration(v) * time.Second
	}
	killed, err := r.fightWhere(radius, limit, func(e entities.Entity) bool { return hostileType(e.Type) }, 0)
	if err != nil {
		return fmt.Sprintf("killed=%d", killed), err
	}
	return fmt.Sprintf("killed=%d", killed), nil
}

// cmdHunt hunts n (2) animals for their meat — the nearest within 48 blocks
// first — and picks up what they drop. It answers how many it killed and the
// meat it has.
func cmdHunt(r *robot, args []string) (string, error) {
	n := 2
	if len(args) == 1 {
		v, err := strconv.Atoi(args[0])
		if err != nil {
			return "", err
		}
		n = v
	}
	// none about: it goes looking, as a hunter does — a walk off another way
	// each time, round where it set out (never far from home), for a few
	// minutes at most
	// (a hunt by day ends at dusk: no walking off in the dark)
	killed, deadline, byDay := 0, time.Now().Add(4*time.Minute), !r.night()
	p0 := r.player.Position()
	from := world.BlockPos{X: int(math.Floor(p0.X)), Y: int(math.Floor(p0.Y + 1e-6)), Z: int(math.Floor(p0.Z))}
	var err error
	for try := 0; killed < n && time.Now().Before(deadline) && !(byDay && r.night()); try++ {
		var k int
		k, err = r.fightWhere(48, time.Until(deadline), func(e entities.Entity) bool { return gameType(e.Type) }, n-killed)
		killed += k
		if err != nil || killed >= n || try == 5 {
			break
		}
		if eerr := r.wander(from, try); eerr != nil {
			plan("hunt: %v", eerr)
		}
	}
	if _, cerr := cmdCollect(r, []string{"8"}); cerr != nil {
		log.Printf("hunt: collect: %v", cerr)
	}
	meat := 0
	for _, m := range []string{"minecraft:beef", "minecraft:porkchop", "minecraft:mutton", "minecraft:chicken", "minecraft:rabbit"} {
		meat += r.ui.Count(m)
	}
	if err != nil && killed == 0 {
		return "", err
	}
	return fmt.Sprintf("killed=%d meat=%d", killed, meat), nil
}

// fightWhere fights the mobs want picks within radius, the nearest first, with
// the best weapon, hitting whenever the attack is charged, until none is left
// for three seconds, max are killed (0: no limit), or the time is up.
func (r *robot) fightWhere(radius float64, limit time.Duration, want func(entities.Entity) bool, max int) (int, error) {
	if err := r.holdBestWeapon(); err != nil {
		return 0, err
	}
	defer r.walker.Stop()
	hits, killed := map[int32]bool{}, 0
	// a target not hit within ten seconds is out of reach (behind a fence, in
	// a wall): it is left alone
	since, ignored := map[int32]time.Time{}, map[int32]bool{}
	quietSince, deadline := time.Now(), time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if r.player.Status().Health <= 0 {
			return killed, fmt.Errorf("died (killed %d)", killed)
		}
		p := r.player.Position()
		var target *entities.Entity
		for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, radius) {
			if want(e) && !ignored[e.ID] {
				target = &e
				break
			}
		}
		// a creeper coming while it hunts or fights: away from it first
		for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, 8) {
			if e.Type == "minecraft:creeper" {
				r.walker.Stop()
				log.Printf("robot: a creeper at %.1f %.1f %.1f: away from it", e.X, e.Y, e.Z)
				r.retreat(e.X, e.Z)
				events.emit("fight", map[string]any{"against": e.Type, "killed": 0, "fled": true})
				break
			}
		}
		for id := range hits { // a mob hit and gone is a kill
			if _, ok := r.entities.Get(id); !ok {
				delete(hits, id)
				killed++
			}
		}
		if max > 0 && killed >= max {
			return killed, nil
		}
		// too hurt to go on: away, running, as a person does, to eat and heal
		// — but one mob it fights out till six: its back turned on a bridge
		// or in a tunnel, the mob follows and hits it from behind; against
		// two it goes at twelve, three or more at sixteen (a zombie with a
		// sword hits for ten)
		n := r.hostilesNear(p.X, p.Y, p.Z, 8)
		if h := r.player.Status().Health; h > 0 && target != nil && !r.onBridge && (h <= 6 || h <= 12 && n >= 2 || h <= 16 && n >= 3) {
			r.walker.Stop()
			r.retreat(target.X, target.Z)
			return killed, fmt.Errorf("retreated, health %.1f (killed %d)", h, killed)
		}
		if target == nil {
			r.walker.Stop()
			if time.Since(quietSince) > 3*time.Second {
				return killed, nil
			}
			if err := r.ctl.WaitTicks(r.ctx, 5); err != nil {
				return killed, err
			}
			continue
		}
		quietSince = time.Now()
		id := target.ID
		if _, ok := since[id]; !ok {
			since[id] = time.Now()
		}
		if time.Since(since[id]) > 10*time.Second {
			ignored[id] = true
			log.Printf("guard: %s %d at %.1f %.1f %.1f out of reach from %.1f %.1f %.1f, left alone", target.Type, id, target.X, target.Y, target.Z, p.X, p.Y, p.Z)
			continue
		}
		where := func() (entities.Entity, bool) { return r.entities.Get(id) }
		// on a bridge it stands its ground and lets the mob come: following
		// it is the way off the edge
		if !r.onBridge {
			r.walker.Follow(func() (float64, float64, float64, bool) {
				e, ok := where()
				return e.X, e.Y, e.Z, ok
			}, 1.5)
		}
		if math.Hypot(target.X-p.X, target.Z-p.Z) < 2.8 && math.Abs(target.Y-p.Y) < 1.5 && !r.hands.Busy() {
			done := make(chan act.Attack, 1)
			r.hands.Attack(id, where, func(a act.Attack) { done <- a })
			select {
			case a := <-done:
				if a.Err == nil {
					hits[id] = true
					since[id] = time.Now()
				}
				log.Printf("guard: attack %s %d: waited %d, %v", target.Type, id, a.Waited, a.Err)
			case <-time.After(5 * time.Second):
			}
		}
		if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
			return killed, err
		}
	}
	return killed, fmt.Errorf("still fighting after %s", limit)
}

// outOfWall digs the robot free when a block is where its eyes are — gravel
// fallen on it, a block put down wrongly: a player in a wall suffocates, one
// heart a second, and is only slowly pushed out.
func (r *robot) outOfWall() {
	p := r.player.Position()
	ex, ey, ez := p.X, p.Y+1.62, p.Z
	eye := world.BlockPos{X: int(math.Floor(ex)), Y: int(math.Floor(ey)), Z: int(math.Floor(ez))}
	s, ok := r.world.BlockAt(eye)
	if !ok {
		return
	}
	in := false // the eyes in the block's shape (a ladder or a slab beside them is not a wall)
	fx, fy, fz := ex-float64(eye.X), ey-float64(eye.Y), ez-float64(eye.Z)
	for _, b := range block.CollisionShape(s) {
		if fx > b.MinX && fx < b.MaxX && fy > b.MinY && fy < b.MaxY && fz > b.MinZ && fz < b.MaxZ {
			in = true
		}
	}
	if !in {
		return
	}
	plan("in a wall: %s at %v, dug out", world.StateString(s), eye)
	if err := r.clear(eye); err != nil {
		plan("in a wall: %v", err)
	}
}

// wander walks to a place about thirty blocks from origin, a new direction
// each try — a look round, not a journey.
func (r *robot) wander(origin world.BlockPos, try int) error {
	a := float64(try)*math.Pi*0.75 + math.Pi/4
	for d := 32.0; d >= 16; d -= 8 {
		x, z := int(math.Floor(float64(origin.X)+math.Cos(a)*d)), int(math.Floor(float64(origin.Z)+math.Sin(a)*d))
		g, ok := r.groundAt(x, z, origin.Y)
		if !ok {
			continue
		}
		plan("wander: to %d %d %d", g.X, g.Y, g.Z)
		return r.goTo(g)
	}
	return fmt.Errorf("no ground toward %s", compass(a))
}

// hostilesNear counts the hostile mobs within radius of x, y, z.
func (r *robot) hostilesNear(x, y, z, radius float64) int {
	n := 0
	for _, e := range r.entities.Nearby(x, y, z, radius) {
		if hostileType(e.Type) {
			n++
		}
	}
	return n
}

// defend fights back, from whatever the robot is doing, when a hostile mob is
// close (6 blocks) or it was just hurt (5 seconds) with one about (12): a
// person stops chopping when a zombie walks up. It reports whether it fought.
func (r *robot) defend() bool {
	// its own walks check again (a hideout's blocks fetched: a fight on the
	// way, yes); deeper than that, no
	if r.defending >= 2 {
		return false
	}
	r.defending++
	defer func() { r.defending-- }()
	r.outOfWall()
	r.awayFromLava()
	r.outOfStream()
	r.hideIfNight()
	if r.awayFromWarden() {
		return true
	}
	p := r.player.Position()
	hurt := time.Since(time.UnixMilli(r.lastHurt.Load())) < 5*time.Second
	look := 12.0
	if hurt {
		look = 20 // hit from further: an archer
	}
	near := r.entities.Nearby(p.X, p.Y, p.Z, look)
	if hurt {
		// hit, and the creeper is not yet at it (it only blows up): the
		// hit came from another (an archer) — that one first
		sort.SliceStable(near, func(i, j int) bool {
			far := func(e entities.Entity) bool {
				return e.Type == "minecraft:creeper" && e.DistanceSqr(p.X, p.Y, p.Z) > 16
			}
			return !far(near[i]) && far(near[j])
		})
	}
	for _, e := range near {
		if !hostileType(e.Type) || e.Type == "minecraft:warden" { // a warden is never fought (awayFromWarden)
			continue
		}
		// a creeper is left while it is still eight off: within three its
		// fuse is lit, and running then is too late
		creeperNear := e.Type == "minecraft:creeper" && e.DistanceSqr(p.X, p.Y, p.Z) <= 64
		if e.DistanceSqr(p.X, p.Y, p.Z) > 36 && !hurt && !creeperNear {
			continue
		}
		// one a defence could not reach (a spider in the cave under the
		// stairs) and that has not hurt it since: left alone a while
		if time.Now().Before(r.leftAlone[e.ID]) && !hurt {
			continue
		}
		// a creeper is not fought but left: one that reaches it blows up
		// for more than its whole health
		if e.Type == "minecraft:creeper" {
			// close, its fuse lit: a hit first — the knockback throws it out
			// of its blast's reach and its fuse goes out as the robot gets
			// away; backing off alone it follows
			if e.DistanceSqr(p.X, p.Y, p.Z) <= 3.5*3.5 && r.holdBestWeapon() == nil {
				id := e.ID
				where := func() (entities.Entity, bool) { return r.entities.Get(id) }
				done := make(chan act.Attack, 1)
				r.hands.Attack(id, where, func(a act.Attack) { done <- a })
				select {
				case a := <-done:
					log.Printf("robot: hit the creeper %d: %v", id, a.Err)
				case <-time.After(2 * time.Second):
				}
			}
			log.Printf("robot: a creeper at %.1f %.1f %.1f: away from it", e.X, e.Y, e.Z)
			r.retreat(e.X, e.Z)
			events.emit("fight", map[string]any{"against": e.Type, "killed": 0, "fled": true})
			return true
		}
		log.Printf("robot: %s at %.1f %.1f %.1f: defending", e.Type, e.X, e.Y, e.Z)
		began := time.Now()
		killed, err := r.fightWhere(max(10, math.Sqrt(e.DistanceSqr(p.X, p.Y, p.Z))+2), 30*time.Second, func(e entities.Entity) bool { return hostileType(e.Type) && e.Type != "minecraft:creeper" }, 0)
		log.Printf("robot: defended: killed=%d %v", killed, err)
		if killed == 0 && time.UnixMilli(r.lastHurt.Load()).Before(began) {
			// none killed, not hurt: the ones about are out of its reach —
			// a minute on with its work, not another fight with them
			if r.leftAlone == nil {
				r.leftAlone = map[int32]time.Time{}
			}
			q := r.player.Position()
			for _, m := range r.entities.Nearby(q.X, q.Y, q.Z, 12) {
				if hostileType(m.Type) {
					r.leftAlone[m.ID] = time.Now().Add(time.Minute)
				}
			}
		}
		events.emit("fight", map[string]any{"against": e.Type, "killed": killed})
		// hungry: what they dropped (a zombie's flesh is food, at a pinch);
		// without a rod yet: a spider's string
		if killed > 0 && (r.food.Load() <= 14 || r.wantsString()) {
			if _, cerr := cmdCollect(r, []string{"6"}); cerr != nil {
				log.Printf("robot: collect after the fight: %v", cerr)
			}
		}
		return true
	}
	// badly hurt and nothing about: it stops and heals (eats, waits), as a
	// person does before going on with a heart left
	if h := r.player.Status().Health; h > 0 && h < 8 && !r.healing && time.Since(r.restedAt) > time.Minute {
		r.healing = true
		r.rest(14)
		r.healing, r.restedAt = false, time.Now() // with nothing to eat, not again for a minute
	}
	return false
}

// retreat runs some sixteen blocks straight away from (fromX, fromZ), to
// where the ground is, and eats.
func (r *robot) retreat(fromX, fromZ float64) {
	if r.hiding {
		return // walling itself in (or walled in): a run would leave the walls half built
	}
	if time.Since(r.retreatsSince) > 30*time.Second {
		r.retreatsSince, r.retreats = time.Now(), 0
	}
	r.retreats++
	if r.wallInFromMob() { // in a tunnel, or chased: blocks between, not a run it follows
		r.retreats = 0
		return
	}
	p := r.player.Position()
	dx, dz := p.X-fromX, p.Z-fromZ
	d := math.Hypot(dx, dz)
	if d < 0.1 {
		dx, dz, d = 1, 0, 1
	}
	log.Printf("robot: retreating from %.1f %.1f", fromX, fromZ)
	events.emit("retreat", map[string]any{"health": r.player.Status().Health})
	for _, dist := range []float64{16, 10, 6} {
		x, z := int(math.Floor(p.X+dx/d*dist)), int(math.Floor(p.Z+dz/d*dist))
		g, ok := r.groundNear(x, z, int(math.Floor(p.Y)))
		if !ok {
			continue
		}
		r.walker.Sprint = true
		r.walker.Goto(g)
		for t := 0; t < 60 && r.walker.Status() == "walking"; t++ {
			if err := r.ctl.WaitTicks(r.ctx, 2); err != nil {
				break
			}
		}
		r.walker.Sprint = false
		r.walker.Stop()
		break
	}
	r.eatIfHungry()
}

// awayFromWarden: a warden within thirty-two blocks is never fought (five
// hundred health; its sonic boom carries fifteen through rock): the robot
// stops its noise and goes up its stairs home, and the mine waits for
// another night. It reports whether it went.
func (r *robot) awayFromWarden() bool {
	if r.fleeing {
		return false
	}
	p := r.player.Position()
	for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, 32) {
		if e.Type != "minecraft:warden" {
			continue
		}
		r.wardenAt = time.Now()
		r.fleeing = true
		defer func() { r.fleeing = false }()
		plan("a warden %.0f blocks off: away from it, home", math.Sqrt(e.DistanceSqr(p.X, p.Y, p.Z)))
		if r.home != nil {
			if _, err := cmdIn(r, nil); err != nil {
				plan("away from the warden: %v", err)
			}
		}
		return true
	}
	return false
}
