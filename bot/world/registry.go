package world

import (
	"sync"

	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
)

// BlockID returns the id of a state's block in the block registry (what block
// tags and tool rules list), -1 for an unknown state.
func BlockID(state block.StateID) int32 {
	if int(state) < 0 || int(state) >= len(block.StateList) {
		return -1
	}
	id, ok := blockIDs()[block.StateList[state].ID()]
	if !ok {
		return -1
	}
	return id
}

var blockIDs = sync.OnceValue(func() map[string]int32 {
	m := make(map[string]int32, len(registryid.Block))
	for i, name := range registryid.Block {
		m[name] = int32(i)
	}
	return m
})
