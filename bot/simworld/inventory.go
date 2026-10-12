package simworld

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// The player's inventory as the server keeps it: the slots of its own menu
// (container 0), numbered as the protocol does — 0 the crafting result, 1–4
// the grid, 5–8 the armor, 9–35 the pack, 36–44 the hotbar, 45 the offhand.

// maxStack is how many of an item a slot holds (the data does not say; every
// item the bricks meet stacks to 64 — tools to 1, which Give checks by name).
const maxStack = 64

type slotItem struct {
	item   string
	n      int
	damage int // a tool's wear (the damage component): 0 is new
}

// of is the stack's item and wear with another count.
func (s slotItem) of(n int) slotItem { s.n = n; return s }

// itemStack is a slot's content as the wire has it.
func itemStack(s slotItem) types.ItemStack {
	if s.n <= 0 {
		return types.ItemStack{}
	}
	d := component.SlotData{Count: pk.VarInt(s.n), Item: pk.VarInt(slices.Index(registryid.Item, s.item))}
	if s.damage > 0 {
		d.Components.PositiveCount = 1
		d.Components.Positive = append(d.Components.Positive, component.Typed{
			Type: pk.VarInt(slices.Index(registryid.DataComponentType, "minecraft:damage")), Value: &component.Damage{Value: pk.VarInt(s.damage)}})
	}
	return types.ItemStack{SlotData: d}
}

// stackLimit is how many of an item go in a slot: tools, weapons and armor one.
func stackLimit(item string) int {
	for _, s := range []string{"_pickaxe", "_axe", "_shovel", "_hoe", "_sword", "_helmet", "_chestplate", "_leggings", "_boots", "_bucket"} {
		if len(item) > len(s) && item[len(item)-len(s):] == s {
			return 1
		}
	}
	return maxStack
}

// Give puts n of the item in the player's inventory, into a stack of it with
// room first, else the first empty slot from the hotbar's left (Inventory.add),
// and tells the bot each slot changed. It says how many did not fit.
func (g *Game) Give(item string, n int) int {
	g.T.Helper()
	if slices.Index(registryid.Item, item) < 0 {
		g.T.Fatalf("simworld: no item %q", item)
	}
	left := g.add(item, n)
	g.broadcast()
	return left
}

// add puts the items in the inventory without telling the bot (the slots are told at
// the end of the server's tick, as the game does).
func (g *Game) add(item string, n int) int {
	limit := stackLimit(item)
	for _, i := range slotOrder() { // stacks with room first
		if n > 0 && g.slots[i].item == item && g.slots[i].n < limit {
			d := min(n, limit-g.slots[i].n)
			g.slots[i].n += d
			n -= d
		}
	}
	for _, i := range slotOrder() {
		if n > 0 && g.slots[i].n == 0 {
			d := min(n, limit)
			g.slots[i] = slotItem{item: item, n: d}
			n -= d
		}
	}
	return n
}

// slotOrder is the order a picked-up item looks for a slot: the hotbar, then the pack.
func slotOrder() []int {
	var o []int
	for i := 36; i <= 44; i++ {
		o = append(o, i)
	}
	for i := 9; i <= 35; i++ {
		o = append(o, i)
	}
	return o
}

// broadcast tells the bot the slots (and the cursor) that changed since it was
// last told — AbstractContainerMenu.broadcastChanges: a slot packet for each,
// in the order of the slots, the cursor last, for the player's own menu and the
// open one; and the recipes the new inventory unlocks.
func (g *Game) broadcast() {
	g.T.Helper()
	menus := []*menu{g.menu0}
	if g.open != nil {
		// with a menu open only it is told (the bench's trace of a craft at a table: the pack's slots come as the table's
		// own, the player's own menu says nothing till it is closed)
		menus = []*menu{g.open}
	}
	for _, m := range menus {
		g.refreshResult(m) // (the result slot follows the grid)
		for i := 0; i < m.size(); i++ {
			if s := *g.at(m, i); s != m.snap[i] {
				m.snap[i] = s
				g.stateID++
				err := g.S.Send(play.ContainerSetSlot{ContainerID: pk.VarInt(m.id), StateID: pk.VarInt(g.stateID), Slot: pk.Short(i), ItemStack: itemStack(s)})
				if err != nil {
					g.T.Fatal(err)
				}
			}
		}
	}
	if g.cursor != g.snapCursor {
		g.snapCursor = g.cursor
		if err := g.S.Send(play.SetCursorItem{Contents: itemStack(g.cursor)}); err != nil {
			g.T.Fatal(err)
		}
	}
	if err := g.unlockRecipes(); err != nil {
		g.T.Fatal(err)
	}
	if err := g.tellAttributes(); err != nil {
		g.T.Fatal(err)
	}
}

// Count is how many of the item the player carries.
func (g *Game) Count(item string) int {
	n := 0
	for _, s := range g.slots {
		if s.item == item {
			n += s.n
		}
	}
	return n
}

// heldItem is what the player holds (the hotbar slot it chose).
func (g *Game) heldItem() slotItem { return g.slots[36+g.held] }

// String names a slot for a message.
func (s slotItem) String() string {
	if s.n == 0 {
		return "nothing"
	}
	return fmt.Sprintf("%d %s", s.n, s.item)
}

// armorSlot is the inventory's slot a piece of armor is worn in (5 head, 6 chest, 7 legs, 8 feet), 0 for
// what is no armor.
func armorSlot(item string) int {
	for i, s := range []string{"_helmet", "_chestplate", "_leggings", "_boots"} {
		if strings.HasSuffix(item, s) {
			return 5 + i
		}
	}
	return 0
}

// wear is a UseItem with armor in hand (Equippable: swapWithEquipmentSlot): the piece goes to its slot,
// what was worn there comes to the hand.
func (g *Game) wear(slot int, seq pk.VarInt) error {
	hand := 36 + g.held
	g.slots[hand], g.slots[slot] = g.slots[slot], g.slots[hand]
	g.broadcast()
	return g.ack(seq)
}

// Worn is what the player wears in the armor slot (5 head, 6 chest, 7 legs, 8 feet): "" nothing.
func (g *Game) Worn(slot int) string {
	if g.slots[slot].n == 0 {
		return ""
	}
	return g.slots[slot].item
}
