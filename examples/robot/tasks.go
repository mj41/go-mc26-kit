package main

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// The orders, one at a time: every order — from the console, from a player
// in chat, and from the robot's own loops — goes into one queue; one runner
// takes them in turn, runs the command and answers where the order came
// from. Looks (concurrentOK) and stop run at once, beside the order running.
// The running order ends at its next safe point when it is stopped or the
// robot dies, and it can be paused there and let go on (the reflex loop:
// a danger dealt with, then the order on).

// source is where an order came from, and so where its answer goes.
type source struct {
	kind string // console, chat, whisper (the robot's own loops later)
	who  string // the player, for chat and whisper
}

func (s source) String() string {
	if s.who == "" {
		return s.kind
	}
	return s.kind + ":" + s.who
}

// task is an order queued: its words, where it came from, what runs it and
// what is told the answer.
type task struct {
	words []string
	src   source
	cmd   func(*robot, []string) (string, error)
	reply func(answer string, err error)
}

// errStopped ends an order stopped (stop) at its next safe point.
var errStopped = errors.New("stopped")

// errDied ends an order the robot died in: it respawned with nothing, far
// from its work — what it was doing does not go on from there (a person
// starts again: recover, home).
var errDied = errors.New("died")

// concurrentOK are the commands that run beside the order running: they
// only look, or stop it.
var concurrentOK = map[string]bool{
	"pos": true, "state": true, "status": true, "inv": true, "time": true, "seen": true,
	"nearby": true, "entities": true, "around": true, "light": true, "block": true, "stop": true,
}

// queueLen is how many orders may wait behind the running one.
const queueLen = 16

// order gives an order: a look (or stop) run at once, anything else queued
// for the runner; reply is told the answer either way. wait: the queue full,
// it waits for room (the console) rather than refusing (chat).
func (r *robot) order(words []string, src source, cmd func(*robot, []string) (string, error), reply func(string, error), wait bool) {
	if concurrentOK[words[0]] {
		done := r.looking(strings.Join(words, " "), src)
		answer, err := cmd(r, words[1:])
		done(answer, err)
		reply(answer, err)
		return
	}
	t := task{words: words, src: src, cmd: cmd, reply: reply}
	if wait {
		select {
		case r.queue <- t:
		case <-r.ctx.Done():
		}
		return
	}
	select {
	case r.queue <- t:
	default:
		reply("", fmt.Errorf("busy: %d orders waiting", queueLen))
	}
}

// runOrders is the runner: the queue's orders one after another.
func (r *robot) runOrders() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case t := <-r.queue:
			r.reflexMu.Lock() // a reflex round running with no order: done first
			r.reflexMu.Unlock()
			r.orderDeaths.Store(r.deaths.Load())
			r.orderStops.Store(r.stops.Load())
			r.ordering.Store(true)
			done := r.running(strings.Join(t.words, " "), t.src)
			r.retool(t.words[0])
			answer, err := t.cmd(r, t.words[1:])
			// stuck (the watchdog stopped it): out toward where it was going,
			// then the order once again
			if stuck := r.stuckStop.Swap(false); stuck {
				// told either way: a stuck order is not retried when it ended
				// by itself meanwhile (an answer, another error)
				log.Printf("robot: stuck %q ended: %v (retried: %v)", strings.Join(t.words, " "), err, errors.Is(err, errStopped))
				if errors.Is(err, errStopped) {
					r.orderStops.Store(r.stops.Load())
					r.unstick()
					answer, err = t.cmd(r, t.words[1:])
				}
			}
			r.stuckStop.Store(false)
			// dead on the way, though the command answered (its loop met no
			// safe point after the respawn — a descend answered from the
			// world's spawn): the order ended in a death
			if err == nil && r.deaths.Load() != r.orderDeaths.Load() {
				err = fmt.Errorf("%w during the order (it answered %q)", errDied, answer)
				answer = ""
			}
			if errors.Is(err, errLate) { // stopped for the dark on purpose: home (or walled in), not a failure
				answer, err = "stopped: "+err.Error(), nil
			}
			r.ordering.Store(false)
			r.gate.release() // a pause asked as it ended waits for none
			done(answer, err)
			t.reply(answer, err)
		}
	}
}

