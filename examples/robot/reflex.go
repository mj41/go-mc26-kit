package main

import (
	"flag"

	"github.com/mj41/go-mc26-kit/bot/control"
	"log"
	"time"
)

// The reflexes: safety first, from whatever the robot is doing, and when it
// is doing nothing. Four times a second the danger signals are checked; on
// one the running order is paused at its next safe point (tasks.go), the
// danger dealt with — defend: out of a wall, away from lava, out of a
// stream, home or hidden from the night, away from a warden, a creeper
// left, a fight, walled in from an archer; then something eaten, a rest when
// badly hurt — and the order let go on. With no order running the round
// runs alone, and the runner waits for it before the next order. An order
// between two safe points for longer than the pause waits (a dig, a
// furnace's wait) keeps its own checks, as before.

var reflexesOn = flag.Bool("reflexes", true, "check for danger four times a second and deal with it, an order running or not (off: only the commands' own checks)")

// reflexEvery is how often the signals are checked.
const reflexEvery = 250 * time.Millisecond

// reflexCooldown is how long a danger dealt with waits before its next round.
func reflexCooldown(why string) time.Duration {
	switch why {
	case "under water":
		return 500 * time.Millisecond
	case "hurt":
		return 2 * time.Second
	case "hungry", "badly hurt":
		return 30 * time.Second
	}
	return 5 * time.Second
}

// reflexes is the loop.
func (r *robot) reflexes() {
	if !*reflexesOn {
		return
	}
	r.reflexNext = map[string]time.Time{}
	t := time.NewTicker(reflexEvery)
	defer t.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-t.C:
		}
		why := r.danger()
		if why == "" || time.Now().Before(r.reflexNext[why]) {
			continue
		}
		// a danger that stays (hungry with nothing to eat, a mob it cannot
		// reach): not every quarter second — the order would never go on
		r.reflexNext[why] = time.Now().Add(reflexCooldown(why))
		if r.ordering.Load() {
			// the order held at its next safe point first: two never drive
			// the walker and the hands at once
			// an urgent danger waits longer for the order's next safe point: a
			// dig by hand takes three seconds, a pause given up at two was
			// never met (a zombie killed it felling a tree, hit after hit,
			// every pause timed out; the dig gives way to a hit at once)
			wait := 2 * time.Second
			switch why {
			case "hurt", "a mob close", "a creeper close", "a warden near", "under water":
				wait = 8 * time.Second
			}
			if !r.pauseOrder(wait) {
				continue
			}
			r.reflexRound(why)
			r.resumeOrder()
			continue
		}
		r.reflexMu.Lock()
		if !r.ordering.Load() {
			r.reflexRound(why)
		}
		r.reflexMu.Unlock()
	}
}

// reflexRound deals with the danger: defend, then food, then a rest when it
// is badly hurt and nothing is about.
func (r *robot) reflexRound(why string) {
	r.inReflex = true
	defer func() { r.inReflex, r.hideReflex = false, false }()
	log.Printf("robot: reflex: %s", why)
	events.emit("reflex", map[string]any{"why": why})
	if why == "under water" {
		r.swimUp()
		return // nothing else under water: up first, the rest next round
	}
	if why == "night far from home" {
		r.hideReflex = true
		r.hideIfNight()
		r.hideReflex = false
	}
	r.defend()
	r.eatIfHungry()
	if r.player.Status().Health < 8 && !r.hostileNear(12) {
		r.rest(14)
	}
}

// danger is the first danger signal on, worst first ("" none): hurt just
// now, a mob or a creeper close, its eyes in a block, lava beside it, in
// moving water, a warden near, badly hurt, hungry.
func (r *robot) danger() string {
	st := r.player.Status()
	if st.Health <= 0 {
		return "" // dead: the respawn is the death's
	}
	sig := r.sigs() // the surroundings watch's (watch.go)
	// under water first: its air runs out, then two hearts a second (it once
	// sank down a flooded hole it dug, the reflexes resting it as it drowned)
	if sig.underWater {
		return "under water"
	}
	if time.Since(time.UnixMilli(r.lastHurt.Load())) < 5*time.Second {
		return "hurt"
	}
	switch {
	case sig.wardenNear:
		return "a warden near"
	case sig.creeperClose:
		return "a creeper close"
	case sig.mobClose:
		return "a mob close"
	case sig.inWall:
		return "in a wall"
	case sig.byLava:
		return "lava beside"
	case sig.inStream:
		return "in a stream"
	}
	// living by itself, nothing to do and out at night far from home (the day
	// loop's work failed out there): home, or walled in till morning
	if *daysOn && !r.ordering.Load() && r.night() && r.home != nil && r.distanceTo(r.home.centre()) > 48 && r.underSky() {
		return "night far from home"
	}
	if st.Health < 8 {
		return "badly hurt"
	}
	if r.food.Load() <= 14 {
		return "hungry"
	}
	return ""
}

// swimUp holds the jump key, as a person swims up, till its eyes are out of
// the water (eight seconds at most); the walk stopped first (it would steer).
func (r *robot) swimUp() {
	r.walker.Stop()
	r.ctl.SetInput(control.Input{Jump: true})
	defer r.ctl.SetInput(control.Input{})
	for t := 0; t < 160 && r.sigs().underWater; t++ {
		if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
			return
		}
	}
}
