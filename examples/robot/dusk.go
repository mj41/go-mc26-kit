package main

import (
	"errors"
	"github.com/mj41/go-mc26/level/block"
	"math"
	"time"
)

// A player watches the sun: with less light left than the way home takes,
// they turn for home — or, too far from it to be in before dark, they make a
// shelter where they are while they still can see. The robot reckons both:
// the light left from the time of day (dusk at tick 12000, twenty ticks a
// second), the way home from how far it is (three blocks a second over
// rough ground, and a margin: half the walk again, at least twenty seconds —
// a flat minute made a field twenty blocks from the door "too far" with half
// a minute of light left, and the robot walled itself in by it).

// nightWalk is the longest way home it takes after dark rather than walling
// itself in: about ninety blocks.
const nightWalk = 50 * time.Second

// errLate: out too late — the robot went home (or walled itself in) and
// the work is left for tomorrow.
var errLate = errors.New("too late in the day")

// lightLeft is the daylight left: seconds till dusk, none at dusk or night.
func (r *robot) lightLeft() time.Duration {
	t, ok := r.clock.TimeOfDay()
	if !ok {
		return time.Hour
	}
	switch {
	case t < 12000:
		return time.Duration(12000-t) * time.Second / 20
	case t >= 23000: // dawn: tomorrow's light
		return time.Duration(24000-t+12000) * time.Second / 20
	}
	return 0
}

// wayHome is how long the walk home would take, a margin with it; none
// without a home.
func (r *robot) wayHome() time.Duration {
	if r.home == nil {
		return 0
	}
	p := r.player.Position()
	f := r.home.front
	d := math.Hypot(float64(f.X)-p.X, float64(f.Z)-p.Z) + math.Abs(float64(f.Y)-p.Y)
	walk := time.Duration(d / 3 * float64(time.Second))
	return walk + max(walk/2, 20*time.Second)
}

// lateOut reports whether it is time to stop the outdoor work for the day:
// out under the sky, home known, less light left than the way home takes.
func (r *robot) lateOut() bool {
	if r.home == nil || r.hiding {
		return false
	}
	if r.night() {
		return r.underSky() // dark and out under the sky (a tree is no roof) (down the mine is the night's work)
	}
	return r.lightLeft() < r.wayHome()
}

// headHome ends the outdoor work at the turn of the day: home if it can be
// there before dark, else a shelter made where it is while there is light.
// It answers errLate (the work left).
func (r *robot) headHome() error {
	left, way := r.lightLeft(), r.wayHome()
	// home in time (the margin counted twice); after dark, home if it is a
	// short walk (nightWalk), else walled in where it is
	if r.distanceTo(r.home.centre()) < 16 || left > way/2 || r.night() && way < nightWalk { // at home already: in
		plan("dusk in %s, home %s away: home", left.Round(time.Second), way.Round(time.Second))
		if _, err := cmdIn(r, nil); err != nil {
			plan("home: %v", err)
		}
		return errLate
	}
	plan("dusk in %s, home %s away: too far, a shelter here", left.Round(time.Second), way.Round(time.Second))
	r.hiding = true
	defer func() { r.hiding = false }()
	if ans, err := cmdHide(r, nil); err != nil {
		plan("hide: %v", err)
	} else {
		plan("hide: %s", ans)
	}
	return errLate
}

// dayWork is the check before outdoor work (trees, a leg of the search):
// nothing out there after dark — out already, it heads home (or hides);
// in, it stays in. It answers errLate when the work is for tomorrow.
func (r *robot) dayWork() error {
	if r.home != nil && r.night() && !r.hiding {
		if r.underSky() {
			return r.headHome()
		}
		return errLate
	}
	if r.lateOut() {
		return r.headHome()
	}
	r.outFirst()
	return nil
}

// outFirst: in its room, the door shut, it goes out by the door before work
// outside (else every tree is out of reach, and a way round finds only its
// stairs down).
func (r *robot) outFirst() {
	if m := r.home; m != nil && !r.night() && r.distanceTo(m.centre()) < 5 { // by night the door stays shut
		if s, ok := r.world.BlockAt(m.cell(1, 0, 0)); ok && len(block.CollisionShape(s)) > 0 {
			if _, err := cmdOut(r, nil); err != nil {
				plan("out first: %v", err)
			}
		}
	}
}
