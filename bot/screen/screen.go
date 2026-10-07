package screen

import (
	"errors"
	"sync"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/constants"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

type Manager struct {
	c *bot.Client

	// mu is held while a packet changes the screens; hold it (Lock, Unlock)
	// to read them from another goroutine.
	mu        sync.Mutex
	Screens   map[int]Container
	Inventory Inventory
	Cursor    Slot
	// HeldSlot is the selected hotbar slot, 0–8 (Inventory.selected).
	HeldSlot int
	// SlotUpdates counts the single slots the server sent (container_set_slot):
	// after a click the client predicted right, none come.
	SlotUpdates int
	events      EventsListener
	// The last received State ID from server
	stateID int32
}

// Lock and Unlock hold the screens for reading from another goroutine.
func (m *Manager) Lock()   { m.mu.Lock() }
func (m *Manager) Unlock() { m.mu.Unlock() }

// locked runs a packet handler with the manager held.
func (m *Manager) locked(f func(pk.Packet) error) func(pk.Packet) error {
	return func(p pk.Packet) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		return f(p)
	}
}

func NewManager(c *bot.Client, e EventsListener) *Manager {
	m := &Manager{
		c:       c,
		Screens: make(map[int]Container),
		events:  e,
	}
	m.Screens[0] = &m.Inventory
	c.Events.AddListener(
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayOpenScreen, F: m.locked(m.onOpenScreen)},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayContainerSetContent, F: m.locked(m.onSetContentPacket)},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayContainerClose, F: m.locked(m.onCloseScreen)},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayContainerSetSlot, F: m.locked(m.onSetSlot)},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlaySetPlayerInventory, F: m.locked(m.onSetPlayerInventory)},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlaySetHeldSlot, F: m.locked(m.onSetHeldSlot)},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayMerchantOffers, F: m.locked(m.onMerchantOffers)},
	)
	return m
}

// ChangedSlots lists the slots a click changed on the client's side, by slot
// number; a nil Slot means the slot became empty.
type ChangedSlots map[int16]*Slot

// ContainerClick sends a click on slot of container id. changed and carried
// (the cursor after the click) are sent as hashed stacks. Component hashes are
// not computed here: a stack with components goes out with empty component
// maps, and the server answers the mismatch by re-sending the slot.
func (m *Manager) ContainerClick(id int, slot int16, button byte, mode types.ContainerInput, changed ChangedSlots, carried *Slot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.containerClick(id, int(slot), int(button), mode, changed, carried)
}

func (m *Manager) containerClick(id, slot, button int, mode types.ContainerInput, changed ChangedSlots, carried *Slot) error {
	click := play.ContainerClick{
		ContainerID:    pk.VarInt(id),
		StateID:        pk.VarInt(m.stateID),
		SlotNum:        pk.Short(slot),
		ButtonNum:      pk.Byte(button),
		ContainerInput: mode,
		ChangedSlots:   make(types.Map[pk.Short, *pk.Short, hashedStack, *hashedStack], 0, len(changed)),
		CarriedItem:    m.hashedStackOf(carried),
	}
	for i, s := range changed {
		click.ChangedSlots = append(click.ChangedSlots, types.Entry[pk.Short, hashedStack]{Key: pk.Short(i), Val: m.hashedStackOf(s)})
	}
	return m.c.Conn.WritePacket(pk.Marshal(click.PacketID(), click))
}

// hashedStack is the wire form of a slot in a click: absent for an empty slot.
type hashedStack = pk.Option[types.HashedStackActualItem, *types.HashedStackActualItem]

// hashedStackOf is a slot as a click carries it: the item, the count and the
// stack's patch — the hash of every added component (component.Hash; one it
// cannot hash goes out as the hash of nothing, and the server answers by
// sending the slot), the ids of the removed ones.
func (m *Manager) hashedStackOf(s *Slot) hashedStack {
	if s == nil || s.Count <= 0 {
		return hashedStack{}
	}
	names := func(registry string, id int32) (string, bool) { return m.c.Registries.Registry(registry).KeyOf(id) }
	v := types.HashedStackActualItem{Item: s.Item, Count: s.Count}
	for _, c := range s.Components.Positive {
		h, _ := component.HashWith(c.Value, names)
		v.Components.AddedComponents = append(v.Components.AddedComponents, types.Entry[pk.VarInt, pk.Int]{Key: c.Type, Val: pk.Int(h)})
	}
	v.Components.RemovedComponents = append(v.Components.RemovedComponents, s.Components.Negative...)
	return hashedStack{Has: true, Val: v}
}

func (m *Manager) onOpenScreen(p pk.Packet) error {
	var os play.OpenScreen
	if err := p.Scan(&os); err != nil {
		return Error{err}
	}
	id := int(os.ContainerID)
	m.Screens[id] = &Menu{ID: id, Type: menuName(os.Type), Title: chat.Message(os.Title)}
	if m.events.Open != nil {
		if err := m.events.Open(id, int32(os.Type), chat.Message(os.Title)); err != nil {
			return Error{err}
		}
	}
	return nil
}

