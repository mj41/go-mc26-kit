package main

import (
	"slices"
	"sort"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/entity"
	"github.com/mj41/go-mc26/data/item"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/component"
)

// What the robot knows of kinds of things is the game's own data, not lists
// of its own: which blocks are logs, ores, crops — the tags the server sends
// (its data pack); which items are food and how much — the items' default
// components; which mobs are hostile — the entities' categories (go-mc26's
// data packages, generated from the server). What is judgement and not data
// stays here, said as such: which ores are worth digging, the neutral mobs
// not to provoke, what a miner leaves in a chest.

// serverTags are the tags the server sent, set once the robot has joined.
var serverTags *bot.Tags

// tagNames is the names of the entries of a tag of registry, from names
// (the registry's names by id).
func tagNames(registry, tag string, names []string) []string {
	if serverTags == nil {
		return nil
	}
	var out []string
	for _, id := range serverTags.IDs(registry, tag) {
		if id >= 0 && int(id) < len(names) {
			out = append(out, names[id])
		}
	}
	return out
}

// tagBlocks is the blocks of a block tag ("minecraft:crops"), by name.
func tagBlocks(tag string) []string { return tagNames("minecraft:block", tag, registryid.Block) }

// tagItems is the items of an item tag ("minecraft:logs"), by name.
func tagItems(tag string) []string { return tagNames("minecraft:item", tag, registryid.Item) }

// logItems are the logs of any tree (those that burn: charcoal, a furnace's fuel).
func logItems() []string { return tagItems("minecraft:logs_that_burn") }

// isCrop: a crop is sown, not dug for what it drops (wheat gives seeds for
// sure, but a field pulled up for them is no way to farm).
func isCrop(block string) bool { return slices.Contains(tagBlocks("minecraft:crops"), block) }

// oreTags are the ores worth digging on the way (judgement: coal for
// torches, iron for tools, diamonds); each tag has the deepslate kind too.
var oreTags = []string{"minecraft:coal_ores", "minecraft:iron_ores", "minecraft:diamond_ores"}

// oreBlocks are the blocks of oreTags.
func oreBlocks() []string {
	var out []string
	for _, t := range oreTags {
		out = append(out, tagBlocks(t)...)
	}
	return out
}

// neutral are monsters left alone unless they start it (judgement): an
// enderman looked at, a piglin, fights it cannot win — and the monsters'
// mounts (a zombie horse, a camel husk, a zombie nautilus), which never do:
// their riders are fought, they are not.
var neutral = map[string]bool{
	"minecraft:enderman": true, "minecraft:zombified_piglin": true, "minecraft:piglin": true,
	"minecraft:zombie_horse": true, "minecraft:camel_husk": true, "minecraft:zombie_nautilus": true,
}

// hostileType: a mob of the monster category, the neutral aside.
func hostileType(t string) bool {
	e, ok := entity.ByName[t]
	return ok && e.Type == "monster" && !neutral[t]
}

// gameType: an animal (the creature category), what a hunt is after.
func gameType(t string) bool {
	e, ok := entity.ByName[t]
	return ok && e.Type == "creature"
}

// foods are the items that feed it, the most nourishing first: every item
// with food in its default components whose eating does nothing else (not
// rotten flesh's hunger, a spider eye's poison, a chorus fruit's jump).
var foods = edibleFoods()

func edibleFoods() []string {
	type food struct {
		name string
		n    int
		sat  float32
	}
	var fs []food
	for id, name := range registryid.Item {
		f := item.DefaultComponent[*component.Food](item.ID(id))
		if f == nil {
			continue
		}
		if c := item.DefaultComponent[*component.Consumable](item.ID(id)); c != nil && len(c.OnConsumeEffects) > 0 {
			continue
		}
		fs = append(fs, food{name, int(f.Nutrition), float32(f.Saturation)})
	}
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].n != fs[j].n {
			return fs[i].n > fs[j].n
		}
		return fs[i].sat > fs[j].sat
	})
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.name
	}
	return out
}

// rangedType: a mob that shoots from afar — running from it is a back shot.
func rangedType(t string) bool {
	switch t {
	case "minecraft:skeleton", "minecraft:stray", "minecraft:bogged", "minecraft:pillager", "minecraft:witch":
		return true
	}
	return false
}
