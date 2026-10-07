// Package act is what a player does with its hands: dig, place, use, eat —
// what MultiPlayerGameMode sends, with the client's prediction sequence and
// in the client's timing.
package act

import (
	"github.com/mj41/go-mc26/data/item"
	"github.com/mj41/go-mc26/level/component"
	"github.com/mj41/go-mc26/protocol/types"
)

// Component returns a stack's component of the type of T: the stack's own
// patch first (added, or removed), then the item's default (data/item).
func Component[T component.DataComponent](s types.ItemStack) T {
	var zero T
	if s.Count <= 0 {
		return zero
	}
	for _, c := range s.Components.Positive {
		if v, ok := c.Value.(T); ok {
			return v
		}
	}
	for _, removed := range s.Components.Negative {
		// a removed default: the patch names the type by id
		if _, ok := component.NewComponent(int32(removed)).(T); ok {
			return zero
		}
	}
	return item.DefaultComponent[T](item.ID(s.Item))
}
