package world

import (
	"sync"
	"testing"

	"bytes"
	"github.com/mj41/go-mc26/level"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

type told struct {
	mu sync.Mutex
	l  []string
}

func (t *told) add(pos BlockPos, s block.StateID) {
	t.mu.Lock()
	t.l = append(t.l, StateString(s))
	t.mu.Unlock()
}

func (t *told) all() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.l...)
}

func ack(t *testing.T, w *World, seq int32) {
	t.Helper()
	var buf bytes.Buffer
	a := play.BlockChangedAck{Sequence: pk.VarInt(seq)}
	if _, err := a.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if err := w.handleBlockChangedAck(pk.Packet{ID: int32(a.PacketID()), Data: buf.Bytes()}); err != nil {
		t.Fatal(err)
	}
}

// A client's own dig is a change of the world: the listener (the events' `changes`) is told at the prediction,
// and the ack that finds the world as the server says tells nothing more.
func TestPredictIsTold(t *testing.T) {
	var got told
	w := &World{Columns: map[level.ChunkPos]*level.Chunk{}, minY: -64, events: EventsListener{BlockChange: got.add}}
	w.Columns[level.ChunkPos{0, 0}] = level.EmptyChunk(24)
	pos := BlockPos{3, 10, 4}
	stone := block.ToStateID[block.Stone{}]
	air := block.StateID(0)
	w.SetBlock(pos, stone)

	if !w.Predict(pos, air, 7) {
		t.Fatal("not predicted")
	}
	if l := got.all(); len(l) != 1 || l[0] != "minecraft:air" {
		t.Fatalf("told at the prediction: %v", l)
	}
	// the server's own update for it (air) is kept for the ack: nothing told
	w.mu.Lock()
	changed := w.serverBlock(pos, air)
	w.mu.Unlock()
	if changed {
		t.Error("the server's update changed the world under a prediction")
	}
	ack(t, w, 7)
	if l := got.all(); len(l) != 1 {
		t.Errorf("the ack told again though the world was as the server says: %v", l)
	}
	// predicting what the world has is no change
	w.Predict(pos, air, 8)
	if l := got.all(); len(l) != 1 {
		t.Errorf("a prediction of what is there was told: %v", l)
	}
}

// A prediction the server did not take: told at the prediction, and again, the server's block, at the ack.
func TestPredictUndoneIsToldAgain(t *testing.T) {
	var got told
	w := &World{Columns: map[level.ChunkPos]*level.Chunk{}, minY: -64, events: EventsListener{BlockChange: got.add}}
	w.Columns[level.ChunkPos{0, 0}] = level.EmptyChunk(24)
	pos := BlockPos{3, 10, 4}
	w.SetBlock(pos, block.ToStateID[block.Stone{}])
	w.Predict(pos, 0, 3)
	ack(t, w, 3) // no block update: the server kept the stone
	if l := got.all(); len(l) != 2 || l[0] != "minecraft:air" || l[1] != "minecraft:stone" {
		t.Errorf("told: %v", l)
	}
}
