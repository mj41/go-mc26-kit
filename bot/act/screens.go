package act

import (
	"context"
	"errors"
	"fmt"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/recipes"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Screens is what a person does in an open screen, one click at a time with
// the ticks between that the server needs to answer: crafting from the recipe
// book, moving stacks with shift-clicks, smelting.
type Screens struct {
	C       *bot.Client
	Ctl     *control.Controller
	Screens *screen.Manager
	Book    *recipes.Book
}

func itemName(id pk.VarInt) string {
	if int(id) >= 0 && int(id) < len(registryid.Item) {
		return registryid.Item[id]
	}
	return ""
}

// current is the open menu's id and slots: the open container, or the
// inventory menu (0) when none is open.
func (s *Screens) current() (int, []screen.Slot, int) {
	// an open menu whose contents have not come yet (or with no inventory
	// part): the inventory menu, which mirrors the player's slots
	if m, ok := s.Screens.Open(); ok && m.PlayerStart() >= 0 {
		return m.ID, m.Slots, m.PlayerStart()
	}
	slots := s.Screens.Slots(0)
	return 0, slots, 9 // the inventory menu: the player's own slots start at 9
}

// Count is how many of item the player has (its inventory part of the open menu).
func (s *Screens) Count(item string) int {
	_, slots, start := s.current()
	n := 0
	for _, sl := range slots[start:] {
		if sl.Count > 0 && itemName(sl.Item) == item {
			n += int(sl.Count)
		}
	}
	return n
}

// Craft makes at least n of item from the recipe book: in the open crafting
// table's grid, or in the inventory's two by two. Each round has the server
// fill the grid with one set (ServerboundPlaceRecipe), waits for the result,
// and shift-clicks it into the inventory. It returns how many it made.
func (s *Screens) Craft(ctx context.Context, item string, n int) (int, error) {
	id, _, _ := s.current()
	rs := s.Book.For(item)
	if len(rs) == 0 {
		return 0, fmt.Errorf("act: no recipe for %s in the book", item)
	}
	made := 0
	for made < n {
		var last error
		ok := false
		for _, r := range rs {
			if id == 0 && (r.Width > 2 || r.Height > 2) {
				continue // a three-wide recipe needs a crafting table
			}
			before := s.Count(item)
			got, err := s.craftOnce(ctx, id, r)
			if err != nil {
				last = err
				continue
			}
			if after := s.Count(item); after > before {
				got = after - before
			}
			made += got
			ok = true
			break
		}
		if !ok {
			if last == nil {
				last = errors.New("act: no recipe fits this grid")
			}
			return made, last
		}
	}
	return made, nil
}

func (s *Screens) craftOnce(ctx context.Context, id int, r recipes.Recipe) (int, error) {
	pr := play.PlaceRecipe{ContainerID: pk.VarInt(id), Recipe: types.RecipeDisplayId{Index: pk.VarInt(r.ID)}}
	if err := s.C.Conn.WritePacket(pk.Marshal(pr.PacketID(), pr)); err != nil {
		return 0, err
	}
	for t := 0; ; t++ {
		if err := s.Ctl.WaitTicks(ctx, 1); err != nil {
			return 0, err
		}
		if slots := s.Screens.Slots(id); len(slots) > 0 && slots[0].Count > 0 {
			break
		}
		if t > 10 {
			return 0, fmt.Errorf("act: the server put no %s in the result slot (missing ingredients?)", r.Result)
		}
	}
	if err := s.Screens.Click(id, 0, 0, types.ContainerInputQuickMove); err != nil {
		return 0, err
	}
	if err := s.Ctl.WaitTicks(ctx, 4); err != nil {
		return 0, err
	}
	return r.Count, nil
}

// Move shift-clicks every stack of item between the open container and the
// player's inventory: toContainer true stores, false takes. It returns how
// many items moved.
func (s *Screens) Move(ctx context.Context, item string, toContainer bool) (int, error) {
	id, slots, start := s.current()
	if id == 0 {
		return 0, errors.New("act: no container is open")
	}
	before := s.Count(item)
	from, to := 0, start
	if toContainer {
		from, to = start, len(slots)
	}
	for i := from; i < to; i++ {
		if slots[i].Count > 0 && itemName(slots[i].Item) == item {
			if err := s.Screens.Click(id, i, 0, types.ContainerInputQuickMove); err != nil {
				return 0, err
			}
			if err := s.Ctl.WaitTicks(ctx, 2); err != nil {
				return 0, err
			}
		}
	}
	if err := s.Ctl.WaitTicks(ctx, 4); err != nil {
		return 0, err
	}
	moved := s.Count(item) - before
	if moved < 0 {
		moved = -moved
	}
	return moved, nil
}

// put moves the whole stack of item at its first slot in the player's part
// of the menu into slot to, by two pick-up clicks.
func (s *Screens) put(ctx context.Context, id int, item string, to int) error {
	_, slots, start := s.current()
	for i := start; i < len(slots); i++ {
		if slots[i].Count > 0 && itemName(slots[i].Item) == item {
			if err := s.Screens.Click(id, i, 0, types.ContainerInputPickup); err != nil {
				return err
			}
			if err := s.Ctl.WaitTicks(ctx, 2); err != nil {
				return err
			}
			if err := s.Screens.Click(id, to, 0, types.ContainerInputPickup); err != nil {
				return err
			}
			return s.Ctl.WaitTicks(ctx, 2)
		}
	}
	return fmt.Errorf("act: no %s to put in", item)
}

// putN puts n of item into slot to of the open menu, as a person does: the
// stack picked up, one right click on the slot for each, the rest put back.
func (s *Screens) putN(ctx context.Context, id int, item string, n, to int) error {
	_, slots, start := s.current()
	for i := start; i < len(slots) && n > 0; i++ {
		if slots[i].Count <= 0 || itemName(slots[i].Item) != item {
			continue
		}
		k := min(n, int(slots[i].Count))
		if err := s.Screens.Click(id, i, 0, types.ContainerInputPickup); err != nil {
			return err
		}
		if err := s.Ctl.WaitTicks(ctx, 2); err != nil {
			return err
		}
		for range k {
			if err := s.Screens.Click(id, to, 1, types.ContainerInputPickup); err != nil {
				return err
			}
		}
		if err := s.Ctl.WaitTicks(ctx, 2); err != nil {
			return err
		}
		if err := s.Screens.Click(id, i, 0, types.ContainerInputPickup); err != nil { // the rest back
			return err
		}
		if err := s.Ctl.WaitTicks(ctx, 2); err != nil {
			return err
		}
		n -= k
		_, slots, start = s.current()
	}
	if n > 0 {
		return fmt.Errorf("act: %d %s short", n, item)
	}
	return nil
}

// Load puts n of input and fuelN of fuel in the open furnace and leaves it
// working — a furnace of several, filled one after another, collected later
// (TakeOutput).
func (s *Screens) Load(ctx context.Context, input string, n int, fuel string, fuelN int) error {
	m, ok := s.Screens.Open()
	if !ok || m.Type != "minecraft:furnace" && m.Type != "minecraft:smoker" && m.Type != "minecraft:blast_furnace" {
		return errors.New("act: no furnace is open")
	}
	if err := s.putN(ctx, m.ID, input, n, 0); err != nil {
		return err
	}
	if fuelN > 0 {
		return s.putN(ctx, m.ID, fuel, fuelN, 1)
	}
	return nil
}

// TakeOutput shift-clicks what the open furnace has made into the inventory
// and returns how many.
func (s *Screens) TakeOutput(ctx context.Context) (int, error) {
	m, ok := s.Screens.Open()
	if !ok || m.Type != "minecraft:furnace" && m.Type != "minecraft:smoker" && m.Type != "minecraft:blast_furnace" {
		return 0, errors.New("act: no furnace is open")
	}
	slots := s.Screens.Slots(m.ID)
	if len(slots) < 3 || slots[2].Count <= 0 {
		return 0, nil
	}
	got := int(slots[2].Count)
	if err := s.Screens.Click(m.ID, 2, 0, types.ContainerInputQuickMove); err != nil {
		return 0, err
	}
	return got, s.Ctl.WaitTicks(ctx, 4)
}

// Smelt puts input and fuel in the open furnace, waits for n results and
// shift-clicks them into the inventory.
func (s *Screens) Smelt(ctx context.Context, input, fuel string, n int) (int, error) {
	m, ok := s.Screens.Open()
	if !ok || m.Type != "minecraft:furnace" && m.Type != "minecraft:smoker" && m.Type != "minecraft:blast_furnace" {
		return 0, errors.New("act: no furnace is open")
	}
	if err := s.put(ctx, m.ID, input, 0); err != nil {
		return 0, err
	}
	if err := s.put(ctx, m.ID, fuel, 1); err != nil {
		return 0, err
	}
	for t := 0; ; t += 10 {
		if err := s.Ctl.WaitTicks(ctx, 10); err != nil {
			return 0, err
		}
		if slots := s.Screens.Slots(m.ID); len(slots) > 2 && int(slots[2].Count) >= n {
			break
		}
		if t > n*200+200 {
			return 0, errors.New("act: the furnace did not make enough")
		}
	}
	slots := s.Screens.Slots(m.ID)
	got := int(slots[2].Count)
	if err := s.Screens.Click(m.ID, 2, 0, types.ContainerInputQuickMove); err != nil {
		return 0, err
	}
	return got, s.Ctl.WaitTicks(ctx, 4)
}

// Trade buys offer number index of the open merchant times times, as a person
// clicking it in the list does: ServerboundSelectTrade has the server put the
// payment from the inventory in the menu, and the result is shift-clicked
// out. It returns how many trades were made.
func (s *Screens) Trade(ctx context.Context, index, times int) (int, error) {
	m, ok := s.Screens.Open()
	if !ok || m.Type != "minecraft:merchant" {
		return 0, errors.New("act: no merchant is open")
	}
	if index < 0 || index >= len(m.Offers) {
		return 0, fmt.Errorf("act: the merchant has %d offers", len(m.Offers))
	}
	made := 0
	for made < times {
		st := play.SelectTrade{Item: pk.VarInt(index)}
		if err := s.C.Conn.WritePacket(pk.Marshal(st.PacketID(), st)); err != nil {
			return made, err
		}
		got := false
		for t := 0; t < 10 && !got; t++ {
			if err := s.Ctl.WaitTicks(ctx, 1); err != nil {
				return made, err
			}
			if slots := s.Screens.Slots(m.ID); len(slots) > 2 && slots[2].Count > 0 {
				got = true
			}
		}
		if !got {
			return made, errors.New("act: the trade gave nothing (no payment?)")
		}
		if err := s.Screens.Click(m.ID, 2, 0, types.ContainerInputPickup); err != nil {
			return made, err
		}
		if err := s.Ctl.WaitTicks(ctx, 2); err != nil {
			return made, err
		}
		// the bought stack is on the cursor: into the first free inventory slot
		free := -1
		slots := s.Screens.Slots(m.ID)
		for i := max(len(slots)-36, 0); i < len(slots); i++ { // the inventory part (none: no room)
			if slots[i].Count <= 0 {
				free = i
				break
			}
		}
		if free < 0 {
			return made, errors.New("act: no room for what was bought")
		}
		if err := s.Screens.Click(m.ID, free, 0, types.ContainerInputPickup); err != nil {
			return made, err
		}
		if err := s.Ctl.WaitTicks(ctx, 2); err != nil {
			return made, err
		}
		made++
	}
	return made, nil
}

// CraftByHand makes a recipe the book does not show — the server unlocks a
// recipe for the book only on its advancement (a torch needs a stone pickaxe
// first) — as a person does who knows it: each ingredient picked up, laid
// into its cell with right clicks, one per craft, the rest put back; then the
// result shift-clicked out. cells are the recipe's grid row by row, width
// wide, "" for an empty cell; the open crafting table's grid, or the
// inventory's two by two. It returns how many of the result it made.
func (s *Screens) CraftByHand(ctx context.Context, cells []string, width, times int) (int, error) {
	id, _, start := s.current()
	gw := 2
	if id != 0 {
		gw = 3
	}
	height := (len(cells) + width - 1) / width
	if width > gw || height > gw {
		return 0, fmt.Errorf("act: a %dx%d recipe does not fit a %dx%d grid", width, height, gw, gw)
	}
	var result string
	for i, item := range cells {
		if item == "" {
			continue
		}
		cell := 1 + (i/width)*gw + i%width
		for laid := 0; laid < times; {
			_, slots, _ := s.current()
			from := -1
			for j := start; j < len(slots); j++ {
				if slots[j].Count > 0 && itemName(slots[j].Item) == item {
					from = j
					break
				}
			}
			if from < 0 {
				return 0, fmt.Errorf("act: no %s left for the grid", item)
			}
			if err := s.Screens.Click(id, from, 0, types.ContainerInputPickup); err != nil {
				return 0, err
			}
			for laid < times && s.cursorCount() > 0 {
				if err := s.Screens.Click(id, cell, 1, types.ContainerInputPickup); err != nil {
					return 0, err
				}
				laid++
			}
			if s.cursorCount() > 0 {
				if err := s.Screens.Click(id, from, 0, types.ContainerInputPickup); err != nil {
					return 0, err
				}
			}
			if err := s.Ctl.WaitTicks(ctx, 2); err != nil {
				return 0, err
			}
		}
	}
	// the server works out the result
	for t := 0; ; t++ {
		if err := s.Ctl.WaitTicks(ctx, 1); err != nil {
			return 0, err
		}
		if slots := s.Screens.Slots(id); len(slots) > 0 && slots[0].Count > 0 {
			result = itemName(slots[0].Item)
			break
		}
		if t > 10 {
			return 0, errors.New("act: the grid makes nothing")
		}
	}
	before := s.Count(result)
	if err := s.Screens.Click(id, 0, 0, types.ContainerInputQuickMove); err != nil {
		return 0, err
	}
	if err := s.Ctl.WaitTicks(ctx, 4); err != nil {
		return 0, err
	}
	return s.Count(result) - before, nil
}

// cursorCount is how many items the cursor holds, as the client has it.
func (s *Screens) cursorCount() int {
	s.Screens.Lock()
	defer s.Screens.Unlock()
	return int(s.Screens.Cursor.Count)
}
