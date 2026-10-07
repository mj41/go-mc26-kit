// Package advancements follows a player's advancements as the server tells
// them (ClientboundUpdateAdvancements): each one's display (its title,
// description and frame, in English), its criteria and requirements, and its
// progress across packets — and tells when one with a display becomes done.
// A recipe's advancement (minecraft:recipes/…) has no display: it is followed
// but never told.
package advancements

import (
	"reflect"
	"strings"
	"sync"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Display is how the game shows an advancement: its title and description
// (English, from the game's en_us text) and its frame (task, goal, challenge).
type Display struct {
	Title, Description, Frame string
}

// Advancement is one advancement as the server described it, and how far the
// player is with it.
type Advancement struct {
	ID           string
	Parent       string
	Display      *Display   // nil for one not shown (a recipe's)
	Requirements [][]string // every group needs one of its criteria
	Obtained     map[string]bool
}

// Done reports whether every requirement is met: each group has a criterion
// obtained. One with no requirements is done once its progress came.
func (a *Advancement) Done() bool {
	for _, group := range a.Requirements {
		met := false
		for _, c := range group {
			if a.Obtained[c] {
				met = true
				break
			}
		}
		if !met {
			return false
		}
	}
	return true
}

// Tracker follows the advancements of a client's player.
type Tracker struct {
	mu       sync.Mutex
	adv      map[string]*Advancement
	progress map[string]bool // progress came for it
	told     map[string]bool // told done once: not again (across resets)
	joined   bool            // the first packet with reset is the join's
	onDone   func(a Advancement, initial bool)
}

// New follows the advancements c's server sends; onDone is called once for
// each advancement with a display that is done — initial for those already
// done when the player joined (the first packet with reset).
func New(c *bot.Client, onDone func(a Advancement, initial bool)) *Tracker {
	t := NewTracker(onDone)
	c.Events.AddListener(bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayUpdateAdvancements, F: func(p pk.Packet) error {
		var u play.UpdateAdvancements
		if err := p.Scan(&u); err != nil {
			return err
		}
		t.Update(u)
		return nil
	}})
	return t
}

// NewTracker is a Tracker not tied to a client: Update feeds it.
func NewTracker(onDone func(a Advancement, initial bool)) *Tracker {
	return &Tracker{adv: map[string]*Advancement{}, progress: map[string]bool{}, told: map[string]bool{}, onDone: onDone}
}

// Update applies one ClientboundUpdateAdvancements (apply).
func (t *Tracker) Update(u play.UpdateAdvancements) { t.apply(read(u)) }

// change is one update as every library version can say it.
type change struct {
	reset    bool
	removed  []string
	added    []*Advancement
	progress []criteria
}

// criteria is an advancement's progress: each criterion, obtained or not.
type criteria struct {
	id       string
	obtained map[string]bool
}

