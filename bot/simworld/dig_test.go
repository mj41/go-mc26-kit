package simworld

import (
	"math"
	"reflect"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// digOne has the bot dig the block at at, by hand, and returns how it ended.
func digOne(t *testing.T, g *Game, at world.BlockPos) act.Dig {
	t.Helper()
	done := make(chan act.Dig, 1)
	g.B.Hands.Dig(at, func(d act.Dig) { done <- d })
	var d act.Dig
	if !g.Until(400, func() bool {
		select {
		case d = <-done:
			return true
		default:
			return false
		}
	}) {
		t.Fatalf("the dig never ended (tick %d)", g.Tick)
	}
	if d.Err != nil {
		t.Fatalf("dig: %v", d.Err)
	}
	return d
}

// A log dug by hand: the vanilla break time is hardness 2 × 30 / speed 1 = 60
// ticks (the bench's brick "fell a tree" measured 60: sim-a's row for it), the
// block gone on the server and in the bot's world, one log dropped, lying ten
// ticks, then picked up — the bot's inventory has it.
func TestDigLogByHand(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2, Setup: []string{"setblock 1 64 0 oak_log"}})
		at := world.BlockPos{X: 1, Y: 64, Z: 0}
		d := digOne(t, g, at)
		if d.Ticks < 60 || d.Ticks > 61 {
			t.Errorf("the log took %d ticks, vanilla's formula says 60", d.Ticks)
		}
		g.Step(2)
		if got := g.S.World.At(1, 64, 0); got != 0 {
			t.Errorf("the server still has %v there", got)
		}
		if s, _ := g.B.World.BlockAt(at); s != 0 {
			t.Errorf("the bot still sees %v", s)
		}
		if len(g.items) != 1 {
			t.Fatalf("%d items lying, want 1", len(g.items))
		}
		if !g.Until(40, func() bool { return g.Count("minecraft:oak_log") == 1 }) {
			t.Fatalf("the log was not picked up in 40 ticks: %v", g.slots[36:45])
		}
		g.Step(2)
		if n := slotCount(g, "minecraft:oak_log"); n != 1 {
			t.Errorf("the bot's inventory holds %d logs, want 1", n)
		}
		if len(g.items) != 0 {
			t.Errorf("the item still lies there")
		}
	})
}

// Stone by hand: hardness 1.5, speed 1, no drop without a tool — 1.5 × 100 =
// 150 ticks, the block gone, nothing dropped (the block needs a pickaxe).
func TestDigStoneByHandDropsNothing(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2, Setup: []string{"setblock 1 64 0 stone"}})
		d := digOne(t, g, world.BlockPos{X: 1, Y: 64, Z: 0})
		if d.Ticks < 150 || d.Ticks > 151 {
			t.Errorf("the stone took %d ticks, vanilla's formula says 150", d.Ticks)
		}
		g.Step(30)
		if len(g.items) != 0 || g.Count("minecraft:cobblestone") != 0 {
			t.Errorf("stone by hand dropped %d items", len(g.items))
		}
	})
}

// Oak leaves broken by hand drop a sapling one time in twenty, a stick (one or two) two in a hundred: the loot
// table played with the game's chance, the same seed the same drops; shears give the leaves themselves.
func TestLeavesDrops(t *testing.T) {
	Run(t, func(t *testing.T) {
		const n = 4000
		count := func(seed int64, tool string) map[string]int {
			g := Start(t, Options{View: 2, Seed: seed})
			if tool != "" {
				g.Give(tool, 1)
			}
			total := map[string]int{}
			for i := 0; i < n; i++ {
				drops, err := g.dropsOf(stateOf("minecraft:oak_leaves"), true)
				if err != nil {
					t.Fatal(err)
				}
				for _, d := range drops {
					total[d.item] += d.n
				}
			}
			return total
		}
		a := count(5, "")
		if r := float64(a["minecraft:oak_sapling"]) / n; r < 0.035 || r > 0.065 {
			t.Errorf("saplings %.4f a break, want about 0.05", r)
		}
		if r := float64(a["minecraft:stick"]) / n; r < 0.015 || r > 0.05 {
			t.Errorf("sticks %.4f a break, want about 0.03", r)
		}
		if a["minecraft:oak_leaves"] != 0 {
			t.Errorf("leaves by hand gave leaves")
		}
		if b := count(5, ""); !reflect.DeepEqual(a, b) {
			t.Errorf("the same seed gave other drops: %v, %v", a, b)
		}
		if c := count(6, ""); reflect.DeepEqual(a, c) {
			t.Errorf("another seed gave the same drops")
		}
		if sh := count(5, "minecraft:shears"); sh["minecraft:oak_leaves"] != n || len(sh) != 1 {
			t.Errorf("leaves with shears: %v", sh)
		}
	})
}

