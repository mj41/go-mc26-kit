package trace_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/trace"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// A log dug by hand, seen as a trace: the block gone, the log's item entity added
// (holding a log), lying, picked up, the slot set — what the action changed, in
// order, at its ticks.
func TestTraceOfDiggingALog(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{View: 2, Setup: []string{"setblock 1 64 0 oak_log"}})
		r := trace.New(g.B.C, func() uint64 { return g.B.Ctl.State().Ticks })
		from := r.Mark()
		done := make(chan struct{}, 1)
		g.B.Hands.Dig(world.BlockPos{X: 1, Y: 64, Z: 0}, func(act.Dig) {})
		_ = done
		g.Until(200, func() bool { return g.Count("minecraft:oak_log") == 1 })
		g.Step(5)
		w := r.Since("dig a log", from)
		for _, c := range w.Changes {
			t.Logf("%+v", c)
		}
		var kinds []string
		for _, c := range w.Changes {
			kinds = append(kinds, c.Kind)
		}
		want := []string{"entity", "block", "pickup", "slot", "gone"} // (the bench's order: the item first, the block's update after; the pick-up, the slot, the item gone)
		if !slices.Equal(kinds, want) {
			t.Errorf("the changes %v, want %v", kinds, want)
		}
	})
}

// Compare finds what differs: the same changes in the same order within a slack of ticks; where
// an item entity came is not compared.
func TestCompare(t *testing.T) {
	at := &[3]int{1, 64, 0}
	want := []trace.Change{{Tick: 60, Kind: "block", At: at, Name: "minecraft:air"}, {Tick: 60, Kind: "entity", Name: "minecraft:item", Item: "minecraft:oak_log", Count: 1, At: &[3]int{1, 64, 0}}}
	got := []trace.Change{{Tick: 61, Kind: "block", At: at, Name: "minecraft:air"}, {Tick: 62, Kind: "entity", Name: "minecraft:item", Item: "minecraft:oak_log", Count: 1, At: &[3]int{2, 63, 0}}}
	if d := trace.Compare(want, got, 2); len(d) != 0 {
		t.Errorf("alike traces differ: %v", d)
	}
	got[1].Tick = 70
	if d := trace.Compare(want, got, 2); len(d) != 1 {
		t.Errorf("a change 10 ticks late: %v", d)
	}
	got[1].Item = "minecraft:birch_log"
	if d := trace.Compare(want, got, 2); len(d) != 1 {
		t.Errorf("another item: %v", d)
	}
	if d := trace.Compare(want, got[:1], 2); len(d) != 1 || d[0][:7] != "missing" {
		t.Errorf("a change missing: %v", d)
	}
}

func nameAt(g *simworld.Game) func(x, y, z int) (string, bool) {
	return func(x, y, z int) (string, bool) {
		s, ok := g.B.World.ServerBlockAt(world.BlockPos{X: x, Y: y, Z: z})
		if !ok {
			return "", false
		}
		return block.StateList[s].ID(), true
	}
}

// leavesState is a state of the oak leaves with the distance given.
func leavesState(distance int) block.StateID {
	for i := range block.StateList {
		if t := world.StateString(block.StateID(i)); strings.HasPrefix(t, "minecraft:oak_leaves[") && strings.Contains(t, fmt.Sprintf("distance=%d,", distance)) && strings.Contains(t, "persistent=true") && strings.Contains(t, "waterlogged=false") {
			return block.StateID(i)
		}
	}
	panic("no leaves state")
}

