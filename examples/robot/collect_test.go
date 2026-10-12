package main

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// A walk that ended with no way found (every place reachable searched) and an item out of the pickup's reach:
// the wait for the server to hand it over is pointless (the survival run of 2026-10-11 waited it for each of
// ten items in pockets of its own mining, then dug its way in).
func TestHandoverHopeless(t *testing.T) {
	for _, c := range []struct {
		name       string
		noWay      bool
		dx, dy, dz float64
		hopeless   bool
	}{
		{"a way was found, the item far", false, 5, 0, 0, false},
		{"no way, the item out of reach", true, 1.9, 0, 0, true},
		{"no way, the item above reach", true, 0.2, 1.4, 0.2, true},
		{"no way, the item at the feet", true, 0.4, 0, 0.3, false},
		{"no way, the item just in reach", true, 1.2, 0.3, 0.5, false},
	} {
		if got := handoverHopeless(c.noWay, c.dx, c.dy, c.dz); got != c.hopeless {
			t.Errorf("%s: %v, want %v", c.name, got, c.hopeless)
		}
	}
}

// An item in a pocket of stone the walker cannot enter: collect ends having dug its way in and picked it up.
func TestCollectFromAPocket(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		r, g := simRobot(t, simworld.Options{View: 4, Setup: []string{
			"fill 2 64 -2 5 67 2 minecraft:stone", "setblock 3 64 0 minecraft:air", "setblock 3 65 0 minecraft:air"}})
		if err := g.Drop("minecraft:dirt", 1, 3, 64, 0); err != nil {
			t.Fatal(err)
		}
		g.Step(20)
		began := g.Steps.Now()
		ans, err, ok := run(t, g, 20*60, func() (string, error) { return cmdCollect(r, []string{"8"}) })
		t.Logf("collect: %q err %v ended %v; items left %d; %v of game time", ans, err, ok, g.Items(), g.Steps.Now().Sub(began))
		if !ok || err != nil || ans != "1" {
			t.Fatalf("the item was not collected: %q %v (ended %v)", ans, err, ok)
		}
	})
}
