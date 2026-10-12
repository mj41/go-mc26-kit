package simworld

import (
	"fmt"
	"math"

	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Clicks on the slots of the player's own menu (container 0), as the server
// plays them (AbstractContainerMenu.clicked, InventoryMenu.quickMoveStack):
// the cursor picks a slot up and puts it down; a shift-click moves a slot's
// stack to the other part of the inventory — the result slot's, as many times
// as the grid allows.

// armor and offhand slots: not modelled yet.
func restricted(slot int) bool { return slot >= 5 && slot <= 8 || slot == 45 }

// click plays a ContainerClick, and tells the bot what its prediction got wrong.
func (g *Game) click(c play.ContainerClick) error {
	m := g.current()
	if int(c.ContainerID) != m.id {
		return nil // (a click in a menu the server has closed: it is ignored)
	}
	slot := int(c.SlotNum)
	if slot < 0 || slot >= m.size() {
		return fmt.Errorf("simworld: not modelled: a click on slot %d (outside the menu: dropping)", slot)
	}
	if !m.table && !m.furnace && restricted(slot) {
		return fmt.Errorf("simworld: not modelled: a click on slot %d (armor, offhand)", slot)
	}
	var err error
	switch c.ContainerInput {
	case types.ContainerInputPickup:
		err = g.pickup(m, slot, int(c.ButtonNum))
	case types.ContainerInputQuickMove:
		err = g.quickMove(m, slot)
	case types.ContainerInputSwap:
		err = g.swap(m, slot, int(c.ButtonNum))
	case types.ContainerInputThrow:
		err = g.throw(m, slot, int(c.ButtonNum))
	default:
		return fmt.Errorf("simworld: not modelled: a click of mode %d", c.ContainerInput)
	}
	if err != nil {
		return err
	}
	g.broadcast()
	// what the bot predicted of the slots it changed, against what they are: a slot
	// that differs is told again (it may not have changed since the last broadcast)
	for _, e := range c.ChangedSlots {
		i := int(e.Key)
		if i >= 0 && i < m.size() && !hashedIs(e.Val, *g.at(m, i)) {
			g.stateID++
			if err := g.S.Send(play.ContainerSetSlot{ContainerID: pkVarInt(int32(m.id)), StateID: pkVarInt(g.stateID), Slot: shortOf(i), ItemStack: itemStack(*g.at(m, i))}); err != nil {
				return err
			}
		}
	}
	if !hashedIs(c.CarriedItem, g.cursor) {
		return g.S.Send(play.SetCursorItem{Contents: itemStack(g.cursor)})
	}
	return nil
}

// pickup is a left (0) or right (1) click: the cursor takes the slot's stack,
// puts its own down, or swaps — the result slot only gives.
func (g *Game) pickup(m *menu, slot, button int) error {
	if button != 0 && button != 1 {
		return fmt.Errorf("simworld: not modelled: a pickup with the button %d", button)
	}
	sp := g.at(m, slot)
	s, cur := *sp, g.cursor
	if m.furnace && slot == 2 { // a furnace's output is only taken: onto the cursor, whole, if it has room for it
		if s.n == 0 || cur.n > 0 && (cur.item != s.item || cur.n+s.n > stackLimit(s.item)) {
			return nil
		}
		g.cursor = s.of(cur.n + s.n)
		*sp = slotItem{}
		return nil
	}
	if !m.furnace && slot == 0 { // the result: taken whole, if the cursor can hold it with what it has
		if s.n == 0 || cur.n > 0 && (cur.item != s.item || cur.n+s.n > stackLimit(s.item)) {
			return nil
		}
		g.cursor = s.of(cur.n + s.n)
		g.consumeGrid(m)
		return nil
	}
	switch {
	case cur.n == 0 && s.n > 0: // take: all, or half rounded up
		take := s.n
		if button == 1 {
			take = (s.n + 1) / 2
		}
		g.cursor = s.of(take)
		sp.n -= take
	case cur.n > 0 && s.n == 0: // put: all, or one
		if !g.mayPlace(m, slot, cur.item) {
			return nil
		}
		put := cur.n
		if button == 1 {
			put = 1
		}
		put = min(put, stackLimit(cur.item))
		*sp = cur.of(put)
		g.cursor.n -= put
	case cur.n > 0 && s.item == cur.item: // add to the stack: all, or one
		put := cur.n
		if button == 1 {
			put = 1
		}
		put = min(put, stackLimit(s.item)-s.n)
		sp.n += put
		g.cursor.n -= put
	case cur.n > 0: // another item: swapped
		if cur.n > stackLimit(cur.item) || !g.mayPlace(m, slot, cur.item) {
			return nil
		}
		*sp, g.cursor = cur, s
	}
	g.tidy(m)
	return nil
}

// tidy clears the stacks with nothing in them.
func (g *Game) tidy(m *menu) {
	for i := 0; i < m.size(); i++ {
		if s := g.at(m, i); s.n <= 0 {
			*s = slotItem{}
		}
	}
	if g.cursor.n <= 0 {
		g.cursor = slotItem{}
	}
}

// quickMove is the shift-click (InventoryMenu and CraftingMenu quickMoveStack).
func (g *Game) quickMove(m *menu, slot int) error {
	sp := g.at(m, slot)
	s := *sp
	if s.n == 0 {
		return nil
	}
	grid := m.gw * m.gw
	switch {
	case m.furnace: // FurnaceMenu.quickMoveStack
		switch {
		case slot == 2:
			sp.n = g.moveTo(m, s, m.pack0, m.end, true)
		case slot < 2:
			sp.n = g.moveTo(m, s, m.pack0, m.end, false)
		default:
			if _, ok := smeltOf(s.item); ok {
				sp.n = g.moveTo(m, s, 0, 1, false)
			} else if burnTime(s.item) > 0 {
				sp.n = g.moveTo(m, s, 1, 2, false)
			} else if slot < m.hotbar0 {
				sp.n = g.moveTo(m, s, m.hotbar0, m.end, false)
			} else {
				sp.n = g.moveTo(m, s, m.pack0, m.hotbar0, false)
			}
		}
	case slot == 0: // the result: into the inventory from its end, and again while the grid still makes the same
		for g.at(m, 0).n > 0 {
			res := *g.at(m, 0)
			left := g.moveTo(m, res, m.pack0, m.end, true)
			if left == res.n {
				break // no room
			}
			if left > 0 { // only part of it fitted: not crafted
				return fmt.Errorf("simworld: not modelled: the result of a craft that half fits")
			}
			g.consumeGrid(m)
			g.refreshResult(m)
			if g.at(m, 0).item != res.item {
				break
			}
		}
	case slot < m.pack0:
		sp.n = g.moveTo(m, s, m.pack0, m.end, false)
	case slot < m.hotbar0:
		sp.n = g.moveTo(m, s, m.hotbar0, m.end, false)
	default:
		sp.n = g.moveTo(m, s, m.pack0, m.hotbar0, false)
	}
	_ = grid
	g.tidy(m)
	return nil
}

// moveTo puts the stack into the slots [from, to) — onto stacks of its item
// first, then into empty slots, in the order or the reverse — and says how many
// did not fit (AbstractContainerMenu.moveItemStackTo).
func (g *Game) moveTo(m *menu, s slotItem, from, to int, reverse bool) int {
	limit := stackLimit(s.item)
	order := make([]int, 0, to-from)
	for i := from; i < to; i++ {
		order = append(order, i)
	}
	if reverse {
		for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
			order[i], order[j] = order[j], order[i]
		}
	}
	for _, i := range order {
		if !g.mayPlace(m, i, s.item) {
			continue
		}
		if p := g.at(m, i); s.n > 0 && p.n > 0 && p.item == s.item && p.n < limit {
			d := min(s.n, limit-p.n)
			p.n += d
			s.n -= d
		}
	}
	for _, i := range order {
		if !g.mayPlace(m, i, s.item) {
			continue
		}
		if p := g.at(m, i); s.n > 0 && p.n == 0 {
			d := min(s.n, limit)
			*p = s.of(d)
			s.n -= d
		}
	}
	return s.n
}

