package world

import (
	"github.com/mj41/go-mc26/level"
	"github.com/mj41/go-mc26/level/block"
)

type EventsListener struct {
	LoadChunk   func(pos level.ChunkPos) error
	UnloadChunk func(pos level.ChunkPos) error
	// BlockChange is told each block the server changes (block_update,
	// section_blocks_update), after the world has it: a dig, a placement, a
	// crop grown, sand fallen. Not a whole chunk sent. Called on the packet
	// goroutine with the world free: keep it short.
	BlockChange func(pos BlockPos, state block.StateID)
}
