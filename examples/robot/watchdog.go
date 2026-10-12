package main

import (
	"flag"
	"log"
	"math"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// The stuck watchdog: an order running while the robot has not gone a block
// for stuckAfter — and is not waiting by choice (furnaces at work, hidden,
// a command that waits) — is stuck. The watchdog stops it; the runner digs
// or climbs out toward where the order was walking to, and runs the order
// once again. The mc26 harness's stop at 180 s is then the last resort.

var watchdogOn = flag.Bool("watchdog", true, "stop an order still for 30 s (not waiting by choice), get out toward its goal and run it once again")

// stuckAfter is how long still with an order running is stuck.
const stuckAfter = 30 * time.Second

// waitingCommands are the commands whose standing still is the command.
var waitingCommands = map[string]bool{
	"wait": true, "rest": true, "fish": true, "sleep": true, "follow": true, "guard": true,
}

// watchdog is the loop: once a second, the robot's place against the order's.
func (r *robot) watchdog() {
	if !*watchdogOn {
		return
	}
	var at struct{ x, y, z float64 }
	var done int64 // its digs, placings, crafts: work done, not stuck
	since := time.Now()
	fired := false
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(time.Second):
		}
		p := r.player.Position()
		line := r.doing()
		cmd, _, _ := strings.Cut(line, " ")
		if !r.ordering.Load() || r.waiting.Load() || waitingCommands[cmd] || r.gate.isPaused() ||
			math.Abs(p.X-at.x)+math.Abs(p.Y-at.y)+math.Abs(p.Z-at.z) >= 1 || r.progress.Load() != done {
			at.x, at.y, at.z = p.X, p.Y, p.Z
			done = r.progress.Load()
			since, fired = time.Now(), false
			continue
		}
		if fired || time.Since(since) < stuckAfter {
			continue
		}
		fired = true
		log.Printf("robot: stuck: still %.0fs at %.1f %.1f %.1f doing %q: the order stopped, out, again", time.Since(since).Seconds(), p.X, p.Y, p.Z, line)
		events.emit("stuck", map[string]any{"doing": line, "x": round2(p.X), "y": round2(p.Y), "z": round2(p.Z)})
		r.stuckStop.Store(true)
		r.stops.Add(1)
		r.walker.Stop()
		r.gate.resume()
	}
}

// unstick gets it out of where it stood stuck: dug out toward where it was
// walking to (a step), then climbed out toward it when no path leads there.
func (r *robot) unstick() {
	goal, ok := r.lastGoal, r.hasGoal
	if !ok {
		return
	}
	plan("stuck: out toward %v", goal)
	if err := r.digOut(goal); err != nil {
		plan("stuck: dig out: %v", err)
	}
	p := r.player.Position()
	start, ok := r.walker.Graph.Start(p.X, p.Y, p.Z)
	if !ok {
		return
	}
	if _, err := r.walker.Graph.Find(start, goal, 100000); err != nil {
		if err := r.climbOut(goal); err != nil {
			plan("stuck: climb out: %v", err)
		}
	}
}

// noteGoal remembers where a walk goes (the watchdog's way out).
func (r *robot) noteGoal(g world.BlockPos) { r.lastGoal, r.hasGoal = g, true }
