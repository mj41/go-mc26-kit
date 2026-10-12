package world

import (
	"github.com/mj41/go-mc26/level"
	"github.com/mj41/go-mc26/level/block"
)

type EventsListener struct {
	LoadChunk   func(pos level.ChunkPos) error
	UnloadChunk func(pos level.ChunkPos) error
	// BlockChange is told each block that changes in the world, after the
	// world has it: the server's (block_update, section_blocks_update: a
	// placement, a crop grown, sand fallen; not a whole chunk sent) and the
	// client's own predictions (a dig's break, at the prediction; told again
	// when the ack undoes it). Called on the packet goroutine, or the goroutine
	// of the client's action, with the world free: keep it short.
	BlockChange func(pos BlockPos, state block.StateID)
}
