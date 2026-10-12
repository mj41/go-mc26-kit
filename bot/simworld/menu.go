package simworld

import (
	"fmt"
	"slices"

	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// A menu is what the player sees of a container: the player's own (the
// inventory with its two by two grid) or a crafting table's. Both have the
// result slot first (0), the grid next, then the pack and the hotbar; the pack
// and the hotbar are the player's own slots in either (g.slots).
type menu struct {
	id      int
	table   bool
	furnace bool         // a furnace's menu: input 0, fuel 1, output 2 (the block entity's), then the player's
	pos     [3]int       // a furnace's place
	own     [10]slotItem // a table's own slots: the result (0) and the grid (1–9)
	snap    [46]slotItem // the slots as the bot was last told
	// the grid is gw × gw cells from slot 1; the pack is [pack0, hotbar0), the
	// hotbar [hotbar0, end)
	gw, pack0, hotbar0, end int
}

func inventoryMenu() *menu { return &menu{gw: 2, pack0: 9, hotbar0: 36, end: 45} }

// furnaceMenu is a furnace's: its three slots, then the pack (27) and the hotbar (9).
func furnaceMenu(id int, pos [3]int) *menu {
	return &menu{id: id, furnace: true, pos: pos, pack0: 3, hotbar0: 30, end: 39}
}

func tableMenu(id int) *menu {
	return &menu{id: id, table: true, gw: 3, pack0: 10, hotbar0: 37, end: 46}
}

// size is how many slots the menu has.
func (m *menu) size() int { return m.end }

// at is the storage of the menu's slot i: the player's own slots are the
// inventory's (the table's pack and hotbar come one later than the inventory's).
func (g *Game) at(m *menu, i int) *slotItem {
	switch {
	case m.furnace && i < 3:
		return &g.furnaces[m.pos].s[i]
	case m.furnace:
		return &g.slots[i-m.pack0+9]
	case !m.table:
		return &g.slots[i]
	case i < 10:
		return &m.own[i]
	default:
		return &g.slots[i-m.pack0+9]
	}
}

// grid is the menu's grid cells, row by row.
func (g *Game) grid(m *menu) []slotItem {
	cells := make([]slotItem, m.gw*m.gw)
	for k := range cells {
		cells[k] = *g.at(m, 1+k)
	}
	return cells
}

// view is the menu's slots as the bot sees them.
func (g *Game) view(m *menu) []slotItem {
	v := make([]slotItem, m.end)
	for i := range v {
		v[i] = *g.at(m, i)
	}
	return v
}

// current is the menu the player has open.
func (g *Game) current() *menu {
	if g.open != nil {
		return g.open
	}
	return g.menu0
}

// openTable opens a crafting table's menu: the screen, then its contents.
func (g *Game) openTable() error {
	g.containerID = g.containerID%100 + 1
	m := tableMenu(g.containerID)
	g.open = m
	idx := slices.Index(registryid.Menu, "minecraft:crafting")
	if err := g.S.Send(play.OpenScreen{ContainerID: pk.VarInt(m.id), Type: pk.VarInt(idx), Title: types.Text(chat.Text("Crafting"))}); err != nil {
		return err
	}
	g.stateID++
	items := make([]types.ItemStack, m.end)
	for i, s := range g.view(m) {
		items[i] = itemStack(s)
	}
	m.snap = [46]slotItem(append(g.view(m), make([]slotItem, 46-m.end)...))
	return g.S.Send(play.ContainerSetContent{ContainerID: pk.VarInt(m.id), StateID: pk.VarInt(g.stateID), Items: items, CarriedItem: itemStack(g.cursor)})
}

// closeMenu is the bot closing the open menu: the grid's items go back to the
// player's inventory (AbstractContainerMenu.removed → clearContainer), and the
// cursor's too.
func (g *Game) closeMenu(id int) error {
	if g.open == nil || g.open.id != id {
		if id == 0 && g.open == nil {
			return g.returnGrid(g.menu0)
		}
		return nil // (a menu closed that is not open: the server has closed it already)
	}
	m := g.open
	if err := g.returnGrid(m); err != nil {
		return err
	}
	g.broadcast() // (told in the table's own menu)
	g.open = nil
	// the player's own menu takes over what the client already knows (AbstractContainerMenu.transferState): the pack
	// slots are the same in both, so nothing is told again at the close — the bench's trace of a craft at a table
	for i := 9; i < g.menu0.size(); i++ {
		g.menu0.snap[i] = *g.at(g.menu0, i)
	}
	return nil
}

// returnGrid puts what is on the grid and the cursor back into the inventory.
func (g *Game) returnGrid(m *menu) error {
	for k := 1; k <= m.gw*m.gw; k++ {
		if s := *g.at(m, k); s.n > 0 {
			if left := g.addToInventory(s); left > 0 {
				return fmt.Errorf("simworld: not modelled: no room to return the grid's %s", s)
			}
			*g.at(m, k) = slotItem{}
		}
	}
	if g.cursor.n > 0 {
		if left := g.addToInventory(g.cursor); left > 0 {
			return fmt.Errorf("simworld: not modelled: no room to return the cursor's %s", g.cursor)
		}
		g.cursor = slotItem{}
	}
	g.broadcast()
	return nil
}
