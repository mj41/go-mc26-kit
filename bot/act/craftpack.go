package act

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mj41/go-mc26/data/recipe"
	"github.com/mj41/go-mc26/data/registryid"
)

// A recipe the book does not have yet is made by hand, as a person makes it:
// the book holds only what the server unlocked (sticks once planks are held,
// a moment after they came — the body asked for sticks right after its
// planks and had "no recipe in the book"; a chest only with ten kinds of
// items held), a person knows every recipe. The version's data pack has them
// all (data/recipe): the ingredients laid in the grid by hand (CraftByHand).

// craftFromPack makes n of item from the data pack's recipe, laid by hand in
// the open grid (id 0: the inventory's 2×2, else a table's 3×3).
func (s *Screens) craftFromPack(ctx context.Context, id int, item string, n int) (int, error) {
	gw := 2
	if id != 0 {
		gw = 3
	}
	var last error
	for _, r := range recipe.For(item) {
		if r.Kind != "minecraft:crafting_shaped" && r.Kind != "minecraft:crafting_shapeless" {
			continue
		}
		cells, width := s.cellsFor(r, gw)
		if cells == nil {
			last = fmt.Errorf("act: no %s in the book, and its ingredients not held for the %d×%d grid", item, gw, gw)
			continue
		}
		times := (n + r.Count - 1) / r.Count
		if have := s.timesHeld(cells); have < times {
			times = have
		}
		if times == 0 {
			last = fmt.Errorf("act: no %s in the book, and its ingredients short", item)
			continue
		}
		got, err := s.CraftByHand(ctx, cells, width, times)
		if err != nil {
			last = err
			continue
		}
		return got, nil
	}
	if last == nil {
		last = fmt.Errorf("act: no recipe for %s", item)
	}
	return 0, last
}

// cellsFor is the recipe's grid in items held (each cell the held item that
// fits it, the most held first), and its width; nil when it does not fit the
// grid or a cell has no item held.
func (s *Screens) cellsFor(r recipe.Recipe, gw int) ([]string, int) {
	held := s.heldCounts()
	width := r.Width
	if r.Kind == "minecraft:crafting_shapeless" {
		if len(r.Ingredients) > gw*gw {
			return nil, 0
		}
		width = min(len(r.Ingredients), gw)
	} else if r.Width > gw || r.Height > gw {
		return nil, 0
	}
	cells := make([]string, len(r.Ingredients))
	for i, opts := range r.Ingredients {
		if len(opts) == 0 {
			continue
		}
		best := ""
		for _, o := range opts {
			for name, c := range held {
				if c > held[best] && s.fits(o, name) {
					best = name
				}
			}
		}
		if best == "" {
			return nil, 0
		}
		cells[i] = best
	}
	return cells, width
}

// fits: the item is the option, or in its tag ("#minecraft:planks").
func (s *Screens) fits(option, item string) bool {
	tag, isTag := strings.CutPrefix(option, "#")
	if !isTag {
		return option == item
	}
	id := slices.Index(registryid.Item, item)
	return id >= 0 && s.C != nil && s.C.Tags.Has("minecraft:item", tag, int32(id))
}

// heldCounts is how many of each item the player has (the open menu's player
// part).
func (s *Screens) heldCounts() map[string]int {
	_, slots, start := s.current()
	out := map[string]int{}
	for i := start; i < len(slots); i++ {
		if slots[i].Count > 0 {
			out[itemName(slots[i].Item)] += int(slots[i].Count)
		}
	}
	return out
}

// timesHeld is how many times the cells can be laid with what is held.
func (s *Screens) timesHeld(cells []string) int {
	need := map[string]int{}
	for _, c := range cells {
		if c != "" {
			need[c]++
		}
	}
	held := s.heldCounts()
	times := -1
	for item, k := range need {
		if t := held[item] / k; times < 0 || t < times {
			times = t
		}
	}
	return max(times, 0)
}
