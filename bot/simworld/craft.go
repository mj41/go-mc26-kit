package simworld

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/mj41/go-mc26/data/recipe"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/data/tags"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Crafting, as the server plays it: the recipes of the data pack, the book of
// the ones the player has unlocked (by holding an item, as each recipe's
// advancement asks), the grid filled from the inventory when the bot picks a
// recipe (PlaceRecipe), the result slot that follows the grid, and the clicks
// on slots (pick up, shift-click) that take it.

// recipeBook is the crafting recipes — display id i is list[i] — and which
// the player has been told.
type recipeBook struct {
	list []recipe.Recipe
	told []bool
}

func newBook() *recipeBook {
	b := &recipeBook{}
	for _, r := range recipe.All() {
		if (r.Kind == "minecraft:crafting_shaped" || r.Kind == "minecraft:crafting_shapeless") && r.Result != "" {
			b.list = append(b.list, r)
		}
	}
	b.told = make([]bool, len(b.list))
	return b
}

// itemMatches reports whether the item is one an ingredient takes: it is named,
// or it is in a tag the ingredient names.
func itemMatches(alts []string, item string) bool {
	for _, a := range alts {
		if tag, ok := strings.CutPrefix(a, "#"); ok {
			if tags.ItemHas(tag, item) {
				return true
			}
		} else if a == item {
			return true
		}
	}
	return false
}

// holds reports whether the player holds an item one of the group names (an
// event such as "@minecraft:enter_block" is no item: it never holds).
func (g *Game) holds(group []string) bool {
	for _, s := range g.slots {
		if s.n > 0 && itemMatches(group, s.item) {
			return true
		}
	}
	return false
}

// unlockRecipes tells the bot the recipes the inventory has just unlocked
// (RecipeBookAdd): each recipe's advancement asks the player to hold an item of
// each of its groups, none for the crafting table's — unlocked at once.
func (g *Game) unlockRecipes() error {
	if g.book == nil {
		return nil
	}
	var entries []types.RecipeBookAddEntry
	for i, r := range g.book.list {
		if g.book.told[i] {
			continue
		}
		ok := true
		for _, group := range r.Unlock {
			ok = ok && g.holds(group)
		}
		if !ok {
			continue
		}
		g.book.told[i] = true
		entries = append(entries, types.RecipeBookAddEntry{Contents: g.displayOf(i, r)})
	}
	if len(entries) == 0 {
		return nil
	}
	return g.S.Send(play.RecipeBookAdd{Entries: entries})
}

func idOf(table []string, name string) pk.VarInt {
	i := slices.Index(table, name)
	if i < 0 {
		panic("simworld: no " + name)
	}
	return pk.VarInt(i)
}

// slotDisplay is what an ingredient shows: empty, an item, a tag, or any of several.
func slotDisplay(alts []string) types.SlotDisplay {
	one := func(a string) types.SlotDisplay {
		if tag, ok := strings.CutPrefix(a, "#"); ok {
			d := types.SlotDisplay{Type: idOf(registryid.SlotDisplay, "minecraft:tag")}
			setTag(&d, tag)
			return d
		}
		return types.SlotDisplay{Type: idOf(registryid.SlotDisplay, "minecraft:item"), Item: idOf(registryid.Item, a)}
	}
	switch len(alts) {
	case 0:
		return types.SlotDisplay{Type: idOf(registryid.SlotDisplay, "minecraft:empty")}
	case 1:
		return one(alts[0])
	}
	d := types.SlotDisplay{Type: idOf(registryid.SlotDisplay, "minecraft:composite")}
	for _, a := range alts {
		d.Contents = append(d.Contents, one(a))
	}
	return d
}

// displayOf is the book's entry of recipe i.
func (g *Game) displayOf(i int, r recipe.Recipe) types.RecipeDisplayEntry {
	d := types.RecipeDisplay{
		Type:            idOf(registryid.RecipeDisplay, r.Kind),
		Result:          types.SlotDisplay{Type: idOf(registryid.SlotDisplay, "minecraft:item_stack"), Stack: types.ItemStackTemplate{Item: idOf(registryid.Item, r.Result), Count: pk.VarInt(r.Count)}},
		CraftingStation: slotDisplay([]string{"minecraft:crafting_table"}),
		Width:           pk.VarInt(r.Width), Height: pk.VarInt(r.Height),
	}
	for _, alts := range r.Ingredients {
		d.Ingredients = append(d.Ingredients, slotDisplay(alts))
	}
	return types.RecipeDisplayEntry{ID: types.RecipeDisplayId{Index: pk.VarInt(i)}, Display: d,
		Category: idOf(registryid.RecipeBookCategory, "minecraft:crafting_misc")}
}

