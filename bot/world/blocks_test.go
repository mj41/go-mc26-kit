package world

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mj41/go-mc26/level"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

func TestStateString(t *testing.T) {
	if got := StateString(block.ToStateID[block.Stone{}]); got != "minecraft:stone" {
		t.Errorf("stone: %q", got)
	}
	stairs := block.OakStairs{Facing: block.DirectionEast, Half: block.HalfTop, Shape: block.StairsShapeStraight}
	got := StateString(block.ToStateID[stairs])
	for _, want := range []string{"minecraft:oak_stairs[", "facing=east", "half=top", "shape=straight", "waterlogged=false"} {
		if !strings.Contains(got, want) {
			t.Errorf("oak stairs: %q lacks %q", got, want)
		}
	}
}

// TestSectionBlocksUpdate writes a section update the way the server packs it
// (SectionPos.asLong, state << 12 | x << 8 | z << 4 | y) for blocks in a
// section at negative coordinates, and checks each lands where it was meant to.
func TestSectionBlocksUpdate(t *testing.T) {
	w := &World{Columns: map[level.ChunkPos]*level.Chunk{}, minY: -64}
	w.Columns[level.ChunkPos{-1, 2}] = level.EmptyChunk(24)
	sx, sy, sz := int64(-1), int64(-4), int64(2) // blocks x -16..-1, y -64..-49, z 32..47
	sec := (sx&0x3fffff)<<42 | (sz&0x3fffff)<<20 | sy&0xfffff
	stone := int64(block.ToStateID[block.Stone{}])
	dirt := int64(block.ToStateID[block.Dirt{}])
	u := play.SectionBlocksUpdate{SectionPos: pk.Long(sec)}
	u.States = append(u.States, pk.VarLong(stone<<12|15<<8|0<<4|3), pk.VarLong(dirt<<12|0<<8|15<<4|0))
	var buf bytes.Buffer
	if _, err := u.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if err := w.handleSectionBlocksUpdate(pk.Packet{ID: int32(u.PacketID()), Data: buf.Bytes()}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		pos  BlockPos
		want block.StateID
	}{{BlockPos{-1, -61, 32}, block.StateID(stone)}, {BlockPos{-16, -64, 47}, block.StateID(dirt)}, {BlockPos{-16, -64, 32}, 0}} {
		got, ok := w.BlockAt(c.pos)
		if !ok || got != c.want {
			t.Errorf("%v: %v %v, want %v", c.pos, StateString(got), ok, StateString(c.want))
		}
	}
	if _, ok := w.BlockAt(BlockPos{0, -61, 32}); ok {
		t.Errorf("a block of an unloaded chunk reads as loaded")
	}
}
