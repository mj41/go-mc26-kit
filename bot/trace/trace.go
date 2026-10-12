// Package trace records what a client was told in a window of its game, reduced
// to what changed: a block became another, a slot holds an item, an entity came
// or went, an item was picked up, the health moved — each at its tick from the
// window's start. A window is an action of a bot (a dig, a craft): two games
// that play the same action are alike when their traces are — the same
// changes in the same order, each within a tick or two. Chunk data, light,
// sounds and particles are left out: they are not what an action changes.
package trace

import (
	"fmt"
	"log"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Change is one reduced change. Kinds: "block" (At, Name: the block it became),
// "slot" (Container, Slot, Name, Count), "entity" (Name: its type, At: where it
// came, Item: what an item entity holds), "gone" (Name: the type of the entity
// that went), "pickup" (Count), "health" (Health, Food).
type Change struct {
	Tick      int     `json:"t"`
	Kind      string  `json:"k"`
	At        *[3]int `json:"at,omitempty"`
	Name      string  `json:"name,omitempty"`
	Item      string  `json:"item,omitempty"`
	Count     int     `json:"count,omitempty"`
	Container int     `json:"container,omitempty"`
	Slot      int     `json:"slot,omitempty"`
	Health    float32 `json:"health,omitempty"`
	Food      int     `json:"food,omitempty"`
}

// Window is a recorded action: its label and the changes in it.
type Window struct {
	Label   string   `json:"label"`
	Changes []Change `json:"changes"`
}

// Recorder listens to a client and keeps what it was told, reduced, for the
// last two minutes of its ticks: a window is cut from it when an action ends.
type Recorder struct {
	// Block says the block the client has at a place (its name), before an update changes it: an update that leaves
	// the name as it was — a leaf whose distance moved, a crop's age — is no change of the trace. Nil: every update counts.
	Block func(x, y, z int) (name string, ok bool)
	// Near says where the bot is; a block change farther than Radius from it is not the action's (a far section
	// updated, a tree's leaves decaying across the wood). Nil: no limit.
	Near   func() (x, y, z float64)
	Radius float64

	ticks   func() uint64
	mu      sync.Mutex
	log     []Change         // Tick is the client's tick count (absolute) while kept
	kind    map[int32]string // the type of each entity seen: a removal names its type
	item    map[int32]string // what an item entity holds
	dropped int              // changes not kept for want of room
}

// keep is how many ticks of changes the recorder holds.
const keep = 2400

// maxChanges is the most changes a window holds, and the log: past it the rest are counted, not kept, and
// said — a trace is read by a person, and the store pays for its size.
const maxChanges = 512

// New records what c is told; ticks is the client's tick count (the controller's).
func New(c *bot.Client, ticks func() uint64) *Recorder {
	r := &Recorder{ticks: ticks, kind: map[int32]string{}, item: map[int32]string{}}
	c.Events.AddListener(
		bot.PacketHandler{Priority: 90, ID: packetid.ClientboundPlayBlockUpdate, F: r.onBlockUpdate},
		bot.PacketHandler{Priority: 90, ID: packetid.ClientboundPlaySectionBlocksUpdate, F: r.onSection},
		bot.PacketHandler{Priority: 90, ID: packetid.ClientboundPlayContainerSetSlot, F: r.onSlot},
		bot.PacketHandler{Priority: 90, ID: packetid.ClientboundPlayAddEntity, F: r.onAdd},
		bot.PacketHandler{Priority: 90, ID: packetid.ClientboundPlaySetEntityData, F: r.onData},
		bot.PacketHandler{Priority: 90, ID: packetid.ClientboundPlayRemoveEntities, F: r.onRemove},
		bot.PacketHandler{Priority: 90, ID: packetid.ClientboundPlayTakeItemEntity, F: r.onTake},
		bot.PacketHandler{Priority: 90, ID: packetid.ClientboundPlaySetHealth, F: r.onHealth},
	)
	return r
}

// Mark is the client's tick now: where a window begins.
func (r *Recorder) Mark() uint64 { return r.ticks() }

// Since cuts the window from the tick (a Mark) to now: the changes in it, their
// ticks counted from the mark.
func (r *Recorder) Since(label string, from uint64) Window {
	r.mu.Lock()
	defer r.mu.Unlock()
	w := Window{Label: label}
	cut := 0
	for _, c := range r.log {
		if uint64(c.Tick) >= from {
			if len(w.Changes) >= maxChanges {
				cut++
				continue
			}
			c.Tick -= int(from)
			w.Changes = append(w.Changes, c)
		}
	}
	if cut > 0 {
		// loudly: the trace is cut, and the cut is in it
		log.Printf("trace: %q has %d changes past the %d kept: cut (what changes so often is noise to find and drop)", label, cut, maxChanges)
		w.Changes = append(w.Changes, Change{Tick: -1, Kind: "truncated", Count: cut})
	}
	return w
}

// note adds a change at the tick it was told; the old are dropped.
func (r *Recorder) note(c Change) {
	now := r.ticks()
	c.Tick = int(now)
	if len(r.log) >= 8*maxChanges {
		r.dropped++
		return
	}
	r.log = append(r.log, c)
	if len(r.log) > 0 && uint64(r.log[0].Tick)+keep < now {
		i := 0
		for i < len(r.log) && uint64(r.log[i].Tick)+keep < now {
			i++
		}
		r.log = r.log[i:]
	}
}

func blockName(s block.StateID) string {
	if int(s) < 0 || int(s) >= len(block.StateList) {
		return fmt.Sprintf("unknown state %d", s)
	}
	return block.StateList[s].ID()
}

func itemName(id int32) string {
	if id >= 0 && int(id) < len(registryid.Item) {
		return registryid.Item[id]
	}
	return fmt.Sprintf("item %d", id)
}

func (r *Recorder) onBlockUpdate(p pk.Packet) error {
	var u play.BlockUpdate
	if err := p.Scan(&u); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.block(u.Pos.X, u.Pos.Y, u.Pos.Z, blockName(block.StateID(u.BlockState)))
	return nil
}

// block notes a block update when it changes the block's name and is near enough.
func (r *Recorder) block(x, y, z int, name string) {
	if r.Block != nil {
		if old, ok := r.Block(x, y, z); ok && old == name {
			return
		}
	}
	if r.Near != nil && r.Radius > 0 {
		bx, by, bz := r.Near()
		if math.Abs(float64(x)+0.5-bx) > r.Radius || math.Abs(float64(y)+0.5-by) > r.Radius || math.Abs(float64(z)+0.5-bz) > r.Radius {
			return
		}
	}
	r.note(Change{Kind: "block", At: &[3]int{x, y, z}, Name: name})
}

func (r *Recorder) onSection(p pk.Packet) error {
	var u play.SectionBlocksUpdate
	if err := p.Scan(&u); err != nil {
		return err
	}
	// the section's corner: x, z, y packed in 22, 22 and 20 bits (SectionPos.asLong)
	sx, sz, sy := int(int64(u.SectionPos)>>42), int(int64(u.SectionPos)<<22>>42), int(int64(u.SectionPos)<<44>>44)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range u.States {
		local := int(s) & 0xfff
		r.block(sx<<4+local>>8&15, sy<<4+local&15, sz<<4+local>>4&15, blockName(block.StateID(int64(s)>>12)))
	}
	return nil
}

func (r *Recorder) onSlot(p pk.Packet) error {
	var s play.ContainerSetSlot
	if err := p.Scan(&s); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c := Change{Kind: "slot", Container: int(s.ContainerID), Slot: int(s.Slot)}
	if s.ItemStack.Count > 0 {
		c.Name, c.Count = itemName(int32(s.ItemStack.Item)), int(s.ItemStack.Count)
	}
	r.note(c)
	return nil
}

func (r *Recorder) onAdd(p pk.Packet) error {
	var a play.AddEntity
	if err := p.Scan(&a); err != nil {
		return err
	}
	name := ""
	if int(a.Type) < len(registryid.EntityType) {
		name = registryid.EntityType[a.Type]
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kind[int32(a.ID)] = name
	r.note(Change{Kind: "entity", Name: name, At: &[3]int{int(math.Floor(float64(a.X))), int(math.Floor(float64(a.Y))), int(math.Floor(float64(a.Z)))}})
	return nil
}

// onData learns what an item entity holds (the stack in its entity data, index 8)
// and tells it on the entity change that came with it.
func (r *Recorder) onData(p pk.Packet) error {
	var d play.SetEntityData
	if err := p.Scan(&d); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range d.PackedItems {
		if st, ok := v.Value.(*types.ItemStack); ok && v.Index == 8 && st.Count > 0 {
			name := itemName(int32(st.Item))
			r.item[int32(d.ID)] = name
			// the item of the entity just added
			for i := len(r.log) - 1; i >= 0 && i >= len(r.log)-8; i-- {
				if r.log[i].Kind == "entity" && r.log[i].Item == "" && r.log[i].Name == "minecraft:item" {
					r.log[i].Item, r.log[i].Count = name, int(st.Count)
					break
				}
			}
		}
	}
	return nil
}

func (r *Recorder) onRemove(p pk.Packet) error {
	var rm play.RemoveEntities
	if err := p.Scan(&rm); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range rm.EntityIds {
		r.note(Change{Kind: "gone", Name: r.kind[int32(id)], Item: r.item[int32(id)]})
		delete(r.kind, int32(id))
		delete(r.item, int32(id))
	}
	return nil
}

func (r *Recorder) onTake(p pk.Packet) error {
	var t play.TakeItemEntity
	if err := p.Scan(&t); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.note(Change{Kind: "pickup", Item: r.item[int32(t.ItemID)], Count: int(t.Amount)})
	return nil
}

func (r *Recorder) onHealth(p pk.Packet) error {
	var h play.SetHealth
	if err := p.Scan(&h); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.note(Change{Kind: "health", Health: float32(h.Health), Food: int(h.Food)})
	return nil
}

// same reports whether two changes are the same: the kind, the names, the place, the
// counts — the tick aside, an entity's id (renumbered in each game) and where an
// entity came (a dropped item lies at a place the game chooses at random).
func same(a, b Change) bool {
	if a.Kind == "entity" && b.Kind == "entity" {
		a.At, b.At = nil, nil
	}
	return a.Kind == b.Kind && a.Name == b.Name && a.Item == b.Item && a.Count == b.Count &&
		a.Container == b.Container && a.Slot == b.Slot && a.Health == b.Health && a.Food == b.Food &&
		(a.At == nil && b.At == nil || a.At != nil && b.At != nil && *a.At == *b.At)
}

// Compare says how the trace got differs from the trace want (a real game's): the
// same changes in the same order, each within slack ticks of its time. Each
// difference is a line; none means the traces agree.
func Compare(want, got []Change, slack int) []string {
	var diffs []string
	n := max(len(want), len(got))
	for i := 0; i < n; i++ {
		switch {
		case i >= len(got):
			diffs = append(diffs, fmt.Sprintf("missing: %s", describe(want[i])))
		case i >= len(want):
			diffs = append(diffs, fmt.Sprintf("extra: %s", describe(got[i])))
		case !same(want[i], got[i]):
			diffs = append(diffs, fmt.Sprintf("change %d: want %s, got %s", i, describe(want[i]), describe(got[i])))
		case got[i].Tick < want[i].Tick-slack || got[i].Tick > want[i].Tick+slack:
			diffs = append(diffs, fmt.Sprintf("change %d (%s): at tick %d, want %d ± %d", i, describe(want[i]), got[i].Tick, want[i].Tick, slack))
		}
	}
	return diffs
}

func describe(c Change) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("t+%d", c.Tick), c.Kind)
	if c.At != nil {
		parts = append(parts, fmt.Sprint(*c.At))
	}
	if c.Kind == "slot" {
		parts = append(parts, fmt.Sprintf("container %d slot %d", c.Container, c.Slot))
	}
	if c.Name != "" {
		parts = append(parts, c.Name)
	}
	if c.Item != "" {
		parts = append(parts, c.Item)
	}
	if c.Count != 0 {
		parts = append(parts, fmt.Sprintf("×%d", c.Count))
	}
	if c.Kind == "health" {
		parts = append(parts, fmt.Sprintf("%.1f food %d", c.Health, c.Food))
	}
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), " ")
}
