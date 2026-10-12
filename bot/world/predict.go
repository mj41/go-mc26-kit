package world

import (
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// The client's predictions of its own block changes, as the game's client
// keeps them (BlockStatePredictionHandler): a block the client changes before
// the server has said (a dig's break) keeps the server's state beside it, with
// the action's sequence; a block update the server sends for it meanwhile is
// that state, not put in the world yet; and the server's BlockChangedAck for
// the sequence puts the server's state back in the world. A prediction the
// server did not take — a dig it did not count done (its ticks fewer than
// the client's), a break it found no instant one — answered with the ack and
// no block update, is undone there; without it the client's guess stayed for
// good: air in the bot's world, a block in the server's, the bot pressed
// against a face its path finder saw open.

// prediction is a predicted block's server state and the sequence of the
// last action that predicted it.
type prediction struct {
	seq   int32
	state block.StateID
}

// Predict changes the block at pos to state as the client's action with
// sequence seq predicts it, the server's state kept till the sequence's ack.
// The listener is told (BlockChange) when the world changed: the client's own
// dig is a change of the world as a server's block update is, and without it a
// reader following the changes (the events' replay) sees the felled log standing —
// the server's update for it is kept for the ack and the ack finds the world
// already as the server says. It reports false when the chunk is not loaded.
func (w *World) Predict(pos BlockPos, state block.StateID, seq int32) bool {
	w.mu.Lock()
	sec, i, ok := w.section(pos)
	if !ok {
		w.mu.Unlock()
		return false
	}
	was := block.StateID(sec.GetBlock(i))
	if p, ok := w.predicted[pos]; ok { // predicted again: the server's state is still the first's
		p.seq = seq
		w.predicted[pos] = p
	} else {
		if w.predicted == nil {
			w.predicted = map[BlockPos]prediction{}
		}
		w.predicted[pos] = prediction{seq: seq, state: was}
	}
	w.setBlock(pos, state)
	w.mu.Unlock()
	if was != state && w.events.BlockChange != nil {
		w.events.BlockChange(pos, state) // with the world free
	}
	return true
}

// serverBlock is a block the server says: kept for a predicted block (its
// ack puts it in), else put in the world. It reports whether the world
// changed; the caller holds w.mu.
func (w *World) serverBlock(pos BlockPos, state block.StateID) bool {
	if p, ok := w.predicted[pos]; ok {
		p.state = state
		w.predicted[pos] = p
		return false
	}
	return w.setBlock(pos, state)
}

// handleBlockChangedAck ends the predictions up to the sequence: each of
// their blocks set to the server's state (ClientLevel.handleBlockChangedAck,
// BlockStatePredictionHandler.endPredictionsUpTo).
func (w *World) handleBlockChangedAck(packet pk.Packet) error {
	var a play.BlockChangedAck
	if err := packet.Scan(&a); err != nil {
		return err
	}
	type change struct {
		pos   BlockPos
		state block.StateID
	}
	var told []change
	w.mu.Lock()
	w.acked = max(w.acked, int32(a.Sequence))
	for pos, p := range w.predicted {
		if p.seq > int32(a.Sequence) {
			continue
		}
		delete(w.predicted, pos)
		sec, i, ok := w.section(pos)
		if !ok || block.StateID(sec.GetBlock(i)) == p.state {
			continue
		}
		w.setBlock(pos, p.state)
		told = append(told, change{pos, p.state})
	}
	w.mu.Unlock()
	if w.events.BlockChange != nil {
		for _, c := range told { // with the world free
			w.events.BlockChange(c.pos, c.state)
		}
	}
	return nil
}

// ServerBlockAt is the block the server last said is at pos: for a block the client predicted (its own
// dig's break, not yet acked) the server's state kept beside the guess, else what the world has. It
// reports false when the chunk is not loaded.
func (w *World) ServerBlockAt(pos BlockPos) (block.StateID, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if p, ok := w.predicted[pos]; ok {
		return p.state, true
	}
	sec, i, ok := w.section(pos)
	if !ok {
		return 0, false
	}
	return block.StateID(sec.GetBlock(i)), true
}

// Acked is the highest sequence of the client's block actions the server has
// acked: an action whose sequence is acked has had every block change it
// made told before (the server sends them first).
func (w *World) Acked() int32 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.acked
}
