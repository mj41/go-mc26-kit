package physics

import (
	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Tags answers whether a block state's block is in a block tag the server
// sent ("minecraft:climbable").
type Tags interface {
	BlockIn(state block.StateID, tag string) bool
}

// ClientTags reads the block tags a client received.
type ClientTags struct{ T *bot.Tags }

func (c ClientTags) BlockIn(state block.StateID, tag string) bool {
	id := world.BlockID(state)
	return id >= 0 && c.T.Has("minecraft:block", tag, id)
}
