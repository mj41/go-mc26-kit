package simworld

import (
	"fmt"
	"math"
	"slices"

	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// The skeleton (AbstractSkeleton, RangedBowAttackGoal): it walks to the player till it is within the bow's
// radius and has seen it a second; then it stands off, strafing — round the player one way or the other,
// back when nearer than half the radius, on when farther than seven tenths of it, the ways changed by
// chance each second. It draws its bow while it has seen the player lately, shoots when the bow is drawn
// and the player is in sight, and waits the interval before it draws again. The arrow is an entity of its
// own (arrowsTick).

// skeletonTick is the skeleton's goal for a tick: it returns the way it walks (ux, uz is the way to the
// player), and draws and shoots its bow.
func (g *Game) skeletonTick(m *mob, ux, uz float64) (float64, float64, error) {
	b := m.k.bow
	eyes := [3]float64{m.x, m.y + b.eye, m.z}
	sees := g.sees(eyes, [3]float64{g.pos[0], g.pos[1] + playerEye, g.pos[2]})
	if sees != (m.seeTime > 0) {
		m.seeTime = 0
	}
	if sees {
		m.seeTime++
	} else {
		m.seeTime--
	}
	d2 := sq(g.pos[0]-m.x) + sq(g.pos[1]-m.y) + sq(g.pos[2]-m.z)
	r2 := b.radius * b.radius
	wx, wz := ux, uz // (to the player)
	if d2 <= r2 && m.seeTime >= 20 {
		wx, wz = 0, 0
		m.strafing++
	} else {
		m.strafing = -1
	}
	if m.strafing >= 20 {
		if g.ai.Float32() < 0.3 {
			m.clockwise = !m.clockwise
		}
		if g.ai.Float32() < 0.3 {
			m.backwards = !m.backwards
		}
		m.strafing = 0
	}
	if m.strafing > -1 {
		if d2 > r2*0.75 {
			m.backwards = false
		} else if d2 < r2*0.25 {
			m.backwards = true
		}
		fwd, side := 0.5, -0.5
		if m.backwards {
			fwd = -0.5
		}
		if m.clockwise {
			side = 0.5
		}
		wx, wz = ux*fwd-uz*side, uz*fwd+ux*side
	}
	// the bow
	if m.using >= 0 {
		m.using++
		switch {
		case !sees && m.seeTime < -60:
			m.using = -1
			return wx, wz, g.tellDrawing(m, false)
		case sees && m.using >= b.draw:
			power := float64(m.using) / 20
			power = min(1, (power*power+power*2)/3)
			m.using, m.attackTime = -1, b.interval
			if err := g.tellDrawing(m, false); err != nil {
				return wx, wz, err
			}
			return wx, wz, g.shoot(m, power)
		}
	} else if m.attackTime--; m.attackTime <= 0 && m.seeTime >= -60 {
		m.using = 0
		return wx, wz, g.tellDrawing(m, true)
	}
	return wx, wz, nil
}

// tellDrawing tells the client the mob is drawing its bow or not (the living entity's flags: using an item).
func (g *Game) tellDrawing(m *mob, on bool) error {
	var flags pk.Byte
	if on {
		flags = 1
	}
	return g.S.Send(play.SetEntityData{ID: pk.VarInt(m.id), PackedItems: types.EntityData{
		{Index: entitydata.LivingEntityLivingEntityFlags, Serializer: 0, Value: ptr(flags)}}})
}

// arrow is an arrow in flight or in the ground.
type arrow struct {
	id, owner  int32
	ownerKind  string
	x, y, z    float64
	vx, vy, vz float64
	damage     float64 // its base damage
	inGround   bool
	life       int // ticks in the ground
}

// shoot is AbstractSkeleton.performRangedAttack: an arrow from under its eyes at the player's body (a third of
// its height up, aimed higher by a fifth of the distance), at the bow's speed, turned a little by chance
// (each way a triangle of 0.0172275 × the inaccuracy), its base damage the bow's times the power with a
// little chance (a triangle of 0.57425).
func (g *Game) shoot(m *mob, power float64) error {
	b := m.k.bow
	from := [3]float64{m.x, m.y + b.eye - 0.1, m.z}
	dx, dy, dz := g.pos[0]-from[0], g.pos[1]+playerHigh/3-from[1], g.pos[2]-from[2]
	dy += math.Hypot(dx, dz) * 0.2
	n := math.Sqrt(dx*dx + dy*dy + dz*dz)
	tri := func(dev float64) float64 { return dev * (g.ai.Float64() - g.ai.Float64()) }
	v := [3]float64{dx/n + tri(0.0172275*b.spread), dy/n + tri(0.0172275*b.spread), dz/n + tri(0.0172275*b.spread)}
	for i := range v {
		v[i] *= b.speed
	}
	g.nextID++
	a := &arrow{id: g.nextID, owner: m.id, ownerKind: m.kind, x: from[0], y: from[1], z: from[2], vx: v[0], vy: v[1], vz: v[2],
		damage: power*b.damage + tri(0.57425)}
	g.arrows = append(g.arrows, a)
	var u pk.UUID
	u[15], u[14] = byte(a.id), byte(a.id>>8)
	u[0] = 2 // (apart from the items' and the mobs')
	yaw, pitch := math.Atan2(v[0], v[2])*180/math.Pi, math.Atan2(v[1], math.Hypot(v[0], v[2]))*180/math.Pi
	return g.S.Send(play.AddEntity{ID: pk.VarInt(a.id), UUID: u, Type: pk.VarInt(slices.Index(registryid.EntityType, "minecraft:arrow")),
		X: pk.Double(a.x), Y: pk.Double(a.y), Z: pk.Double(a.z), Movement: types.LpVec3{X: v[0], Y: v[1], Z: v[2]},
		XRot: pk.Byte(int8(pitch * 256 / 360)), YRot: pk.Byte(int8(yaw * 256 / 360)), Data: pk.VarInt(m.id)})
}

// Arrows is how many arrows are in the world (in flight or in the ground).
func (g *Game) Arrows() int { return len(g.arrows) }

// arrowsTick is AbstractArrow.tick for each arrow: in flight it moves by its speed unless a block's shape or
// the player's box (grown by arrowInflate) is met on the way — the player first if nearer. A hit on the player
// does its speed times its base damage, rounded up, and pushes it along the arrow's flight; then the arrow is
// gone (one the player was immune to bounces back, a tenth of its speed). A block stops it: it lies in the
// ground, gone after arrowLife ticks. In flight it slows (arrowDrag) and falls (arrowGravity) after the move.
// It hits nothing else: other mobs, the skeleton itself.
func (g *Game) arrowsTick() error {
	for i := 0; i < len(g.arrows); i++ {
		a := g.arrows[i]
		if a.inGround {
			if a.life++; a.life >= arrowLife {
				g.arrows = slices.Delete(g.arrows, i, i+1)
				i--
				if err := g.S.Send(play.RemoveEntities{EntityIds: []pk.VarInt{pk.VarInt(a.id)}}); err != nil {
					return err
				}
			}
			continue
		}
		from := [3]float64{a.x, a.y, a.z}
		d := [3]float64{a.vx, a.vy, a.vz}
		tBlock, hitBlock := g.clipBlocks(from, [3]float64{a.x + a.vx, a.y + a.vy, a.z + a.vz})
		if !g.dead {
			lo := [3]float64{g.pos[0] - playerHalf - arrowInflate, g.pos[1] - arrowInflate, g.pos[2] - playerHalf - arrowInflate}
			hi := [3]float64{g.pos[0] + playerHalf + arrowInflate, g.pos[1] + playerHigh + arrowInflate, g.pos[2] + playerHalf + arrowInflate}
			if t, ok := rayBox(from, d, lo, hi); ok && (!hitBlock || t <= tBlock) {
				speed := math.Sqrt(a.vx*a.vx + a.vy*a.vy + a.vz*a.vz)
				amount := float32(math.Ceil(speed * a.damage))
				yaw := math.Atan2(a.vx, a.vz) * 180 / math.Pi
				hurt, err := g.hurtPlayer(amount, "minecraft:arrow", a.owner, a.id, yaw, fmt.Sprintf("shot by %s", a.ownerKind))
				if err != nil {
					return err
				}
				if hurt {
					if err := g.knockPlayer(knockStrength, a.vx, a.vz); err != nil {
						return err
					}
					g.arrows = slices.Delete(g.arrows, i, i+1)
					i--
					if err := g.S.Send(play.RemoveEntities{EntityIds: []pk.VarInt{pk.VarInt(a.id)}}); err != nil {
						return err
					}
					continue
				}
				a.vx, a.vy, a.vz = -0.1*a.vx, -0.1*a.vy, -0.1*a.vz // (immune: it bounces off)
				if err := g.tellArrow(a); err != nil {
					return err
				}
				continue
			}
		}
		if hitBlock {
			n := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2])
			a.x, a.y, a.z = a.x+d[0]*tBlock-d[0]/n*0.05, a.y+d[1]*tBlock-d[1]/n*0.05, a.z+d[2]*tBlock-d[2]/n*0.05
			a.vx, a.vy, a.vz, a.inGround = 0, 0, 0, true
			if err := g.tellArrow(a); err != nil {
				return err
			}
			if err := g.S.Send(play.SetEntityData{ID: pk.VarInt(a.id), PackedItems: types.EntityData{
				{Index: entitydata.AbstractArrowInGround, Serializer: 8, Value: ptr(pk.Boolean(true))}}}); err != nil {
				return err
			}
			continue
		}
		a.x, a.y, a.z = a.x+a.vx, a.y+a.vy, a.z+a.vz
		a.vx, a.vy, a.vz = a.vx*arrowDrag, a.vy*arrowDrag-arrowGravity, a.vz*arrowDrag
		if err := g.tellArrow(a); err != nil {
			return err
		}
	}
	return nil
}

// tellArrow tells the client where the arrow is and how fast it goes.
func (g *Game) tellArrow(a *arrow) error {
	err := g.S.Send(play.EntityPositionSync{ID: pk.VarInt(a.id), Position: types.PositionPath{Type: types.PositionPathTypeLinear,
		X: pk.Double(a.x), Y: pk.Double(a.y), Z: pk.Double(a.z)}, YRot: pk.Float(math.Atan2(a.vx, a.vz) * 180 / math.Pi), OnGround: pk.Boolean(a.inGround)})
	if err != nil {
		return err
	}
	return g.S.Send(play.SetEntityMotion{ID: pk.VarInt(a.id), Movement: types.LpVec3{X: a.vx, Y: a.vy, Z: a.vz}})
}
