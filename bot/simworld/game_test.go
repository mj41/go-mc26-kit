package simworld

import (
	"math"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/packetid"
)

var packetidInteract = packetid.ServerboundPlayInteract

func fakeserverPacket(id packetid.ServerboundPacketID) fakeserver.Packet {
	return fakeserver.Packet{ID: id}
}

// A bot walks across a flat world on the stepped clock, the chunks following
// it: arrived, the far chunk loaded, and the server has it where the bot is.
func TestWalkAcrossFlatWorld(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 3})
		g.B.Walker.Goto(world.BlockPos{X: 30, Y: 64, Z: 5})
		if !g.Until(600, func() bool { return g.B.Walker.Status() == "arrived" }) {
			t.Fatalf("the walk: %q at %+v after %d ticks", g.B.Walker.Status(), g.B.Player.Position(), g.Tick)
		}
		p, s := g.B.Player.Position(), g.Pos()
		t.Logf("arrived after %d ticks at %.3f %.3f %.3f", g.Tick, p.X, p.Y, p.Z)
		if math.Floor(p.X) != 30 || math.Floor(p.Z) != 5 {
			t.Errorf("at %.2f %.2f %.2f", p.X, p.Y, p.Z)
		}
		if math.Abs(s[0]-p.X) > 0.5 || math.Abs(s[2]-p.Z) > 0.5 {
			t.Errorf("the server has the player at %v, the bot at %v", s, p)
		}
		if _, ok := g.B.World.BlockAt(world.BlockPos{X: 30 + 16*3 - 1, Y: 63, Z: 5}); !ok {
			t.Errorf("the chunks did not follow the bot")
		}
	})
}

// The same game is the same every time: the walk arrives on the same tick at
// the same place, bit for bit, three times over.
func TestWalkIsDeterministic(t *testing.T) {
	Run(t, func(t *testing.T) {
		type result struct {
			tick    int
			x, y, z float64
		}
		var runs []result
		for i := 0; i < 3; i++ {
			g := Start(t, Options{View: 3})
			g.B.Walker.Goto(world.BlockPos{X: 20, Y: 64, Z: -7})
			if !g.Until(600, func() bool { return g.B.Walker.Status() == "arrived" }) {
				t.Fatalf("run %d: the walk: %q", i, g.B.Walker.Status())
			}
			p := g.B.Player.Position()
			runs = append(runs, result{g.Tick, p.X, p.Y, p.Z})
		}
		if runs[0] != runs[1] || runs[1] != runs[2] {
			t.Fatalf("three runs of one game: %+v", runs)
		}
		t.Logf("arrived on tick %d at %.4f %.4f %.4f, three times", runs[0].tick, runs[0].x, runs[0].y, runs[0].z)
	})
}

// A fall of ten blocks deals what the game deals: ceil(10 − 3) = 7 points, told to the bot.
func TestFallDamage(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Teleport(0.5, 74, 0.5)
		if !g.Until(200, func() bool { return g.Health < 20 }) {
			t.Fatalf("no damage after %d ticks, at %v", g.Tick, g.Pos())
		}
		g.Step(5)
		if g.Health != 13 {
			t.Errorf("health %v after a fall of 10, want 13", g.Health)
		}
		if h := g.B.Player.Status().Health; h != 13 {
			t.Errorf("the bot's health %v, want 13", h)
		}
		if g.Pos()[1] != 64 {
			t.Errorf("landed at %v", g.Pos())
		}
	})
}

// A fall of three blocks or less deals none.
func TestShortFallNoDamage(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Teleport(0.5, 67, 0.5)
		g.Step(60)
		if g.Health != 20 || g.Pos()[1] != 64 {
			t.Errorf("health %v at %v after a fall of 3", g.Health, g.Pos())
		}
	})
}

// A swim is no fall: bobbing up and down in a pond, then out onto its bank, deals none (the fall is
// forgotten in water); a fall of ten into deep water deals none either.
func TestSwimNoFall(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2, Setup: []string{"fill 2 62 -1 6 63 1 minecraft:water"}})
		y := 63.0
		for i := 0; i < 20; i++ { // ten bobs of 0.6 down and up: six blocks "fallen", were it counted
			if err := g.move(3.5, y-0.6, 0.5, false); err != nil {
				t.Fatal(err)
			}
			if err := g.move(3.5, y, 0.5, false); err != nil {
				t.Fatal(err)
			}
		}
		if err := g.move(1.5, 64, 0.5, true); err != nil { // onto the bank
			t.Fatal(err)
		}
		if g.Health != 20 {
			t.Errorf("health %v after a swim and out onto the bank", g.Health)
		}
		g.Teleport(4.5, 74, 0.5)
		g.Step(80)
		if g.Health != 20 {
			t.Errorf("health %v after a fall of ten into water two deep", g.Health)
		}
	})
}

// A move into a block is turned back: the player stays where the server had it.
func TestMoveIntoBlockTurnedBack(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2, Setup: []string{"fill 3 64 0 3 66 0 stone"}})
		if err := g.move(3.5, 64, 0.5, true); err != nil {
			t.Fatal(err)
		}
		if g.Pos()[0] != fakeserverSpawnX || !g.tpWait {
			t.Errorf("the player at %v, teleport pending %v", g.Pos(), g.tpWait)
		}
		g.Step(3)
		if g.tpWait {
			t.Errorf("the bot did not confirm the teleport")
		}
	})
}

const fakeserverSpawnX = 0.5

// A packet the game does not model fails it, loudly.
func TestNotModelledFails(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		err := g.play(fakeserverPacket(packetidInteract))
		if err == nil || !strings.Contains(err.Error(), "not modelled") {
			t.Fatalf("an interact was played: %v", err)
		}
		if err := setup(g.S.World, "gamerule x y"); err == nil || !strings.Contains(err.Error(), "not modelled") {
			t.Fatalf("an unknown world line was taken: %v", err)
		}
	})
}

// The day's clock: told to the bot as the overworld's world clock — held where a stage holds it, or running a
// tick a tick, dusk to night to dawn.
func TestDayClock(t *testing.T) {
	Run(t, func(t *testing.T) {
		if testing.Short() {
			t.Skip("long (3 s, a day's clock): go test without -short")
		}
		g := Start(t, Options{View: 2, Setup: []string{"time set 12200"}})
		g.Step(60)
		if tod, ok := g.B.Clock.TimeOfDay(); !ok || tod != 12200 {
			t.Errorf("held at 12200: the bot reads %d %v", tod, ok)
		}
		run := Start(t, Options{View: 2, Day: &Day{Time: 11800, Run: true}})
		run.Step(100)
		tod, ok := run.B.Clock.TimeOfDay()
		if !ok || tod < 11800+100-20 || tod > 11800+100+20 {
			t.Errorf("running from 11800: after 100 ticks the bot reads %d %v, the server %d", tod, ok, run.DayTime())
		}
		run.Step(900) // past dusk (12542): night
		if tod, _ := run.B.Clock.TimeOfDay(); tod < 12542 {
			t.Errorf("a thousand ticks from 11800 and it is not night: %d", tod)
		}
	})
}

// A stage's "give" lines: what the player holds when it comes in.
func TestGiveLines(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2, Setup: []string{"give Builder minecraft:stone_pickaxe", "give Builder minecraft:oak_log 16"}})
		if g.Count("minecraft:stone_pickaxe") != 1 || g.Count("minecraft:oak_log") != 16 {
			t.Errorf("pickaxe %d logs %d", g.Count("minecraft:stone_pickaxe"), g.Count("minecraft:oak_log"))
		}
	})
}
