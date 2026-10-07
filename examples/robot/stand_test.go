package main

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level"
)

// TestStandCell: the cell it stands in is the one whose block holds it up —
// at a step's edge over a cave, its middle over the void, the step's cell.
func TestStandCell(t *testing.T) {
	w := &world.World{Columns: map[level.ChunkPos]*level.Chunk{{0, 0}: level.EmptyChunk(24)}}
	stone := stateOf(t, "minecraft:stone")
	w.SetBlock(world.BlockPos{X: 5, Y: 9, Z: 6}, stone) // a step, the void round it
	r := &robot{world: w, player: basic.NewPlayer(bot.NewClient(), basic.DefaultSettings, basic.EventsListener{})}
	for _, c := range []struct {
		x, z float64
		want world.BlockPos
	}{
		{5.5, 6.5, world.BlockPos{X: 5, Y: 10, Z: 6}},  // in its middle
		{5.5, 5.71, world.BlockPos{X: 5, Y: 10, Z: 6}}, // its middle over the void at z 5, a corner on the step
		{4.75, 6.5, world.BlockPos{X: 5, Y: 10, Z: 6}}, // over the void at x 4
		{5.5, 5.5, world.BlockPos{X: 5, Y: 10, Z: 5}},  // nothing under it at all: its middle's
	} {
		r.player.SetPosition(basic.Position{X: c.x, Y: 10, Z: c.z})
		if got := r.standCell(); got != c.want {
			t.Errorf("at %.2f 10 %.2f: %v, want %v", c.x, c.z, got, c.want)
		}
	}
}