func slotCount(g *Game, item string) int {
	n := 0
	g.B.Screens.Lock()
	defer g.B.Screens.Unlock()
	for _, s := range g.B.Screens.Inventory.Slots {
		if s.Count > 0 && s.Item >= 0 && itemName(int32(s.Item)) == item {
			n += int(s.Count)
		}
	}
	return n
}

func itemName(id int32) string {
	if id >= 0 && int(id) < len(registryid.Item) {
		return registryid.Item[id]
	}
	return ""
}

// stateOf is the first state of the block that has properties to choose (leaves have a distance).
func stateOf(name string) block.StateID {
	for i, b := range block.StateList {
		if b.ID() == name {
			return block.StateID(i)
		}
	}
	panic("no state of " + name)
}

// dugWith has the bot hold the tool and dig the block beside it; it says the ticks.
func dugWith(t *testing.T, tool, blockName string) (*Game, int) {
	t.Helper()
	g := Start(t, Options{View: 2, Setup: []string{"setblock 1 64 0 " + blockName}})
	if tool != "" {
		g.Give(tool, 1)
		g.Step(2)
	}
	d := digOne(t, g, world.BlockPos{X: 1, Y: 64, Z: 0})
	return g, d.Ticks
}

// Stone with a pickaxe, by vanilla's formula (the tool's speed from its tier,
// hardness 1.5, 30 with the right tool): wooden 2 → 22.5 → 23 ticks, stone 4 →
// 11.25 → 12, iron 6 → 7.5 → 8 (sim-a's row for the wooden: 23). The tags the
// rules name come to the server from the data pack and to the client as
// UpdateTags: the two agree or the server puts the block back.
func TestDigStoneWithPickaxes(t *testing.T) {
	Run(t, func(t *testing.T) {
		for _, c := range []struct {
			tool  string
			ticks int
		}{{"minecraft:wooden_pickaxe", 23}, {"minecraft:stone_pickaxe", 12}, {"minecraft:iron_pickaxe", 8}} {
			g, ticks := dugWith(t, c.tool, "stone")
			if ticks < c.ticks || ticks > c.ticks+1 {
				t.Errorf("%s: the stone took %d ticks, vanilla's formula says %d", c.tool, ticks, c.ticks)
			}
			if !g.Until(40, func() bool { return g.Count("minecraft:cobblestone") == 1 }) {
				t.Errorf("%s: no cobblestone picked up: %v", c.tool, g.slots[36:45])
			}
		}
	})
}

// Iron ore needs a stone tool: a wooden pickaxe breaks it, slowly, and it drops nothing.
func TestDigIronOreWithWoodenPickaxeDropsNothing(t *testing.T) {
	Run(t, func(t *testing.T) {
		g, ticks := dugWith(t, "minecraft:wooden_pickaxe", "iron_ore")
		// hardness 3, speed 2, 100 without the right tool: 3 × 100 / 2 = 150 ticks
		if ticks < 150 || ticks > 151 {
			t.Errorf("the ore took %d ticks, vanilla's formula says 150", ticks)
		}
		g.Step(30)
		if len(g.items) != 0 || g.Count("minecraft:raw_iron") != 0 {
			t.Errorf("the wrong tool dropped %d items", len(g.items))
		}
	})
}

// An axe fells a log faster: stone axe, speed 4: 2 × 30 / 4 = 15 ticks.
func TestDigLogWithAxe(t *testing.T) {
	Run(t, func(t *testing.T) {
		_, ticks := dugWith(t, "minecraft:stone_axe", "oak_log")
		if ticks < 15 || ticks > 16 {
			t.Errorf("the log took %d ticks with a stone axe, vanilla's formula says 15", ticks)
		}
	})
}

// A tool wears a point for each block it breaks (the tool component's damage per block), and the bot is told
// the slot again with the wear: the bench's traces show the pickaxe's slot sent on every stone broken.
func TestToolWearsAndIsTold(t *testing.T) {
	Run(t, func(t *testing.T) {
		g, _ := dugWith(t, "minecraft:stone_pickaxe", "stone")
		g.Step(2)
		if d := g.slots[36].damage; d != 1 {
			t.Errorf("the pickaxe wear is %d after one stone, want 1", d)
		}
	})
}

