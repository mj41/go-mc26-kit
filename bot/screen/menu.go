package screen

import (
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/constants"
	"github.com/mj41/go-mc26/data/item"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Menu is an open container of any type — a chest, a crafting table, a
// furnace — with every slot the server sent: the container's own first, then
// the player's inventory (27 slots, then the hotbar's 9).
type Menu struct {
	ID    int
	Type  string // minecraft:generic_9x3, minecraft:crafting, minecraft:furnace, …
	Title chat.Message
	Slots []Slot
	// Offers are a merchant's trades (ClientboundMerchantOffers), for a
	// villager's or a wandering trader's menu.
	Offers []types.MerchantOffer
}

func (m *Menu) onSetSlot(i int, s Slot) error {
	if i < 0 {
		return nil
	}
	for len(m.Slots) <= i {
		m.Slots = append(m.Slots, Slot{})
	}
	m.Slots[i] = s
	return nil
}

func (m *Menu) onClose() error { return nil }

// PlayerStart is the first slot of the player's inventory in the menu.
// It is negative while the menu's contents have not come (or for a menu with
// no inventory part).
func (m *Menu) PlayerStart() int { return len(m.Slots) - 36 }

// mirror copies a slot of the player's part of an open menu into the
// inventory menu: the client has one inventory, which every menu shows (the
// main 27 then the hotbar's 9, as the inventory menu has them from slot 9).
func (m *Manager) mirror(id, i int) {
	menu, ok := m.Screens[id].(*Menu)
	if !ok || id == 0 {
		return
	}
	start := menu.PlayerStart()
	if start < 0 || i < start || i >= len(menu.Slots) {
		return
	}
	m.Inventory.Slots[constants.InventoryMenuInvSlotStart+i-start] = menu.Slots[i]
}

func menuName(t pk.VarInt) string {
	if int(t) >= 0 && int(t) < len(registryid.Menu) {
		return registryid.Menu[t]
	}
	return ""
}

// slots returns the slots of container id: the inventory menu for 0.
func (m *Manager) slots(id int) []Slot {
	if id == 0 {
		return m.Inventory.Slots[:]
	}
	if c, ok := m.Screens[id].(*Menu); ok {
		return c.Slots
	}
	return nil
}

func (m *Manager) onMerchantOffers(p pk.Packet) error {
	var o play.MerchantOffers
	if err := p.Scan(&o); err != nil {
		return Error{err}
	}
	if menu, ok := m.Screens[int(o.ContainerID)].(*Menu); ok {
		menu.Offers = []types.MerchantOffer(o.Offers)
	}
	return nil
}

// Open returns a copy of the open menu (not the inventory), and whether one is open.
func (m *Manager) Open() (Menu, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, c := range m.Screens {
		if menu, ok := c.(*Menu); ok && id != 0 {
			cp := *menu
			cp.Slots = append([]Slot(nil), menu.Slots...)
			cp.Offers = append([]types.MerchantOffer(nil), menu.Offers...)
			return cp, true
		}
	}
	return Menu{}, false
}

// Slots returns a copy of the slots of container id (0 the inventory menu).
func (m *Manager) Slots(id int) []Slot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Slot(nil), m.slots(id)...)
}

// Close closes the open menu, as Escape does (ServerboundContainerClose).
func (m *Manager) Close(id int) error {
	m.mu.Lock()
	if id != 0 { // the inventory menu is always there: closing it (its 2x2 grid) keeps it
		delete(m.Screens, id)
	}
	m.mu.Unlock()
	c := play.ServerboundContainerClose{ContainerID: pk.VarInt(id)}
	return m.c.Conn.WritePacket(pk.Marshal(c.PacketID(), c))
}

// Click clicks slot of container id as the client does
// (AbstractContainerMenu.clicked, predicted, then ServerboundContainerClick
// with what changed): a left (button 0) or right (1) pick-up click is
// predicted — take, put, merge, swap, take half, put one — and its changes
// sent; any other click goes out with no prediction, and the server sends the
// slots as they became.
func (m *Manager) Click(id, slot, button int, mode types.ContainerInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	slots := m.slots(id)
	changed := ChangedSlots{}
	if mode == types.ContainerInputPickup && slot >= 0 && slot < len(slots) {
		s, c := slots[slot], m.Cursor
		ns, nc := pickup(s, c, button)
		if !sameStack(ns, s) {
			slots[slot] = ns
			changed[int16(slot)] = &slots[slot]
			m.mirror(id, slot)
		}
		m.Cursor = nc
	}
	return m.containerClick(id, slot, button, mode, changed, &m.Cursor)
}

// pickup is a pick-up click's effect on a slot and the cursor.
func pickup(s, cursor Slot, button int) (Slot, Slot) {
	switch {
	case cursor.Count <= 0 && s.Count <= 0:
		return s, cursor
	case cursor.Count <= 0: // take: all, or half rounded up
		n := s.Count
		if button == 1 {
			n = (s.Count + 1) / 2
		}
		taken := s
		taken.Count = n
		s.Count -= n
		if s.Count <= 0 {
			s = Slot{}
		}
		return s, taken
	case s.Count <= 0 || sameItem(s, cursor): // put: all, or one; merge up to the stack size
		n := cursor.Count
		if button == 1 {
			n = 1
		}
		if s.Count <= 0 {
			s = cursor
			s.Count = 0
		}
		room := pk.VarInt(maxStack(s)) - s.Count
		if n > room {
			n = room
		}
		s.Count += n
		cursor.Count -= n
		if cursor.Count <= 0 {
			cursor = Slot{}
		}
		return s, cursor
	default: // different items: swap
		return cursor, s
	}
}

func maxStack(s Slot) int {
	if it, ok := item.ByID[item.ID(s.Item)]; ok {
		return int(it.StackSize)
	}
	return 64
}

// sameItem: the same item with the same component patch (as far as the
// counts of the patch tell).
func sameItem(a, b Slot) bool {
	return a.Item == b.Item && len(a.Components.Positive) == 0 && len(b.Components.Positive) == 0 &&
		len(a.Components.Negative) == 0 && len(b.Components.Negative) == 0
}

func sameStack(a, b Slot) bool {
	return a.Count == b.Count && (a.Count <= 0 || a.Item == b.Item)
}
