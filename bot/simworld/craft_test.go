package simworld

import (
	"context"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/world"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// craftOnGame has the bot craft with its Screens in the background while the
// game is stepped; it returns what Craft answered.
func craftOnGame(t *testing.T, g *Game, item string, n int) (int, error) {
	t.Helper()
	type res struct {
		n   int
		err error
	}
	done := make(chan res, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	go func() {
		got, err := g.B.Act.Craft(ctx, item, n)
		done <- res{got, err}
	}()
	var r res
	if !g.Until(400, func() bool {
		select {
		case r = <-done:
			return true
		default:
			return false
		}
	}) {
		t.Fatalf("the craft of %s never ended (tick %d)", item, g.Tick)
	}
	return r.n, r.err
}

// A log makes four planks in the inventory's grid: the recipe is in the book
// only once the player holds a log (its advancement), the server fills the grid,
// the result follows, a shift-click takes it.
func TestCraftPlanksFromALog(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		if n := g.B.Book.For("minecraft:oak_planks"); len(n) != 0 {
			t.Fatalf("planks in the book before a log: %v", n)
		}
		g.Give("minecraft:oak_log", 1)
		g.Step(3)
		if len(g.B.Book.For("minecraft:oak_planks")) == 0 {
			t.Fatalf("planks not in the book after a log")
		}
		n, err := craftOnGame(t, g, "minecraft:oak_planks", 4)
		if err != nil || n < 4 {
			t.Fatalf("craft: %d, %v", n, err)
		}
		g.Step(5)
		if g.Count("minecraft:oak_planks") != 4 || g.Count("minecraft:oak_log") != 0 {
			t.Errorf("planks %d, logs %d on the server; want 4 and 0", g.Count("minecraft:oak_planks"), g.Count("minecraft:oak_log"))
		}
		if got := slotCount(g, "minecraft:oak_planks"); got != 4 {
			t.Errorf("the bot holds %d planks, want 4", got)
		}
	})
}

// Sticks need planks only: two planks, four sticks (a shaped recipe, two high).
func TestCraftSticks(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Give("minecraft:oak_planks", 2)
		g.Step(3)
		n, err := craftOnGame(t, g, "minecraft:stick", 4)
		if err != nil || n < 4 {
			t.Fatalf("craft: %d, %v", n, err)
		}
		g.Step(5)
		if g.Count("minecraft:stick") != 4 || g.Count("minecraft:oak_planks") != 0 {
			t.Errorf("sticks %d, planks %d; want 4 and 0", g.Count("minecraft:stick"), g.Count("minecraft:oak_planks"))
		}
	})
}

// The wooden pickaxe is unlocked by a stick, not by the planks: with planks only
// it is not in the book — and needs the table's 3 × 3 grid besides.
func TestPickaxeUnlockedByAStick(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Give("minecraft:oak_planks", 3)
		g.Step(3)
		if len(g.B.Book.For("minecraft:wooden_pickaxe")) != 0 {
			t.Errorf("the wooden pickaxe in the book with planks only")
		}
		g.Give("minecraft:stick", 2)
		g.Step(3)
		if len(g.B.Book.For("minecraft:wooden_pickaxe")) == 0 {
			t.Errorf("the wooden pickaxe not in the book with a stick")
		}
	})
}

// Without the ingredients the server fills nothing: the bot's craft ends with its error.
func TestCraftWithoutIngredientsFails(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Give("minecraft:oak_log", 1)
		g.Step(3)
		g.slots[36] = slotItem{} // (the log taken away behind the bot's back)
		g.broadcast()
		g.Step(2)
		_, err := craftOnGame(t, g, "minecraft:oak_planks", 4)
		if err == nil {
			t.Errorf("a craft without a log succeeded")
		}
	})
}

// placeTable has the bot put a crafting table down beside it and open it; the
// table's menu is open on the bot's side when it returns.
func placeTable(t *testing.T, g *Game) {
	t.Helper()
	g.Give("minecraft:crafting_table", 1)
	g.Step(3)
	use := func(do func(done func(act.Use))) act.Use {
		t.Helper()
		ch := make(chan act.Use, 1)
		do(func(u act.Use) { ch <- u })
		var u act.Use
		if !g.Until(200, func() bool {
			select {
			case u = <-ch:
				return true
			default:
				return false
			}
		}) {
			t.Fatalf("the use never ended (tick %d)", g.Tick)
		}
		if u.Err != nil {
			t.Fatalf("use: %v", u.Err)
		}
		return u
	}
	at := world.BlockPos{X: 1, Y: 64, Z: 0}
	use(func(done func(act.Use)) { g.B.Hands.Place(at, done) })
	g.Step(2)
	if got := g.S.World.At(1, 64, 0); got != fakeserver.State("minecraft:crafting_table") {
		t.Fatalf("no table on the server at the place: %v", got)
	}
	if g.Count("minecraft:crafting_table") != 0 {
		t.Fatalf("the table is still in the inventory")
	}
	use(func(done func(act.Use)) { g.B.Hands.Interact(at, done) })
	if !g.Until(40, func() bool { m, ok := g.B.Screens.Open(); return ok && m.Type == "minecraft:crafting" }) {
		t.Fatalf("the table's menu never opened on the bot's side")
	}
}