// A broken block's item is thrown up and falls: it lies on the ground some ticks after it came, not at once, so
// the pick-up (ten ticks of delay, then the bot near it) comes later than the delay for a block dug above the
// bot's feet (the bench's fell-a-tree trace: a log dug three blocks up was taken 20 ticks after it came).
func TestItemFallsFromTheBrokenBlock(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3})
		if err := g.popItem(8, 67, 8, slotItem{item: "minecraft:oak_log", n: 1}); err != nil {
			t.Fatal(err)
		}
		it := g.items[0]
		if it.y < 66.9 {
			t.Fatalf("the item begins at %.2f, in the block it came from (67 to 68)", it.y)
		}
		g.Step(40)
		if it.y < 63.99 || it.y > 64.01 || !it.onGround {
			t.Errorf("the item is at y=%.2f (onGround %v) after 40 ticks, want the ground at 64", it.y, it.onGround)
		}
	})
}

// A stop with less than 0.7 of the progress is not turned back (ServerPlayerGameMode): the dig goes on as a
// delayed destroy and the block breaks when the progress reaches one — a lagging server shows the block back
// at the ack and air a bit later (the shelter brick of the bench).
func TestEarlyStopIsADelayedDestroy(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2, Setup: []string{"setblock 3 64 0 oak_log"}})
		p := func(a types.PlayerActionAction) play.PlayerAction {
			return play.PlayerAction{Action: a, Pos: pk.Position{X: 3, Y: 64, Z: 0}, Sequence: 1}
		}
		if err := g.playAction(p(types.PlayerActionActionStartDestroyBlock)); err != nil {
			t.Fatal(err)
		}
		g.Step(20) // a third of the 60 ticks
		if err := g.playAction(p(types.PlayerActionActionStopDestroyBlock)); err != nil {
			t.Fatal(err)
		}
		g.Step(5)
		if g.S.World.At(3, 64, 0) == 0 {
			t.Fatal("the log broke at once")
		}
		if s, _ := g.B.World.BlockAt(world.BlockPos{X: 3, Y: 64, Z: 0}); s == 0 {
			t.Error("the bot was told the block was gone before it was")
		}
		if !g.Until(60, func() bool { return g.S.World.At(3, 64, 0) == 0 }) {
			t.Fatal("the delayed destroy never broke the block")
		}
		if g.Tick < 55 || g.Tick > 70 {
			t.Errorf("it broke at tick %d, want about 60 after the start", g.Tick)
		}
	})
}

// An item in water rises and floats (ItemEntity.setUnderwaterMovement): one dropped at a pond's floor, three
// deep, is at its top a while later — not lying on the floor, out of a swimmer's reach.
func TestItemFloatsInWater(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3, Setup: []string{"fill 6 61 6 10 63 10 minecraft:water"}})
		if err := g.popItem(8, 61, 8, slotItem{item: "minecraft:wheat", n: 1}); err != nil {
			t.Fatal(err)
		}
		it := g.items[0]
		g.Step(200) // (ten seconds: up from the floor, three deep)
		lo, hi := 100.0, 0.0
		for i := 0; i < 200; i++ { // ten seconds of it floating
			g.Step(1)
			lo, hi = math.Min(lo, it.y), math.Max(hi, it.y)
		}
		if lo < 63 || hi > 64.2 {
			t.Errorf("the item floats between y=%.2f and %.2f in a pond whose top is 64, want at its top (63 to 64.2)", lo, hi)
		}
	})
}

// An item lying five minutes is gone (the game's 6000 ticks), and the bot is told.
func TestItemDespawns(t *testing.T) {
	Run(t, func(t *testing.T) {
		if testing.Short() {
			t.Skip("long (12 s, an item lying five minutes): go test without -short")
		}
		g := Start(t, Options{View: 3})
		if err := g.Drop("minecraft:cobblestone", 1, 8, 64, 8); err != nil {
			t.Fatal(err)
		}
		g.Step(20)
		if g.Items() != 1 {
			t.Fatalf("%d items after a second, want the one", g.Items())
		}
		g.Step(itemLife)
		if g.Items() != 0 {
			t.Errorf("%d items after five minutes, want none", g.Items())
		}
	})
}