// retool makes new tools after a death took them, by day, before the next
// order that works (not the run back for its things, nor home): the stone
// pickaxe, sword and axe, as a person respawned with nothing does first (it
// once went on scouting and cutting logs with bare hands after a death, and
// the night found it with no pickaxe: a night in, no iron pickaxe by the
// fourth morning). Once a death.
func (r *robot) retool(cmd string) {
	switch cmd {
	case "recover", "in", "wait", "time", "smeltall", "live":
		return
	}
	d := r.deaths.Load()
	if d == r.retooled || r.night() || r.countAll(tagItems("minecraft:pickaxes")) > 0 {
		return
	}
	r.retooled = d
	plan("no pickaxe (lost with a death): the stone tools first")
	for _, it := range []string{"minecraft:stone_pickaxe", "minecraft:stone_sword", "minecraft:stone_axe"} {
		if err := r.get(it, 1, 0); err != nil {
			plan("retool: %s: %v", it, err)
			if errors.Is(err, errLate) || errors.Is(err, errStopped) || errors.Is(err, errDied) {
				return
			}
		}
	}
}

// orderOver is why the running order ends now: the robot died or the order
// was stopped since it began; nil while it goes on.
func (r *robot) orderOver() error {
	if r.deaths.Load() != r.orderDeaths.Load() {
		p := r.player.Position()
		return fmt.Errorf("%w during the order (respawned at %.0f %.0f %.0f)", errDied, p.X, p.Y, p.Z)
	}
	if r.stops.Load() != r.orderStops.Load() {
		return fmt.Errorf("%w", errStopped)
	}
	return nil
}

// safePoint is where an order may wait or end, between its steps: held
// there while it is paused, then why it ends (orderOver). It reports whether
// it was held (a walk then sets out again: what the pause did moved it).
func (r *robot) safePoint() (bool, error) {
	held := r.gate.point()
	return held, r.orderOver()
}

// gate holds the running order at its next safe point while paused.
type gate struct {
	mu      sync.Mutex
	c       *sync.Cond
	paused  bool
	atPoint bool // the order is held at a safe point now
}

func (g *gate) cond() *sync.Cond {
	if g.c == nil {
		g.c = sync.NewCond(&g.mu)
	}
	return g.c
}

// point holds the caller while the gate is paused; whether it was held.
func (g *gate) point() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.atPoint {
		return false // the order is held here already: this is the round that paused it
	}
	held := false
	for g.paused {
		held = true
		g.atPoint = true
		g.cond().Broadcast()
		g.cond().Wait()
	}
	g.atPoint = false
	return held
}

// pause asks the running order to stop at its next safe point and waits up
// to d for it to be there; whether it is (false: the order is between safe
// points still — the pause stands, the order stops at its next one).
func (g *gate) pause(d time.Duration) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.paused = true
	stop := time.AfterFunc(d, func() { g.mu.Lock(); g.cond().Broadcast(); g.mu.Unlock() })
	defer stop.Stop()
	for end := time.Now().Add(d); !g.atPoint && time.Now().Before(end); {
		g.cond().Wait()
	}
	return g.atPoint
}

// resume lets the paused order go on.
func (g *gate) resume() {
	g.mu.Lock()
	g.paused = false
	g.cond().Broadcast()
	g.mu.Unlock()
}

// isPaused reports whether a pause holds (or waits for) the order: a reflex
// round's time, not the order's.
func (g *gate) isPaused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}

// release ends a pause no order waits at (the order ended).
func (g *gate) release() {
	g.mu.Lock()
	if !g.atPoint {
		g.paused = false
		g.cond().Broadcast()
	}
	g.mu.Unlock()
}

// pauseOrder pauses the running order at its next safe point (false: none
// runs, or it did not get there within d — nothing to resume then); resume
// with resumeOrder.
func (r *robot) pauseOrder(d time.Duration) bool {
	if !r.ordering.Load() {
		return false
	}
	if r.gate.pause(d) {
		return true
	}
	r.gate.resume()
	return false
}

// resumeOrder lets the order paused by pauseOrder go on.
func (r *robot) resumeOrder() { r.gate.resume() }
