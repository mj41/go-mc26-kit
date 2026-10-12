package simworld

import (
	"fmt"
	"math"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Hunger, health and food, as the game's rules play them (FoodData.tick, on normal difficulty): what a
// player does wears the food down (exhaustion), a full stomach heals, an empty one hurts, and food eaten
// fills it. The constants are the game's code, not its data: they are said here, one by one.

const (
	exhaustSprint    = 0.1   // a block run
	exhaustJump      = 0.05  // a jump; 0.2 sprinting
	exhaustSprintJmp = 0.2   //
	exhaustBreak     = 0.005 // a block broken
	exhaustHurt      = 0.1   // a hit taken (any damage)
	exhaustRegen     = 6.0   // the healing of a full stomach, per point healed
)

// exhaust adds to the player's exhaustion.
func (g *Game) exhaust(x float32) { g.exhaustion += x }

// foodTick is one server tick of FoodData.tick: exhaustion past four takes a point of saturation, or of
// food when there is none; a food level of 18 heals a point every 80 ticks; saturation at a full 20
// heals fast (a sixth of it a point, every 10 ticks); an empty stomach hurts a point every 80 ticks, on
// normal difficulty down to one health.
func (g *Game) foodTick() error {
	if g.dead {
		return nil
	}
	before := [3]float32{g.Health, float32(g.Food), g.sat}
	if g.exhaustion > 4 {
		g.exhaustion -= 4
		if g.sat > 0 {
			g.sat = max(g.sat-1, 0)
		} else {
			g.Food = max(g.Food-1, 0)
		}
	}
	hurt := g.Health < 20
	switch {
	case g.sat > 0 && g.Food >= 20 && hurt:
		if g.healTimer++; g.healTimer >= 10 {
			amount := min(g.sat, 6)
			g.Health = min(g.Health+amount/6, 20)
			g.exhaust(amount)
			g.healTimer = 0
		}
	case g.Food >= 18 && hurt:
		if g.healTimer++; g.healTimer >= 80 {
			g.Health = min(g.Health+1, 20)
			g.exhaust(exhaustRegen)
			g.healTimer = 0
		}
	case g.Food <= 0:
		if g.healTimer++; g.healTimer >= 80 {
			if g.Health > 1 { // (normal difficulty: it does not kill)
				g.Health--
				g.exhaust(exhaustHurt)
			}
			g.healTimer = 0
		}
	default:
		g.healTimer = 0
	}
	if err := g.finishEating(); err != nil {
		return err
	}
	return g.tellHealth(before)
}

// tellHealth sends the health, food and saturation when they differ from what the bot was last told.
func (g *Game) tellHealth(before [3]float32) error {
	now := [3]float32{g.Health, float32(g.Food), g.sat}
	if now == g.toldHealth && now == before {
		return nil
	}
	if now == g.toldHealth {
		return nil
	}
	g.toldHealth = now
	return g.S.Send(play.SetHealth{Health: pk.Float(g.Health), Food: pk.VarInt(g.Food), Saturation: pk.Float(g.sat)})
}

// startEating is a UseItem with food in hand: the player eats for the item's consume seconds, and the food
// is eaten if it still holds it then (a stomach full to the brim refuses unless the food can always be eaten).
func (g *Game) startEating(u play.UseItem) error {
	held := g.heldItem()
	if held.n == 0 {
		return fmt.Errorf("simworld: not modelled: using nothing")
	}
	st := itemStack(held)
	food := act.Component[*component.Food](st)
	cons := act.Component[*component.Consumable](st)
	if food == nil || cons == nil {
		return fmt.Errorf("simworld: not modelled: using %s (neither food nor drink)", held.item)
	}
	if g.Food >= 20 && !bool(food.CanAlwaysEat) {
		return g.ack(u.Sequence)
	}
	g.eating = &eating{slot: 36 + g.held, item: held.item, until: g.Tick + int(math.Ceil(float64(cons.ConsumeSeconds)*20)), nutrition: int(food.Nutrition), saturation: float32(food.Saturation)}
	return g.ack(u.Sequence)
}

// eating is a meal in progress.
type eating struct {
	slot, until, nutrition int
	item                   string
	saturation             float32
}

// finishEating completes the meal when its time is up, if the item is still in the hand.
func (g *Game) finishEating() error {
	e := g.eating
	if e == nil || g.Tick < e.until {
		return nil
	}
	g.eating = nil
	if g.slots[e.slot].item != e.item || g.slots[e.slot].n == 0 {
		return nil // (put away or eaten meanwhile)
	}
	g.slots[e.slot].n--
	if g.slots[e.slot].n == 0 {
		g.slots[e.slot] = slotItem{}
	}
	g.Food = min(g.Food+e.nutrition, 20)
	g.sat = min(g.sat+e.saturation, float32(g.Food))
	g.broadcast()
	return nil
}

var _ = types.PlayerCommandActionStartSprinting
