package simworld

import (
	"math"
	"math/rand"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26/data/loot"
)

// A zombie put six blocks off walks to the player and hits it: three points at the normal difficulty, the
// bot told the health, and not again for a second (its blow comes every 20 ticks, the player is hurt-immune
// for the same twenty).
func TestZombieWalksToThePlayerAndHits(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3})
		if err := g.SummonNear("minecraft:zombie", 6, 0); err != nil {
			t.Fatal(err)
		}
		if !g.Until(100, func() bool { return g.Health < 20 }) {
			t.Fatalf("the zombie never hit in 5 s (it is at %.1f)", g.mobs[0].x)
		}
		hitAt := g.Tick
		// 4.6 blocks at 0.1175 a tick: about 40 ticks of walking
		if hitAt < 30 || hitAt > 55 {
			t.Errorf("it hit at tick %d, want about 20 after it came", hitAt)
		}
		if g.Health != 17 {
			t.Errorf("health %.1f after a blow, want 17", g.Health)
		}
		g.Step(5)
		if s := g.B.Player.Status().Health; s != 17 {
			t.Errorf("the bot was told %.1f, want 17", s)
		}
		g.Step(30) // (the second blow 20 ticks after the first; the player heals a little between: saturation)
		if g.Health > 16.5 || g.Health < 13.5 {
			t.Errorf("health %.1f after the second blow, want about 14 (three more off, some healed)", g.Health)
		}
	})
}

// The zombie's walk is the bench's: its steady step, a tick to a tick, lies in 0.115–0.120 blocks (cmd/mobspeed over
// the kept runs of natural mobs: the mode of 543 chasing steps).
func TestZombieWalkSpeedIsTheBenchs(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3})
		if err := g.SummonNear("minecraft:zombie", 20, 0); err != nil {
			t.Fatal(err)
		}
		g.Step(40) // (it starts from rest)
		x0 := g.mobs[0].x
		g.Step(20)
		if v := (x0 - g.mobs[0].x) / 20; v < 0.115 || v >= 0.120 {
			t.Errorf("the zombie walks %.4f blocks a tick, the bench's mode is 0.115–0.120", v)
		}
	})
}

// A zombie takes a stone sword's blow of 5 (a hand's is 1).
func TestPlayerHitsAZombie(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3, Setup: []string{"give Builder minecraft:stone_sword"}})
		if err := g.SummonNear("minecraft:zombie", 2, 0); err != nil {
			t.Fatal(err)
		}
		m := g.mobs[0]
		g.Step(10)
		g.B.Hands.Attack(m.id, func() (entities.Entity, bool) { return g.B.Entities.Get(m.id) }, func(act.Attack) {})
		g.Step(10)
		// a stone sword: 5 damage (4 more than a hand's 1)
		if m.health > 16 || m.health < 14 {
			t.Errorf("the zombie has %.1f health after a stone sword's blow, want about 15", m.health)
		}
	})
}

// A zombie walks over a one-deep hole in its way and climbs out of it, as the game's jump does.
func TestZombieClimbsOutOfAHole(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3, Setup: []string{"fill 4 63 0 4 63 0 minecraft:air"}})
		if err := g.SummonNear("minecraft:zombie", 10, 0); err != nil {
			t.Fatal(err)
		}
		if !g.Until(300, func() bool { return g.Health < 20 }) {
			t.Fatalf("the zombie never reached the player: %s", g.MobsAt())
		}
	})
}

// A death: what the player carries lies where it fell (with the pick-up delay of 40), the bot is told its health
// is 0; asked to respawn, it is put at the spawn with full health and an empty pack, the level sent again.
func TestDeathDropsAndRespawn(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3, Setup: []string{"give Builder minecraft:stone_pickaxe", "give Builder minecraft:oak_log 16"}})
		g.Health = 2
		if err := g.SummonNear("minecraft:zombie", 3, 0); err != nil {
			t.Fatal(err)
		}
		if !g.Until(100, g.Dead) {
			t.Fatalf("it did not die (health %.1f): %s", g.Health, g.MobsAt())
		}
		g.Step(5)
		if g.Items() != 2 {
			t.Errorf("%d items lie where it fell, want 2 stacks", g.Items())
		}
		if h := g.B.Player.Status().Health; h != 0 {
			t.Errorf("the bot was told %.1f health, want 0", h)
		}
		if err := g.B.Player.Respawn(); err != nil {
			t.Fatal(err)
		}
		if !g.Until(100, func() bool { return !g.Dead() && g.B.Ctl.State().Loaded }) {
			t.Fatalf("not alive and loaded 5 s after the respawn (dead %v)", g.Dead())
		}
		g.Step(10)
		if g.Health != 20 || g.Count("minecraft:oak_log") != 0 {
			t.Errorf("health %.1f, %d logs after the respawn", g.Health, g.Count("minecraft:oak_log"))
		}
		if p := g.B.Player.Position(); math.Abs(p.X-0.5) > 0.5 || math.Abs(p.Z-0.5) > 0.5 {
			t.Errorf("at %.1f %.1f after the respawn, want the spawn", p.X, p.Z)
		}
		if g.Deaths() != 1 {
			t.Errorf("%d deaths", g.Deaths())
		}
	})
}