// The wooden pickaxe needs the table's three by three: a table put down and
// opened, the recipe (unlocked by the stick) placed, the result taken.
func TestCraftWoodenPickaxeAtATable(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		placeTable(t, g) // (the table is the first thing it holds: the hotbar's first slot, in hand)
		g.Give("minecraft:oak_planks", 3)
		g.Give("minecraft:stick", 2)
		g.Step(3)
		n, err := craftOnGame(t, g, "minecraft:wooden_pickaxe", 1)
		if err != nil || n < 1 {
			t.Fatalf("craft: %d, %v", n, err)
		}
		g.Step(5)
		if g.Count("minecraft:wooden_pickaxe") != 1 || g.Count("minecraft:oak_planks") != 0 || g.Count("minecraft:stick") != 0 {
			t.Errorf("pickaxe %d, planks %d, sticks %d; want 1, 0, 0", g.Count("minecraft:wooden_pickaxe"), g.Count("minecraft:oak_planks"), g.Count("minecraft:stick"))
		}
		if got := slotCount(g, "minecraft:wooden_pickaxe"); got != 1 {
			t.Errorf("the bot holds %d pickaxes, want 1", got)
		}
	})
}

// The stone tools at the table: the pickaxe, axe, sword and shovel from cobblestone and sticks.
func TestCraftStoneToolsAtATable(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		placeTable(t, g)
		g.Give("minecraft:cobblestone", 9)
		g.Give("minecraft:stick", 7)
		g.Step(3)
		for _, tool := range []string{"minecraft:stone_pickaxe", "minecraft:stone_axe", "minecraft:stone_sword", "minecraft:stone_shovel"} {
			if n, err := craftOnGame(t, g, tool, 1); err != nil || n < 1 {
				t.Fatalf("craft %s: %d, %v", tool, n, err)
			}
			g.Step(3)
			if g.Count(tool) != 1 {
				t.Errorf("no %s on the server", tool)
			}
		}
		if g.Count("minecraft:cobblestone") != 0 || g.Count("minecraft:stick") != 0 {
			t.Errorf("cobblestone %d, sticks %d left; the four tools take 9 and 7", g.Count("minecraft:cobblestone"), g.Count("minecraft:stick"))
		}
	})
}

// The table's menu closed gives the grid back: a recipe placed and not taken is
// in the inventory again once the menu is closed.
func TestTableClosedReturnsTheGrid(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		placeTable(t, g)
		g.Give("minecraft:oak_planks", 3)
		g.Give("minecraft:stick", 2)
		g.Step(3)
		rs := g.B.Book.For("minecraft:wooden_pickaxe")
		if len(rs) == 0 {
			t.Fatal("no pickaxe in the book")
		}
		m, _ := g.B.Screens.Open()
		pr := play.PlaceRecipe{ContainerID: pk.VarInt(m.ID), Recipe: types.RecipeDisplayId{Index: pk.VarInt(rs[0].ID)}}
		if err := g.B.C.Conn.WritePacket(pk.Marshal(pr.PacketID(), pr)); err != nil {
			t.Fatal(err)
		}
		g.Step(3)
		if g.Count("minecraft:oak_planks") != 0 || g.open.own[0].item != "minecraft:wooden_pickaxe" {
			t.Fatalf("the grid not filled: planks %d, result %v", g.Count("minecraft:oak_planks"), g.open.own[0])
		}
		if err := g.B.Screens.Close(m.ID); err != nil {
			t.Fatal(err)
		}
		g.Step(3)
		if g.open != nil || g.Count("minecraft:oak_planks") != 3 || g.Count("minecraft:stick") != 2 {
			t.Errorf("open %v, planks %d, sticks %d; want the menu closed and 3 and 2 back", g.open != nil, g.Count("minecraft:oak_planks"), g.Count("minecraft:stick"))
		}
	})
}

// TestCraftNotInTheBookYet: a boat at a table, five planks held — the book
// has no boat (its advancement asks to enter water; a stick on holding planks
// comes a moment after them, the case in the real game): the craft lays the
// planks by hand from the data pack's recipe, as a person does, not "no
// recipe in the book" (botprobe's hut brick: sticks, a chest).
func TestCraftNotInTheBookYet(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		placeTable(t, g)
		g.Give("minecraft:oak_planks", 5)
		g.Step(3)
		if rs := g.B.Act.Book.For("minecraft:oak_boat"); len(rs) != 0 {
			t.Fatalf("the boat in the book already: %v", rs)
		}
		n, err := craftOnGame(t, g, "minecraft:oak_boat", 1)
		if err != nil || n < 1 {
			t.Fatalf("craft: %d, %v", n, err)
		}
		g.Step(5)
		if g.Count("minecraft:oak_boat") != 1 || g.Count("minecraft:oak_planks") != 0 {
			t.Errorf("boat %d, planks %d; want 1, 0", g.Count("minecraft:oak_boat"), g.Count("minecraft:oak_planks"))
		}
	})
}
