package simworld

import (
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
)

// ticks plays n server ticks of hunger and health (no bot needed).
func (g *Game) hungerTicks(n int) {
	for i := 0; i < n; i++ {
		g.Tick++
		if err := g.foodTick(); err != nil {
			g.T.Fatal(err)
		}
	}
}

// Exhaustion past four takes a point of saturation; with none, a point of food.
func TestExhaustionWearsTheFood(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.exhaust(4.5)
		g.hungerTicks(1)
		if g.sat != 4 || g.Food != 20 {
			t.Errorf("sat %v food %d after 4.5 of exhaustion, want 4 and 20", g.sat, g.Food)
		}
		g.sat = 0
		g.exhaust(4.5)
		g.hungerTicks(1)
		if g.Food != 19 {
			t.Errorf("food %d with no saturation, want 19", g.Food)
		}
	})
}

// A food level of 18 or more heals a point every 80 ticks, at a cost of 6 exhaustion.
func TestRegenerationOfAFullStomach(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Health, g.sat, g.Food = 10, 0, 19
		g.hungerTicks(160)
		if g.Health != 12 {
			t.Errorf("health %v after 160 ticks at food 19, want 12", g.Health)
		}
		if g.B.Player.Status().Health < 10 {
			t.Errorf("the bot was not told")
		}
	})
}

// With food at 20 and saturation, healing is fast: every 10 ticks.
func TestFastRegeneration(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Health, g.sat, g.Food = 10, 5, 20
		g.hungerTicks(10)
		if g.Health <= 10 {
			t.Errorf("health %v after 10 ticks with saturation", g.Health)
		}
	})
}

// An empty stomach hurts a point every 80 ticks, down to one health on normal difficulty.
func TestStarvation(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		g.Health, g.sat, g.Food = 3, 0, 0
		g.hungerTicks(80 * 5)
		if g.Health != 1 {
			t.Errorf("health %v after starving, want 1 (normal difficulty does not kill)", g.Health)
		}
	})
}

// The meal: bread eaten takes its consume seconds, fills the stomach and is gone; the bot hears of it.
func TestEatingBread(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2, Setup: []string{"give Builder minecraft:bread 2"}})
		g.Food, g.sat = 10, 0
		g.toldHealth = [3]float32{20, 10, 0}
		done := make(chan act.Use, 1)
		g.B.Hands.Eat(func(u act.Use) { done <- u })
		if !g.Until(200, func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		}) {
			t.Fatal("the meal never ended")
		}
		g.Step(3)
		if g.Food != 15 || g.Count("minecraft:bread") != 1 {
			t.Errorf("food %d bread %d after a bread, want 15 and 1", g.Food, g.Count("minecraft:bread"))
		}
		if f := g.B.Player.Status().Food; f != 15 {
			t.Errorf("the bot's food %d, want 15", f)
		}
		_ = time.Second
	})
}