// A leaf whose distance moves (a tree being felled) is the same block: no change of the trace; a block that
// becomes another is. Far from the bot, or a thousand changes of one window, are kept out or cut and said.
func TestTraceKeepsOnlyChangesOfTheBlock(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{View: 3, Setup: []string{"setblock 2 64 0 minecraft:oak_leaves[persistent=true]"}})
		r := trace.New(g.B.C, func() uint64 { return g.B.Ctl.State().Ticks })
		r.Block = nameAt(g)
		r.Near = func() (float64, float64, float64) { p := g.B.Player.Position(); return p.X, p.Y, p.Z }
		r.Radius = 8
		from := r.Mark()
		// the leaf's distance moving, again and again
		for d := 1; d <= 7; d++ {
			if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: 2, Y: 64, Z: 0}, BlockState: pk.VarInt(leavesState(d))}); err != nil {
				t.Fatal(err)
			}
		}
		g.Step(3)
		if w := r.Since("leaves", from); len(w.Changes) > 1 { // (the first may differ from the plain state the setup made)
			t.Errorf("%d changes for a leaf whose name stays the same: %+v", len(w.Changes), w.Changes)
		}
		// a block far from the bot is not the action's
		from = r.Mark()
		g.S.World.Set(40, 64, 0, "minecraft:stone")
		if err := g.S.SetBlock(40, 64, 0, "minecraft:dirt"); err != nil {
			t.Fatal(err)
		}
		g.Step(3)
		if w := r.Since("far", from); len(w.Changes) != 0 {
			t.Errorf("a change 40 blocks off: %+v", w.Changes)
		}
		// a block that becomes another, near, is
		from = r.Mark()
		if err := g.S.SetBlock(3, 64, 0, "minecraft:stone"); err != nil {
			t.Fatal(err)
		}
		g.Step(3)
		if w := r.Since("near", from); len(w.Changes) != 1 || w.Changes[0].Name != "minecraft:stone" {
			t.Errorf("a stone placed: %+v", w.Changes)
		}
		// a window of far more than it keeps is cut, and the cut is in it
		from = r.Mark()
		for i := 0; i < 700; i++ {
			name := "minecraft:stone"
			if i%2 == 1 {
				name = "minecraft:dirt"
			}
			if err := g.S.SetBlock(4+i%3, 64, 0, name); err != nil {
				t.Fatal(err)
			}
		}
		g.Step(3)
		w := r.Since("many", from)
		if n := len(w.Changes); n > 513 || w.Changes[n-1].Kind != "truncated" {
			t.Errorf("%d changes, the last %+v: want at most 512 and a cut noted", n, w.Changes[n-1])
		}
		if b, _ := json.Marshal(w); len(b) > 90000 {
			t.Errorf("a window of %d bytes", len(b))
		}
	})
}

// A tree felled on the small game: the trace has the logs gone, the drops and the pick-ups, small.
func TestTraceOfAFelledTreeIsSmall(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{View: 3, Setup: []string{
			"fill 4 67 -2 8 70 2 minecraft:oak_leaves[persistent=true] replace minecraft:air", "fill 6 64 0 6 69 0 oak_log"}})
		r := trace.New(g.B.C, func() uint64 { return g.B.Ctl.State().Ticks })
		r.Block = nameAt(g) // (the block as the server last said: the client's own dig predicts air before it does)
		r.Near = func() (float64, float64, float64) { p := g.B.Player.Position(); return p.X, p.Y, p.Z }
		r.Radius = 8
		from := r.Mark()
		for y := 64; y <= 69; y++ {
			done := make(chan struct{}, 1)
			g.B.Hands.Dig(world.BlockPos{X: 6, Y: y, Z: 0}, func(act.Dig) { done <- struct{}{} })
			g.Until(400, func() bool {
				select {
				case <-done:
					return true
				default:
					return false
				}
			})
		}
		g.Step(60)
		w := r.Since("fell", from)
		b, _ := json.Marshal(w)
		if len(b) > 5000 {
			t.Errorf("the trace of a felled tree is %d bytes (%d changes), want under 5 KB", len(b), len(w.Changes))
		}
		var logs int
		for _, c := range w.Changes {
			if c.Kind == "block" && c.Name == "minecraft:air" {
				logs++
			}
		}
		if logs != 6 {
			t.Errorf("%d logs gone in the trace, want 6", logs)
		}
	})
}
