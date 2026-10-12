package main

import (
	"context"
	"errors"
	"testing"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// simRobot joins a robot (as main assembles it, newRobot) to a game of simworld on its stepped clock, its order
// goroutines not started: a test gives it commands (cmdGoto …) and steps the game till they end.
func simRobot(t *testing.T, o simworld.Options) (*robot, *simworld.Game) {
	t.Helper()
	g := simworld.NewServer(t, o)
	r := newRobot("Robot")
	r.player.KeepAliveTimeout = -1
	r.ctl.Clock = g.Steps
	ctx, cancel := contextForTest(t)
	r.ctx = ctx
	if err := g.Join(r.client); err != nil { // over the game's network: memory in a bubble
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = r.client.Close() })
	go func() {
		for ctx.Err() == nil {
			var handlerErr bot.PacketHandlerError
			if err := r.client.HandleGame(); err != nil && !errors.As(err, &handlerErr) {
				return
			}
		}
	}()
	go func() { _ = r.ctl.Run(ctx) }()
	g.Attach(func() bool { return r.ctl.State().Loaded })
	return r, g
}

// run starts a command of the robot and steps the game till it ends (at most ticks): its answer and error.
func run(t *testing.T, g *simworld.Game, ticks int, f func() (string, error)) (string, error, bool) {
	t.Helper()
	type res struct {
		s   string
		err error
	}
	done := make(chan res, 1)
	go func() { s, err := f(); done <- res{s, err} }()
	var out res
	ok := g.Until(ticks, func() bool {
		select {
		case out = <-done:
			return true
		default:
			return false
		}
	})
	return out.s, out.err, ok
}

// A goto from the last block of a pillar ten over the ground, to a place at its foot: no way is planned (a drop of
// ten is not a walk), the stairs dug down would be a bridge over open air, and the robot drops off the edge at last
// (dropOff) — what the days run of 2026-10-11 could not do, standing on a cliff's lone block until it ended.
func TestGotoFromAPillarDropsOff(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		r, g := simRobot(t, simworld.Options{View: 4, Setup: []string{"fill 10 64 0 10 73 0 minecraft:stone"}})
		g.Teleport(10.5, 74, 0.5)
		if !g.Until(40, func() bool { return r.ctl.State().OnGround && r.player.Position().Y > 73.9 }) {
			t.Fatalf("not on the pillar: %+v", r.player.Position())
		}
		ans, err, ok := run(t, g, 20*120, func() (string, error) { return cmdGoto(r, []string{"14", "64", "0"}) })
		p := r.player.Position()
		t.Logf("goto: %q err %v ended %v; at %.1f %.1f %.1f health %v", ans, err, ok, p.X, p.Y, p.Z, r.player.Status().Health)
		if !ok || err != nil {
			t.Fatalf("the goto did not end well: %v (ended %v)", err, ok)
		}
		if p.Y > 65 {
			t.Errorf("still up at y %.1f", p.Y)
		}
	})
}

func contextForTest(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}

// An iron ore seen in a pocket of air, rock between it and the chamber the robot stands in under the ground: it digs
// a tunnel to it (digGoal's tunnelToward) and has the ore's drop.
func TestGetIronThroughTheRock(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		r, g := simRobot(t, simworld.Options{View: 4, Setup: []string{
			"fill -6 36 -6 26 62 6 minecraft:stone", "fill -2 40 -1 0 42 1 minecraft:air", "fill 7 40 -1 8 42 1 minecraft:air", "setblock 9 41 0 minecraft:iron_ore",
			"give Robot minecraft:stone_pickaxe",
		}})
		g.Teleport(-0.5, 40, 0.5)
		if !g.Until(60, func() bool { return r.ctl.State().OnGround && r.player.Position().Y < 41 }) {
			t.Fatalf("not in the chamber: %+v", r.player.Position())
		}
		ans, err, ok := run(t, g, 20*240, func() (string, error) { return cmdGet(r, []string{"raw_iron", "1"}) })
		t.Logf("get: %q err %v ended %v; raw_iron %d", ans, err, ok, r.ui.Count("minecraft:raw_iron"))
		if !ok || err != nil || r.ui.Count("minecraft:raw_iron") < 1 {
			t.Fatalf("no iron: %v (ended %v)", err, ok)
		}
	})
}