func (m *Manager) onSetContentPacket(p pk.Packet) error {
	var sc play.ContainerSetContent
	if err := p.Scan(&sc); err != nil {
		return Error{err}
	}
	ContainerID, StateID := sc.ContainerID, sc.StateID
	SlotData := []Slot(sc.Items)
	m.Cursor = sc.CarriedItem
	m.stateID = int32(StateID)
	// copy the slot data to container
	container, ok := m.Screens[int(ContainerID)]
	if !ok {
		// Unknown container ID: the server may send spurious updates for containers
		// the bot hasn't opened (e.g., after death/respawn or dimension change).
		// Silently ignore rather than crashing HandleGame.
		return nil
	}
	for i, v := range SlotData {
		err := container.onSetSlot(i, v)
		if err != nil {
			return Error{err}
		}
		m.mirror(int(ContainerID), i)
		if m.events.SetSlot != nil {
			if err := m.events.SetSlot(int(ContainerID), i); err != nil {
				return Error{err}
			}
		}
	}
	return nil
}

func (m *Manager) onCloseScreen(p pk.Packet) error {
	var cc play.ClientboundContainerClose
	if err := p.Scan(&cc); err != nil {
		return Error{err}
	}
	ContainerID := cc.ContainerID
	if c, ok := m.Screens[int(ContainerID)]; ok {
		delete(m.Screens, int(ContainerID))
		if err := c.onClose(); err != nil {
			return Error{err}
		}
		if m.events.Close != nil {
			if err := m.events.Close(int(ContainerID)); err != nil {
				return Error{err}
			}
		}
	}
	return nil
}

func (m *Manager) onSetSlot(p pk.Packet) (err error) {
	var ss play.ContainerSetSlot
	if err := p.Scan(&ss); err != nil {
		return Error{err}
	}
	ContainerID, StateID, SlotID := ss.ContainerID, ss.StateID, ss.Slot
	SlotData := ss.ItemStack

	m.stateID = int32(StateID)
	m.SlotUpdates++
	if ContainerID == -1 && SlotID == -1 {
		m.Cursor = SlotData
	} else if ContainerID == -2 {
		err = m.Inventory.onSetSlot(int(SlotID), SlotData)
	} else if c, ok := m.Screens[int(ContainerID)]; ok {
		err = c.onSetSlot(int(SlotID), SlotData)
		m.mirror(int(ContainerID), int(SlotID))
	}

	if m.events.SetSlot != nil {
		if err := m.events.SetSlot(int(ContainerID), int(SlotID)); err != nil {
			return Error{err}
		}
	}
	if err != nil {
		return Error{err}
	}
	return nil
}

// onSetPlayerInventory handles ClientboundSetPlayerInventory. Its slot is an
// index of the player's Inventory (0–8 the hotbar, 9–35 the rest, 36–39 the
// armor from the feet up, 40 the offhand), not of the inventory menu, which
// is what Inventory.Slots holds; MenuSlot converts.
func (m *Manager) onSetPlayerInventory(p pk.Packet) error {
	var spi play.SetPlayerInventory
	if err := p.Scan(&spi); err != nil {
		return Error{err}
	}
	slot, ok := MenuSlot(int(spi.Slot))
	if !ok {
		return nil // body armor and saddle have no slot in the player's menu
	}
	if err := m.Inventory.onSetSlot(slot, spi.Contents); err != nil {
		return Error{err}
	}
	if m.events.SetSlot != nil {
		if err := m.events.SetSlot(0, slot); err != nil {
			return Error{err}
		}
	}
	return nil
}

// MenuSlot converts an index of the player's Inventory to the slot of the
// inventory menu that shows it (InventoryMenu's constructor).
func MenuSlot(index int) (int, bool) {
	switch {
	case index >= 0 && index < constants.InventorySelectionSize: // the hotbar
		return constants.InventoryMenuUseRowSlotStart + index, true
	case index < constants.InventoryInventorySize: // the rest of the main inventory
		return index, true
	case index < constants.InventoryInventorySize+4: // feet, legs, chest, head → head first in the menu
		return constants.InventoryMenuArmorSlotStart + 3 - (index - constants.InventoryInventorySize), true
	case index == constants.InventorySlotOffhand:
		return constants.InventoryMenuShieldSlot, true
	}
	return 0, false
}

// onSetHeldSlot is ClientboundSetHeldSlot: the server selects a hotbar slot.
func (m *Manager) onSetHeldSlot(p pk.Packet) error {
	var h play.SetHeldSlot
	if err := p.Scan(&h); err != nil {
		return Error{err}
	}
	if h.Slot >= 0 && int(h.Slot) < constants.InventorySelectionSize {
		m.HeldSlot = int(h.Slot)
	}
	return nil
}

// SelectHotbar selects a hotbar slot, as scrolling or a number key does
// (ServerboundSetCarriedItem).
func (m *Manager) SelectHotbar(slot int) error {
	if slot < 0 || slot >= constants.InventorySelectionSize {
		return Error{errors.New("not a hotbar slot")}
	}
	m.mu.Lock()
	m.HeldSlot = slot
	m.mu.Unlock()
	c := play.SetCarriedItem{Slot: pk.Short(slot)}
	return m.c.Conn.WritePacket(pk.Marshal(c.PacketID(), c))
}

// Slot is one slot of a container: the item stack the server sent, with its
// count, item id and component patch (types.ItemStack); count 0 is empty.
type Slot = types.ItemStack

type Container interface {
	onSetSlot(i int, s Slot) error
	onClose() error
}

type Error struct {
	Err error
}

func (e Error) Error() string {
	return "bot/screen: " + e.Err.Error()
}

func (e Error) Unwrap() error {
	return e.Err
}