// hashedIs reports whether a slot as the bot sent it is the slot as the server has it.
func hashedIs(h hashed, s slotItem) bool {
	if !h.Has {
		return s.n == 0
	}
	return s.n > 0 && itemStack(s).Item == h.Val.Item && int(h.Val.Count) == s.n
}

type hashed = pk.Option[types.HashedStackActualItem, *types.HashedStackActualItem]

func pkVarInt(i int32) pk.VarInt { return pk.VarInt(i) }
func shortOf(i int) pk.Short     { return pk.Short(i) }

// swap exchanges a slot with a hotbar slot (button 0–8): the number key over a slot.
func (g *Game) swap(m *menu, slot, button int) error {
	if button < 0 || button > 8 {
		return fmt.Errorf("simworld: not modelled: a swap with the button %d (the offhand)", button)
	}
	if slot == 0 {
		return fmt.Errorf("simworld: not modelled: a swap with the result slot")
	}
	hot := g.at(m, m.hotbar0+button)
	sp := g.at(m, slot)
	if sp == hot {
		return nil
	}
	*sp, *hot = *hot, *sp
	g.tidy(m)
	return nil
}

// throw drops a slot's items (Q: one; Ctrl+Q: the whole stack) as an item that lies
// a pickup delay of 40 ticks. It lands a block and a half ahead of the player, on the
// ground there: its flight is not simulated.
func (g *Game) throw(m *menu, slot, button int) error {
	if slot == 0 {
		return fmt.Errorf("simworld: not modelled: throwing the result slot")
	}
	sp := g.at(m, slot)
	if sp.n == 0 {
		return nil
	}
	n := 1
	if button == 1 {
		n = sp.n
	}
	what := sp.of(n)
	sp.n -= n
	g.tidy(m)
	yaw := g.yaw * math.Pi / 180
	x, z := g.pos[0]-math.Sin(yaw)*1.5, g.pos[2]+math.Cos(yaw)*1.5
	return g.spawnItem(x, int(math.Floor(g.pos[1]+1.62-0.3)), z, what, 40)
}
