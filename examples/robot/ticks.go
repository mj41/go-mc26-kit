package main

import (
	"math"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
)

// The tick event (EVENTS.md "tick"): at the end of each client tick that
// brought something a watcher draws — its own move or turn, the others'
// moves (the path each packet gives, with its ticks), arrivals and
// departures, swings, hurts, deaths, pick-ups, explosions, velocities — one
// line, so a replay moves everything as the game's client does, tick by
// tick. Within 32 blocks of it (the view's reach).

// tickWas is what the last tick event told of itself (only changes are told)
var tickWas struct {
	me     [5]float64
	swings int64
}

// tickEvent is the controller's Ticked: called locked, at each tick's end
func (r *robot) tickEvent(s *control.State, p *basic.Player) {
	if events == nil || r.ticks == nil {
		return
	}
	pos := p.Position()
	if r.ticks.Near == nil {
		r.ticks.Near = func(x, y, z float64) bool {
			q := r.player.Position()
			return math.Abs(x-q.X) <= viewSize && math.Abs(y-q.Y) <= viewSize && math.Abs(z-q.Z) <= viewSize
		}
	}
	// its own swing: the server does not send a client its own
	if n := r.hands.Swings(); n != tickWas.swings {
		tickWas.swings = n
		r.ticks.Swung(int32(p.Login.PlayerID))
	}
	t := r.ticks.Take()
	e := map[string]any{"t": s.Ticks}
	me := [5]float64{round2(pos.X), round2(pos.Y), round2(pos.Z), math.Round(float64(pos.Yaw)*10) / 10, math.Round(float64(pos.Pitch)*10) / 10}
	if me != tickWas.me {
		tickWas.me = me
		e["me"] = me
	}
	if len(t.Ents) > 0 {
		e["ents"] = t.Ents
	}
	if len(t.New) > 0 {
		var news []map[string]any
		for _, n := range t.New {
			news = append(news, r.entityEntry(n))
		}
		e["new"] = news
	}
	for k, v := range map[string][]int32{"gone": t.Gone, "swing": t.Swing, "hurt": t.Hurt, "died": t.Died} {
		if len(v) > 0 {
			e[k] = v
		}
	}
	if len(t.Pickup) > 0 {
		e["pickup"] = t.Pickup
	}
	if len(t.Explode) > 0 {
		e["explode"] = t.Explode
	}
	if len(t.Motion) > 0 {
		e["motion"] = t.Motion
	}
	if len(e) == 1 { // nothing but its count: no line
		return
	}
	events.emit("tick", e)
}
