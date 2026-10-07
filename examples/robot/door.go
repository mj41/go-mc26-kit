package main

import (
	"fmt"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// The shelter's door: the first nights the way in is shut with cobblestone
// and dug open in the morning; coming up at the end of the first night (its
// stairs dug, the morning come) the robot makes a wooden door of the wood it
// carries and hangs it in the way in, as a person does once they have
// settled. From then on in closes it and out opens it — and shuts it behind.

// doorAt reports whether a door's lower half is at p, and whether it is open.
func (r *robot) doorAt(p world.BlockPos) (door, open bool) {
	s, ok := r.world.BlockAt(p)
	if !ok || int(s) >= len(block.StateList) {
		return false, false
	}
	if !strings.HasSuffix(block.StateList[s].ID(), "_door") {
		return false, false
	}
	return true, strings.Contains(world.StateString(s), "open=true")
}

// setDoor opens or shuts the door at p (a use when it is not as wanted).
func (r *robot) setDoor(p world.BlockPos, open bool) error {
	door, is := r.doorAt(p)
	if !door {
		return fmt.Errorf("no door at %v", p)
	}
	if is == open {
		return nil
	}
	if err := r.reach(p); err != nil {
		return err
	}
	_, err := waitUse(r, func(done func(act.Use)) { r.hands.UseBlock(p, done) })
	return err
}

// doorWood is the wood its door is made of: the planks it carries, else its
// logs (oak when it has none of either: the recipe asks for planks).
func (r *robot) doorWood() string {
	for _, it := range r.inventoryItems() {
		if w, ok := strings.CutSuffix(it, "_planks"); ok && strings.HasPrefix(w, "minecraft:") {
			return w
		}
	}
	for _, it := range r.inventoryItems() {
		if w, ok := strings.CutSuffix(it, "_log"); ok && strings.HasPrefix(w, "minecraft:") && !strings.Contains(w, "stripped") {
			return w
		}
	}
	return "minecraft:oak"
}

// hangDoor makes a door and puts it in the shelter's way in, standing inside
// in the room, looking out: the door shut, across the way in.
func (r *robot) hangDoor(m room) error {
	way := m.cell(1, 0, 0)
	if door, _ := r.doorAt(way); door {
		return nil
	}
	kind := r.doorWood() + "_door"
	if r.ui.Count(kind) == 0 {
		if err := r.get(kind, 1, 1); err != nil {
			return fmt.Errorf("a door: %w", err)
		}
	}
	// from inside: the cobblestone the way in was shut with taken out
	if err := r.goTo(m.cell(2, 0, 0)); err != nil {
		return fmt.Errorf("to the way in: %w", err)
	}
	for _, dy := range []int{0, 1} {
		if err := r.clear(m.cell(1, 0, dy)); err != nil {
			return fmt.Errorf("the way in: %w", err)
		}
	}
	if _, err := r.toHotbar(kind, true); err != nil {
		return err
	}
	if _, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(way, done) }); err != nil {
		return fmt.Errorf("the door at %v: %w", way, err)
	}
	if door, _ := r.doorAt(way); !door {
		return fmt.Errorf("no door at %v after it was put", way)
	}
	plan("shelter: a door hung in the way in at %v", way)
	return r.setDoor(way, false)
}

// inventoryItems are the items it carries, each once.
func (r *robot) inventoryItems() []string {
	r.screens.Lock()
	defer r.screens.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, s := range r.screens.Inventory.Slots {
		if s.Count <= 0 {
			continue
		}
		if n := itemName(int32(s.Item)); n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}