// read reads the packet field by field: its shape differs between the
// library versions (Reset / ShouldReset; an added advancement alone or
// positioned; its requirements a list or a struct holding one; a criterion's
// progress an option or a struct holding one), and this one source builds
// against every supported version.
func read(u play.UpdateAdvancements) change {
	var c change
	pv := reflect.ValueOf(u)
	if f, ok := fieldOf(pv, "ShouldReset", "Reset"); ok {
		c.reset = f.Bool()
	}
	for _, id := range u.Removed {
		c.removed = append(c.removed, string(id))
	}
	added, _ := fieldOf(pv, "Added")
	for i := 0; added.IsValid() && i < added.Len(); i++ {
		hv := added.Index(i)
		if inner, ok := fieldOf(hv, "Advancement"); ok { // 26.3: positioned
			hv = inner
		}
		h, ok := hv.Interface().(types.AdvancementHolder)
		if !ok {
			continue
		}
		a := &Advancement{ID: string(h.ID), Obtained: map[string]bool{}}
		if h.Value.Parent.Has {
			a.Parent = string(h.Value.Parent.Val)
		}
		if h.Value.Display.Has {
			d := h.Value.Display.Val
			a.Display = &Display{Title: d.Title.ClearString(), Description: d.Description.ClearString(), Frame: d.Frame.Name()}
		}
		reqs, _ := fieldOf(reflect.ValueOf(h.Value), "Requirements")
		if inner, ok := fieldOf(reqs, "Requirements"); ok { // before 26.3: wrapped
			reqs = inner
		}
		for gi := 0; reqs.IsValid() && reqs.Kind() == reflect.Slice && gi < reqs.Len(); gi++ {
			group := reqs.Index(gi)
			var g []string
			for ci := 0; group.Kind() == reflect.Slice && ci < group.Len(); ci++ {
				g = append(g, group.Index(ci).String())
			}
			a.Requirements = append(a.Requirements, g)
		}
		c.added = append(c.added, a)
	}
	progress, _ := fieldOf(pv, "Progress")
	for i := 0; progress.IsValid() && i < progress.Len(); i++ {
		e := progress.Index(i)
		key, _ := fieldOf(e, "Key")
		crit, _ := fieldOf(e, "Val")
		if inner, ok := fieldOf(crit, "Criteria"); ok { // 26.3: AdvancementProgress
			crit = inner
		}
		p := criteria{id: key.String(), obtained: map[string]bool{}}
		for j := 0; crit.IsValid() && crit.Kind() == reflect.Slice && j < crit.Len(); j++ {
			ce := crit.Index(j)
			name, _ := fieldOf(ce, "Key")
			val, _ := fieldOf(ce, "Val")
			if o, ok := fieldOf(val, "Obtained"); ok { // before 26.3: CriterionProgress
				val = o
			}
			has, _ := fieldOf(val, "Has")
			p.obtained[name.String()] = has.IsValid() && has.Bool()
		}
		c.progress = append(c.progress, p)
	}
	return c
}

// apply applies one update, as the game's client does
// (ClientAdvancements.update): with reset, all forgotten first; the removed
// dropped; the added kept; each progress sent replacing the one before.
func (t *Tracker) apply(c change) {
	t.mu.Lock()
	initial := false
	if c.reset {
		t.adv, t.progress = map[string]*Advancement{}, map[string]bool{}
		if !t.joined {
			t.joined, initial = true, true
		}
	}
	for _, id := range c.removed {
		delete(t.adv, id)
		delete(t.progress, id)
	}
	for _, a := range c.added {
		if a.Obtained == nil {
			a.Obtained = map[string]bool{}
		}
		if old, ok := t.adv[a.ID]; ok {
			a.Obtained = old.Obtained // a description again: the progress stays
		}
		t.adv[a.ID] = a
	}
	for _, p := range c.progress {
		a, ok := t.adv[p.id]
		if !ok {
			continue
		}
		a.Obtained = p.obtained
		t.progress[a.ID] = true
	}
	var done []Advancement
	for id, a := range t.adv {
		if a.Display == nil || strings.HasPrefix(id, "minecraft:recipes/") || !t.progress[id] || t.told[id] || !a.Done() {
			continue
		}
		t.told[id] = true
		done = append(done, *a)
	}
	t.mu.Unlock()
	for _, a := range done {
		if t.onDone != nil {
			t.onDone(a, initial)
		}
	}
}

// Get is the advancement id as it stands, and whether the server told of it.
func (t *Tracker) Get(id string) (Advancement, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	a, ok := t.adv[id]
	if !ok {
		return Advancement{}, false
	}
	return *a, true
}

// fieldOf is the first of the named fields v (a struct) has.
func fieldOf(v reflect.Value, names ...string) (reflect.Value, bool) {
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	for _, n := range names {
		if f := v.FieldByName(n); f.IsValid() {
			return f, true
		}
	}
	return reflect.Value{}, false
}
