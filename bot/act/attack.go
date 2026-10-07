package act

import (
	"errors"
	"math"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26/protocol/play"
)

// Attack is the result of an attack: the ticks waited for the attack to be
// fully charged.
type Attack struct {
	Waited int
	Err    error
}

// AttackDelay is how many ticks a full attack takes to charge again with the
// held item (Player.getCurrentItemAttackStrengthDelay: 20 / attack speed).
func AttackDelay(bp *basic.Player) float64 {
	speed := 4.0
	if a, ok := bp.Attribute("minecraft:attack_speed"); ok {
		speed = a.Value()
	}
	return 20 / speed
}

// Attack hits an entity as a person clicking it does: it looks at the entity,
// waits until the attack is fully charged, then sends the attack and swings
// (MultiPlayerGameMode.attack, Minecraft.startAttack). where says where the
// entity is now; done is called once the attack went out.
func (h *Hands) Attack(target int32, where func() (entities.Entity, bool), done func(Attack)) {
	if done == nil {
		done = func(Attack) {}
	}
	h.start(&attack{target: target, where: where, done: done})
}

type attack struct {
	target int32
	where  func() (entities.Entity, bool)
	done   func(Attack)
	waited int
}

func (a *attack) tick(h *Hands, s *control.State, bp *basic.Player) bool {
	e, ok := a.where()
	if !ok {
		a.done(Attack{Err: errors.New("act: the entity is gone")})
		return true
	}
	// aim at the middle of a mob of a player's height or less
	LookAt(bp, s.Input.Shift, e.X, e.Y+0.9, e.Z)
	ex, ey, ez := Eye(bp.Position(), s.Input.Shift)
	if d := math.Sqrt((e.X-ex)*(e.X-ex) + (e.Y+0.9-ey)*(e.Y+0.9-ey) + (e.Z-ez)*(e.Z-ez)); d > entityReach(bp)+0.5 {
		a.done(Attack{Err: errors.New("act: out of reach")})
		return true
	}
	if float64(h.attackTicks) < AttackDelay(bp) {
		a.waited++
		return false
	}
	if err := h.send(play.Attack{EntityID: pkVarInt(a.target)}); err != nil {
		a.done(Attack{Err: err})
		return true
	}
	if err := h.swing(); err != nil {
		a.done(Attack{Err: err})
		return true
	}
	h.attackTicks = 0 // resetAttackStrengthTicker
	a.done(Attack{Waited: a.waited})
	return true
}

// entityReach is the entity interaction range (3 by default).
func entityReach(bp *basic.Player) float64 {
	if a, ok := bp.Attribute("minecraft:entity_interaction_range"); ok {
		return a.Value()
	}
	return 3
}