// matchGrid is the recipe the w × h grid (row by row) makes, if any: a shaped
// recipe whose pattern is the grid's filled cells, as it is or mirrored; a
// shapeless one whose ingredients the filled cells are, one each.
func (g *Game) matchGrid(cells []slotItem, w, h int) (recipe.Recipe, bool) {
	minX, minY, maxX, maxY, filled := w, h, -1, -1, 0
	for i, c := range cells {
		if c.n > 0 {
			x, y := i%w, i/w
			minX, minY, maxX, maxY, filled = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y), filled+1
		}
	}
	if filled == 0 {
		return recipe.Recipe{}, false
	}
	bw, bh := maxX-minX+1, maxY-minY+1
	for _, r := range g.book.list {
		switch r.Kind {
		case "minecraft:crafting_shaped":
			if r.Width != bw || r.Height != bh {
				continue
			}
			for _, mirror := range []bool{false, true} {
				ok := true
				for y := 0; y < bh && ok; y++ {
					for x := 0; x < bw && ok; x++ {
						rx := x
						if mirror {
							rx = bw - 1 - x
						}
						alts := r.Ingredients[y*r.Width+rx]
						c := cells[(minY+y)*w+minX+x]
						ok = len(alts) == 0 && c.n == 0 || len(alts) > 0 && c.n > 0 && itemMatches(alts, c.item)
					}
				}
				if ok {
					return r, true
				}
			}
		case "minecraft:crafting_shapeless":
			if len(r.Ingredients) != filled {
				continue
			}
			var items []string
			for _, c := range cells {
				if c.n > 0 {
					items = append(items, c.item)
				}
			}
			if shapelessMatch(r.Ingredients, items) {
				return r, true
			}
		}
	}
	return recipe.Recipe{}, false
}

// shapelessMatch reports whether every ingredient can be given a different item.
func shapelessMatch(ings [][]string, items []string) bool {
	if len(ings) == 0 {
		return len(items) == 0
	}
	for i, it := range items {
		if itemMatches(ings[0], it) && shapelessMatch(ings[1:], slices.Delete(slices.Clone(items), i, i+1)) {
			return true
		}
	}
	return false
}

// refreshResult sets the menu's result slot to what its grid makes.
func (g *Game) refreshResult(m *menu) {
	if m.furnace { // (no grid: slot 0 is the input)
		return
	}
	*g.at(m, 0) = slotItem{}
	if r, ok := g.matchGrid(g.grid(m), m.gw, m.gw); ok {
		*g.at(m, 0) = slotItem{item: r.Result, n: r.Count}
	}
}

// resultChanged is CraftingMenu.slotsChanged: the grid changed, so the result is made again and sent at once
// (ContainerSetSlot of slot 0) — whether or not it differs from what the bot was last told: a change of the
// grid is one send, and a placed recipe or a taken result changes the grid a cell at a time (the bench's
// traces show each: four empty results as the grid is cleared, the result of each ingredient as it is put in).
func (g *Game) resultChanged(m *menu) {
	g.refreshResult(m)
	s := *g.at(m, 0)
	m.snap[0] = s
	g.stateID++
	if err := g.S.Send(play.ContainerSetSlot{ContainerID: pk.VarInt(m.id), StateID: pk.VarInt(g.stateID), Slot: 0, ItemStack: itemStack(s)}); err != nil {
		g.T.Fatal(err)
	}
}

