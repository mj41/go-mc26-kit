// Package recipes keeps the recipe book the server shows the player: every
// recipe it unlocked, by the display id the server gave it this session, with
// what it makes — what a person picks from the book to have the server fill
// the crafting grid (ServerboundPlaceRecipe).
package recipes

import (
	"reflect"
	"sync"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Ingredient is one slot of a recipe's grid (or a furnace's input): the items
// it takes, and the item tags whose items it takes.
type Ingredient struct {
	Items []string
	Tags  []string
}

// Recipe is one recipe of the book.
type Recipe struct {
	ID       int32  // the display id ServerboundPlaceRecipe names
	Kind     string // minecraft:crafting_shaped, minecraft:crafting_shapeless, minecraft:furnace, …
	Result   string // the item it makes, "" when the display does not name one
	Count    int
	Width    int // a shaped recipe's grid: 3 needs a crafting table
	Height   int
	Category string
	// Ingredients are the filled slots of the grid, one entry per item used.
	Ingredients []Ingredient
}

// Book is the player's recipe book.
type Book struct {
	c  *bot.Client
	mu sync.Mutex
	m  map[int32]Recipe
}

// New creates the book and registers its handlers.
func New(c *bot.Client) *Book {
	b := &Book{c: c, m: map[int32]Recipe{}}
	c.Events.AddListener(
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayRecipeBookAdd, F: b.onAdd},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayRecipeBookRemove, F: b.onRemove},
	)
	return b
}

// For returns the recipes that make item ("minecraft:oak_planks").
func (b *Book) For(item string) []Recipe {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Recipe
	for _, r := range b.m {
		if r.Result == item {
			out = append(out, r)
		}
	}
	return out
}

// Len is the number of recipes in the book.
func (b *Book) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.m)
}

func name(table []string, id pk.VarInt) string {
	if int(id) >= 0 && int(id) < len(table) {
		return table[id]
	}
	return ""
}

// result reads the item a slot display shows: an item, an item stack.
func result(d types.SlotDisplay) (string, int) {
	switch name(registryid.SlotDisplay, d.Type) {
	case "minecraft:item":
		return name(registryid.Item, d.Item), 1
	case "minecraft:item_stack":
		return name(registryid.Item, d.Stack.Item), int(d.Stack.Count)
	}
	return "", 0
}

func (b *Book) onAdd(packet pk.Packet) error {
	var a play.RecipeBookAdd
	if err := packet.Scan(&a); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if a.Replace {
		b.m = map[int32]Recipe{}
	}
	for _, e := range a.Entries {
		c := e.Contents
		r := Recipe{ID: int32(c.ID.Index), Kind: name(registryid.RecipeDisplay, c.Display.Type),
			Width: int(c.Display.Width), Height: int(c.Display.Height), Category: name(registryid.RecipeBookCategory, c.Category)}
		r.Result, r.Count = result(c.Display.Result)
		for _, d := range c.Display.Ingredients {
			if ing, ok := ingredient(d); ok {
				r.Ingredients = append(r.Ingredients, ing)
			}
		}
		if r.Kind == "minecraft:furnace" || r.Kind == "minecraft:smoker" || r.Kind == "minecraft:blast_furnace" {
			if ing, ok := ingredient(c.Display.Ingredient); ok {
				r.Ingredients = append(r.Ingredients, ing)
			}
		}
		b.m[r.ID] = r
	}
	return nil
}

// ingredient reads what a slot display of a recipe's input takes: an item,
// a stack, a tag, or any of several (composite); an empty slot is no
// ingredient.
func ingredient(d types.SlotDisplay) (Ingredient, bool) {
	var ing Ingredient
	switch name(registryid.SlotDisplay, d.Type) {
	case "minecraft:item":
		ing.Items = []string{name(registryid.Item, d.Item)}
	case "minecraft:item_stack":
		ing.Items = []string{name(registryid.Item, d.Stack.Item)}
	case "minecraft:tag":
		tag, ids := tagOf(d.Tag)
		if ids != nil {
			for _, id := range ids {
				ing.Items = append(ing.Items, name(registryid.Item, pk.VarInt(id)))
			}
		} else {
			ing.Tags = []string{tag}
		}
	case "minecraft:composite":
		for _, c := range d.Contents {
			if sub, ok := ingredient(c); ok {
				ing.Items = append(ing.Items, sub.Items...)
				ing.Tags = append(ing.Tags, sub.Tags...)
			}
		}
	default:
		return ing, false
	}
	return ing, len(ing.Items)+len(ing.Tags) > 0
}

// tagOf reads a tag display: before 26.3 the tag's name, from 26.3 an id set
// (a tag's name, or the ids themselves). By reflection, since the field's type
// differs between the versions.
func tagOf(v any) (string, []int32) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.String {
		return rv.String(), nil
	}
	if rv.Kind() == reflect.Struct {
		var ids []int32
		if f := rv.FieldByName("IDs"); f.IsValid() && !f.IsNil() {
			for i := 0; i < f.Len(); i++ {
				ids = append(ids, int32(f.Index(i).Int()))
			}
			return "", ids
		}
		if f := rv.FieldByName("Tag"); f.IsValid() {
			return f.String(), nil
		}
	}
	return "", nil
}

// Options returns the items an ingredient takes, its tags resolved with the
// tags the server sent.
func (b *Book) Options(ing Ingredient) []string {
	out := append([]string(nil), ing.Items...)
	for _, t := range ing.Tags {
		for _, id := range b.c.Tags.IDs("minecraft:item", t) {
			out = append(out, name(registryid.Item, pk.VarInt(id)))
		}
	}
	return out
}

func (b *Book) onRemove(packet pk.Packet) error {
	var rm play.RecipeBookRemove
	if err := packet.Scan(&rm); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range rm.Recipes {
		delete(b.m, int32(id.Index))
	}
	return nil
}