// The player's attributes are told as the server tells them: the base values at the join, and with a sword in hand
// its attack damage (5 for stone) and attack speed (4 − 2.4), so the bot's swing waits for the charge.
func TestPlayerAttributesFollowTheHeldItem(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2, Setup: []string{"give Builder minecraft:stone_sword"}})
		g.Step(5)
		a, ok := g.B.Player.Attribute("minecraft:attack_speed")
		if !ok || math.Abs(a.Value()-1.6) > 1e-6 {
			t.Errorf("attack speed %v (told %v), want 1.6 with a sword", a.Value(), ok)
		}
		d, _ := g.B.Player.Attribute("minecraft:attack_damage")
		if math.Abs(d.Value()-5) > 1e-6 {
			t.Errorf("attack damage %v, want 5 with a stone sword", d.Value())
		}
	})
}

// A zombie killed by the player leaves what its loot table says with the game's seed: the seed is picked
// so that the table gives a rare drop too (an iron ingot, a carrot or a potato: one kill in forty), and the
// items lying where it died are the table's, stack for stack; they can be picked up after ten ticks.
func TestZombieKilledDropsItsLoot(t *testing.T) {
	Run(t, func(t *testing.T) {
		rare := map[string]bool{"minecraft:iron_ingot": true, "minecraft:carrot": true, "minecraft:potato": true}
		var seed int64
		var want []loot.Stack
		for s := int64(1); s < 2000 && seed == 0; s++ {
			got, err := loot.EntityDrops("minecraft:zombie", loot.Context{Rand: rand.New(rand.NewSource(s)),
				Attacker: &loot.Entity{Type: "minecraft:player", Mainhand: loot.Tool{Item: "minecraft:stone_sword"}}, KilledByPlayer: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, st := range got {
				if rare[st.Item] {
					seed, want = s, got
				}
			}
		}
		if seed == 0 {
			t.Fatal("no seed in 2000 gives a zombie's rare drop")
		}
		g := Start(t, Options{View: 3, Seed: seed, Setup: []string{"give Builder minecraft:stone_sword"}})
		if err := g.SummonNear("minecraft:zombie", 2, 0); err != nil {
			t.Fatal(err)
		}
		m := g.mobs[0]
		m.health = 1 // (one blow kills it)
		g.Step(10)
		g.B.Hands.Attack(m.id, func() (entities.Entity, bool) { return g.B.Entities.Get(m.id) }, func(act.Attack) {})
		g.Step(10)
		if m.dying == 0 {
			t.Fatalf("the zombie lives (%.1f health)", m.health)
		}
		if len(g.items) != len(want) {
			t.Fatalf("%d items lie where it died, the table gives %v", len(g.items), want)
		}
		for i, it := range g.items {
			if it.what.item != want[i].Item || it.what.n != want[i].Count {
				t.Errorf("item %d: %d %s, the table gives %d %s", i, it.what.n, it.what.item, want[i].Count, want[i].Item)
			}
			if math.Hypot(it.x-m.x, it.z-m.z) > 1.5 {
				t.Errorf("%s lies %.1f blocks from where the zombie died", it.what.item, math.Hypot(it.x-m.x, it.z-m.z))
			}
		}
		for _, it := range g.items {
			if _, ok := g.B.Entities.Get(it.id); !ok {
				t.Errorf("the bot was not told of the %s", it.what.item)
			}
		}
	})
}

// A mob beyond its follow range has no target: a creeper 20 blocks off stands; one 10 off walks to the player.
func TestFollowRange(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		if err := g.SummonNear("minecraft:creeper", 20, 0); err != nil {
			t.Fatal(err)
		}
		far := g.LastMob()
		if err := g.SummonNear("minecraft:creeper", 0, 10); err != nil {
			t.Fatal(err)
		}
		near := g.LastMob()
		x0, _, _, _ := g.MobAt(far)
		_, _, z0, _ := g.MobAt(near)
		g.Step(40)
		x1, _, _, _ := g.MobAt(far)
		_, _, z1, _ := g.MobAt(near)
		if x1 != x0 {
			t.Errorf("the creeper 20 off moved: x %.2f → %.2f", x0, x1)
		}
		if z1 >= z0-1 {
			t.Errorf("the creeper 10 off did not come: z %.2f → %.2f", z0, z1)
		}
	})
}