// consumeGrid takes one of each ingredient out of the grid: a craft made.
// (A recipe that leaves an item behind — a bucket — is not modelled.)
func (g *Game) consumeGrid(m *menu) {
	for k := 1; k <= m.gw*m.gw; k++ {
		s := g.at(m, k)
		if s.n > 0 {
			if strings.HasSuffix(s.item, "_bucket") {
				g.T.Fatalf("simworld: not modelled: the remainder of %s in a craft", s.item)
			}
			s.n--
			if s.n == 0 {
				*s = slotItem{}
			}
			g.resultChanged(m)
		}
	}
}

// placeRecipe is the bot picking a recipe in the book: the grid cleared into
// the inventory, then one of each ingredient taken from the inventory into its
// cell — nothing, if an ingredient is missing (ServerPlaceRecipe).
func (g *Game) placeRecipe(p play.PlaceRecipe) error {
	m := g.current()
	if int(p.ContainerID) != m.id {
		return nil // (a menu that is not the open one: nothing happens)
	}
	i := int(p.Recipe.Index)
	if i < 0 || i >= len(g.book.list) || !g.book.told[i] {
		return nil // (not in the player's book: the server does nothing)
	}
	r := g.book.list[i]
	if r.Kind == "minecraft:crafting_shaped" && (r.Width > m.gw || r.Height > m.gw) || r.Kind == "minecraft:crafting_shapeless" && len(r.Ingredients) > m.gw*m.gw {
		return nil // too big for the grid
	}
	// the grid back into the inventory
	for c := 1; c <= m.gw*m.gw; c++ {
		if s := g.at(m, c); s.n > 0 {
			if left := g.addToInventory(*s); left > 0 {
				return fmt.Errorf("simworld: not modelled: no room to clear the grid")
			}
			*s = slotItem{}
		}
		if m.gw == 2 {
			g.resultChanged(m) // (every cell, empty or not)
		}
	}
	// the cells: a shaped recipe's pattern in the grid's corner, a shapeless one's from the first cell
	var cells []int
	if r.Kind == "minecraft:crafting_shaped" {
		for y := 0; y < r.Height; y++ {
			for x := 0; x < r.Width; x++ {
				cells = append(cells, 1+y*m.gw+x)
			}
		}
	} else {
		for k := range r.Ingredients {
			cells = append(cells, 1+k)
		}
	}
	taken := map[int]int{} // inventory slot → items taken
	for k, alts := range r.Ingredients {
		if len(alts) == 0 {
			continue
		}
		found := -1
		for _, s := range slotOrder() {
			if g.slots[s].n-taken[s] > 0 && itemMatches(alts, g.slots[s].item) {
				found = s
				break
			}
		}
		if found < 0 { // an ingredient missing: the grid is left empty, as the server leaves it
			g.broadcast()
			return nil
		}
		taken[found]++
		*g.at(m, cells[k]) = slotItem{item: g.slots[found].item, n: 1}
		if m.gw == 2 {
			g.resultChanged(m)
		}
	}
	if m.gw != 2 {
		g.resultChanged(m) // (the crafting table's menu sends the made result once, the grid done: the bench's trace of a pickaxe)
	}
	for s, n := range taken {
		g.slots[s].n -= n
		if g.slots[s].n == 0 {
			g.slots[s] = slotItem{}
		}
	}
	g.broadcast()
	return nil
}

// addToInventory puts a stack in the pack and hotbar (the hotbar first), and
// says how many did not fit.
func (g *Game) addToInventory(s slotItem) int {
	limit := stackLimit(s.item)
	for _, i := range slotOrder() {
		if s.n > 0 && g.slots[i].item == s.item && g.slots[i].n > 0 && g.slots[i].n < limit {
			d := min(s.n, limit-g.slots[i].n)
			g.slots[i].n += d
			s.n -= d
		}
	}
	for _, i := range slotOrder() {
		if s.n > 0 && g.slots[i].n == 0 {
			d := min(s.n, limit)
			g.slots[i] = s.of(d)
			s.n -= d
		}
	}
	return s.n
}

// setTag names the tag a tag display shows. Before 26.3 the field is the tag's
// name, from 26.3 an id set holding it: set by reflection, as the field's type
// differs between the versions.
func setTag(d *types.SlotDisplay, tag string) {
	f := reflect.ValueOf(d).Elem().FieldByName("Tag")
	if f.Kind() == reflect.Struct {
		f = f.FieldByName("Tag")
	}
	f.SetString(tag)
}
