package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// The robot's own dig is a change of the world a reader must see: the events' `changes` held the server's block
// updates only, and the dig's break — predicted at once, the server's update kept for the ack, the ack finding
// the world as the server says — never reached it: a replay (the runs viewer's 3D, a decoded run) showed a felled
// log standing for ever (survival run 20261011-211933, "collect … no way to the goal").
func TestOwnDigIsInTheChanges(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		r, g := simRobot(t, simworld.Options{View: 3, Setup: []string{"setblock 2 64 0 minecraft:dirt"}})
		path := filepath.Join(t.TempDir(), "events.jsonl")
		l, err := openEvents(path, "test")
		if err != nil {
			t.Fatal(err)
		}
		events = l
		t.Cleanup(func() { events = nil })
		go r.reportChanges()
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		g.Step(20)
		ans, err, ok := run(t, g, 20*30, func() (string, error) { return cmdDig(r, []string{"2", "64", "0"}) })
		if !ok || err != nil {
			t.Fatalf("the dig did not end well: %q %v (ended %v)", ans, err, ok)
		}
		g.Step(40)
		time.Sleep(200 * time.Millisecond) // (the changes are told each 50 ms: on the bubble's clock)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var air bool
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, `"name":"changes"`) && strings.Contains(line, `"name":"minecraft:air"`) &&
				strings.Contains(line, `"x":2,"y":64,"z":0`) {
				air = true
			}
		}
		if !air {
			t.Errorf("no changes event with air at 2 64 0 after the robot dug it:\n%s", b)
		}
	})
}
