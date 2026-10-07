package advancements

import "testing"

type told struct {
	id      string
	initial bool
	a       Advancement
}

// adv is an advancement as an update adds it: display or none, requirements.
func adv(id string, display bool, req ...[]string) *Advancement {
	a := &Advancement{ID: id, Requirements: req}
	if display {
		a.Display = &Display{Title: "Stone Age", Description: "Mine Stone with your new Pickaxe", Frame: "goal"}
	}
	return a
}

// progress is an advancement's progress: the criteria obtained, and those not.
func progress(id string, obtained []string, not ...string) criteria {
	p := criteria{id: id, obtained: map[string]bool{}}
	for _, c := range obtained {
		p.obtained[c] = true
	}
	for _, c := range not {
		p.obtained[c] = false
	}
	return p
}

// The tracker over updates in the shape every library version is read into
// (read, by field name, is what differs between versions).
func TestTracker(t *testing.T) {
	var got []told
	tr := NewTracker(func(a Advancement, initial bool) { got = append(got, told{a.ID, initial, a}) })
	// the join: one done already, one half done, a recipe done (never told)
	tr.apply(change{
		reset: true,
		added: []*Advancement{
			adv("minecraft:story/root", true, []string{"crafting_table"}),
			adv("minecraft:story/mine_stone", true, []string{"get_stone"}),
			adv("minecraft:husbandry/balanced_diet", true, []string{"apple"}, []string{"bread"}),
			adv("minecraft:recipes/misc/torch", false, []string{"has_coal"}),
		},
		progress: []criteria{
			progress("minecraft:story/root", []string{"crafting_table"}),
			progress("minecraft:story/mine_stone", nil, "get_stone"),
			progress("minecraft:husbandry/balanced_diet", []string{"apple"}, "bread"),
			progress("minecraft:recipes/misc/torch", []string{"has_coal"}),
		},
	})
	if len(got) != 1 || got[0].id != "minecraft:story/root" || !got[0].initial {
		t.Fatalf("at the join: %+v", got)
	}
	// later: stone mined (told, not initial), the diet still half
	tr.apply(change{progress: []criteria{
		progress("minecraft:story/mine_stone", []string{"get_stone"}),
		progress("minecraft:husbandry/balanced_diet", []string{"apple"}, "bread"),
	}})
	if len(got) != 2 || got[1].id != "minecraft:story/mine_stone" || got[1].initial {
		t.Fatalf("stone: %+v", got)
	}
	if d := got[1].a.Display; d == nil || d.Title != "Stone Age" || d.Frame != "goal" {
		t.Errorf("display %+v", d)
	}
	// the same again: not told twice
	tr.apply(change{progress: []criteria{progress("minecraft:story/mine_stone", []string{"get_stone"})}})
	// removed: forgotten; the diet's other group met: told
	tr.apply(change{
		removed:  []string{"minecraft:story/root"},
		progress: []criteria{progress("minecraft:husbandry/balanced_diet", []string{"apple", "bread"})},
	})
	if len(got) != 3 || got[2].id != "minecraft:husbandry/balanced_diet" {
		t.Fatalf("diet: %+v", got)
	}
	if _, ok := tr.Get("minecraft:story/root"); ok {
		t.Error("root still known after removed")
	}
	// a later reset (a reload): what was told is not told again, not initial
	tr.apply(change{
		reset:    true,
		added:    []*Advancement{adv("minecraft:story/mine_stone", true, []string{"get_stone"})},
		progress: []criteria{progress("minecraft:story/mine_stone", []string{"get_stone"})},
	})
	if len(got) != 3 {
		t.Errorf("told again after a reset: %+v", got[3:])
	}
}
