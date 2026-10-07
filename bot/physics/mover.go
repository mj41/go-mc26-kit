package physics

import (
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
)

// Mover moves a controller's player with this package's physics: set
// controller.Step to its Step. The position it starts from, and any teleport
// later, come from the basic.Player.
type Mover struct {
	World Blocks
	Tags  Tags
	// Food, when set, says the player's food level (sprinting needs more than 6).
	Food func() int
	// Trace, when set, sees the player after every tick.
	Trace func(*Player)

	p    *Player
	last basic.Position // what the last Step wrote
}

// Player returns the simulated player, nil before the first step.
func (m *Mover) Player() *Player { return m.p }

// Step is a control.Controller's Step.
func (m *Mover) Step(s *control.State, bp *basic.Player) {
	pos := bp.Position()
	if m.p == nil {
		m.p = NewPlayer(Vec3{pos.X, pos.Y, pos.Z})
	}
	p := m.p
	if pos.X != m.last.X || pos.Y != m.last.Y || pos.Z != m.last.Z {
		// a teleport (or the spawn) put the player elsewhere
		p.Pos = Vec3{pos.X, pos.Y, pos.Z}
		p.Vel = Vec3{pos.VX, pos.VY, pos.VZ}
		p.FallDistance = 0
	} else if pos.VX != m.last.VX || pos.VY != m.last.VY || pos.VZ != m.last.VZ {
		// the server set the velocity (knockback)
		p.Vel = Vec3{pos.VX, pos.VY, pos.VZ}
	}
	p.Yaw, p.Pitch = pos.Yaw, pos.Pitch
	if m.Food != nil {
		p.FoodLevel = m.Food()
	}

	p.Tick(m.World, m.Tags, Keys(s.Input), attributesOf(bp))
	if m.Trace != nil {
		m.Trace(p)
	}

	s.OnGround, s.HorizontalCollision, s.Sprinting = p.OnGround, p.HorizontalCollision, p.Sprinting
	next := basic.Position{X: p.Pos.X, Y: p.Pos.Y, Z: p.Pos.Z, Yaw: pos.Yaw, Pitch: pos.Pitch, VX: p.Vel.X, VY: p.Vel.Y, VZ: p.Vel.Z}
	bp.UpdatePosition(func(cur basic.Position) basic.Position {
		if cur.X != pos.X || cur.Y != pos.Y || cur.Z != pos.Z {
			return cur // a teleport arrived while this tick ran: it wins
		}
		next.Yaw, next.Pitch = cur.Yaw, cur.Pitch // a look while this tick ran
		m.last = next
		return next
	})
}

// attributesOf reads the movement attributes the server sent, the defaults
// for any it did not (a version without the attribute, or before the first
// update). The sprint modifier is the physics' own to apply.
func attributesOf(bp *basic.Player) Attributes {
	a := DefaultAttributes
	for name, f := range map[string]*float64{
		"minecraft:movement_speed":            &a.MovementSpeed,
		"minecraft:jump_strength":             &a.JumpStrength,
		"minecraft:gravity":                   &a.Gravity,
		"minecraft:step_height":               &a.StepHeight,
		"minecraft:sneaking_speed":            &a.SneakingSpeed,
		"minecraft:movement_efficiency":       &a.MovementEfficiency,
		"minecraft:water_movement_efficiency": &a.WaterMovementEfficiency,
		"minecraft:friction_modifier":         &a.FrictionModifier,
		"minecraft:air_drag_modifier":         &a.AirDragModifier,
		"minecraft:bounciness":                &a.Bounciness,
	} {
		if v, ok := bp.Attribute(name); ok {
			*f = v.Value("minecraft:sprinting")
		}
	}
	return a
}
