package simworld

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/loot"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// The creeper (Creeper, SwellGoal): it walks to the player; nearer than the fuse's start it stands and is
// lit, the fuse's direction told to the client as the entity's data (DATA_SWELL_DIR: 1 lit, -1 not); the
// player farther than the fuse's stop, or out of its sight, puts it out and the fuse burns back down; at
// the fuse's end it blows up (explode) and is gone.

// creeperTick is the creeper's tick before its walk: the fuse burns on by the direction the goal last set,
// then the goal sets it again. It says whether the creeper blew up (it is then gone from the mobs).
func (g *Game) creeperTick(m *mob) (bool, error) {
	f := m.k.fuse
	if m.swell += m.swellDir; m.swell < 0 {
		m.swell = 0
	}
	if m.swell >= f.ticks {
		g.mobs = slices.DeleteFunc(g.mobs, func(x *mob) bool { return x == m })
		if err := g.explode([3]float64{m.x, m.y, m.z}, f.power, m); err != nil {
			return true, err
		}
		return true, g.S.Send(play.RemoveEntities{EntityIds: []pk.VarInt{pk.VarInt(m.id)}})
	}
	d2 := sq(g.pos[0]-m.x) + sq(g.pos[1]-m.y) + sq(g.pos[2]-m.z)
	dir := -1
	if m.swellDir > 0 || d2 < f.start*f.start { // (the goal runs: lit, or the player near)
		eyes := [3]float64{m.x, m.y + m.high*0.85, m.z}
		if d2 <= f.stop*f.stop && g.sees(eyes, [3]float64{g.pos[0], g.pos[1] + playerEye, g.pos[2]}) {
			dir = 1
		}
	}
	return false, g.setSwellDir(m, dir)
}

// setSwellDir sets the creeper's fuse direction and tells the client when it changes.
func (g *Game) setSwellDir(m *mob, dir int) error {
	if m.swellDir == dir {
		return nil
	}
	m.swellDir = dir
	return g.S.Send(play.SetEntityData{ID: pk.VarInt(m.id), PackedItems: types.EntityData{
		{Index: entitydata.CreeperSwellDir, Serializer: 1, Value: ptr(pk.VarInt(dir))}}})
}

// Fuse says a creeper's fuse: how far it has burnt (0 to its length) and whether it is lit (a test's look).
func (g *Game) Fuse(id int32) (swell int, lit bool) {
	for _, m := range g.mobs {
		if m.id == id {
			return m.swell, m.swellDir > 0
		}
	}
	return 0, false
}

// Blasts is how many explosions there have been.
func (g *Game) Blasts() int { return g.blasts }

// explode is ServerExplosion.explode of a mob's blast (the creeper's: blocks destroyed, their drops decayed):
//   - the blocks: rays from the centre to each of the 16×16×16 grid's outer points, each with a strength of
//     the radius times 0.7 to 1.3, walked in steps of 0.3 that each cost 0.225 and, in a block, its blast
//     resistance (a fluid's counted) plus 0.3, times 0.3; a block the ray reaches with strength left is
//     destroyed, its loot table played for a blast of the radius (data/loot: a drop survives one time in
//     the radius);
//   - the player, and the mobs and items, within twice the radius: damage by how near and how seen —
//     the share of points of its box the centre is seen from, times one less the distance over twice the
//     radius (i), (i² + i) / 2 × 7 × twice the radius + 1 — and a push away from the centre (to the eyes)
//     of i; the player's is told in the explosion's packet;
//   - the explosion told: its centre, radius, particle and sound, the player's push; then the blocks.
//
// Fire, a chain of blasts (TNT) and the blocks' own reactions are not modelled.
func (g *Game) explode(c [3]float64, radius float64, src *mob) error {
	g.blasts++
	// the blocks the rays reach
	hit := map[world.BlockPos]bool{}
	var order []world.BlockPos
	for i := 0; i < 16; i++ {
		for j := 0; j < 16; j++ {
			for k := 0; k < 16; k++ {
				if i != 0 && i != 15 && j != 0 && j != 15 && k != 0 && k != 15 {
					continue
				}
				d := [3]float64{float64(i)/15*2 - 1, float64(j)/15*2 - 1, float64(k)/15*2 - 1}
				n := math.Sqrt(sq(d[0]) + sq(d[1]) + sq(d[2]))
				for a := range d {
					d[a] /= n
				}
				strength := radius * (0.7 + float64(g.ai.Float32())*0.6)
				p := c
				for strength > 0 {
					at := world.BlockPos{X: int(math.Floor(p[0])), Y: int(math.Floor(p[1])), Z: int(math.Floor(p[2]))}
					state := g.S.World.At(at.X, at.Y, at.Z)
					if !isAir(state) {
						res := float64(block.BehaviourOf(state).ExplosionResistance)
						if block.FluidOf(state) != nil {
							res = max(res, 100) // (water's and lava's resistance)
						}
						strength -= (res + 0.3) * 0.3
						if strength > 0 && at.Y >= -64 && at.Y < 320 && !hit[at] {
							hit[at] = true
							order = append(order, at)
						}
					}
					for a := range p {
						p[a] += d[a] * 0.3
					}
					strength -= 0.22500001
				}
			}
		}
	}
	// the entities in its reach
	diam := radius * 2
	impact := func(feet [3]float64, half, high float64) float64 {
		dist := math.Sqrt(sq(feet[0]-c[0])+sq(feet[1]-c[1])+sq(feet[2]-c[2])) / diam
		if dist > 1 {
			return -1
		}
		seen := g.seenPercent(c, [3]float64{feet[0] - half, feet[1], feet[2] - half}, [3]float64{feet[0] + half, feet[1] + high, feet[2] + half})
		return (1 - dist) * seen
	}
	damage := func(i float64) float32 { return float32((i*i+i)/2*7*diam + 1) }
	push := func(feet [3]float64, eye, i float64) [3]float64 {
		v := [3]float64{feet[0] - c[0], feet[1] + eye - c[1], feet[2] - c[2]}
		n := math.Sqrt(sq(v[0]) + sq(v[1]) + sq(v[2]))
		if n == 0 {
			return [3]float64{}
		}
		return [3]float64{v[0] / n * i, v[1] / n * i, v[2] / n * i}
	}
	var knock *[3]float64
	var hurtPlayer bool
	var playerDamage float32
	if !g.dead {
		if i := impact(g.pos, playerHalf, playerHigh); i >= 0 {
			v := push(g.pos, playerEye, i)
			knock, hurtPlayer, playerDamage = &v, true, damage(i)
		}
	}
	for _, m := range g.mobs {
		if m.dying > 0 {
			continue
		}
		i := impact([3]float64{m.x, m.y, m.z}, m.half, m.high)
		if i < 0 {
			continue
		}
		v := push([3]float64{m.x, m.y, m.z}, m.high*0.85, i)
		m.vx, m.vy, m.vz = m.vx+v[0], m.vy+v[1], m.vz+v[2]
		m.health -= damage(i)
		if err := g.S.Send(play.DamageEvent{EntityID: pk.VarInt(m.id), SourceType: damageTypeID("minecraft:explosion"),
			SourceCauseID: pk.VarInt(src.id) + 1, SourceDirectID: pk.VarInt(src.id) + 1}); err != nil {
			return err
		}
		if m.health <= 0 {
			how := loot.Context{Rand: g.rng, This: g.lootEntity(m), Attacker: &loot.Entity{Type: src.kind},
				DamageTags: []string{"minecraft:is_explosion"}}
			if err := g.killMob(m, how); err != nil {
				return err
			}
		}
	}
	var gone []pk.VarInt
	g.items = slices.DeleteFunc(g.items, func(it *item) bool { // (an item has 5 health: a near blast burns it)
		if i := impact([3]float64{it.x, it.y, it.z}, 0.125, 0.25); i >= 0 && damage(i) >= 5 {
			gone = append(gone, pk.VarInt(it.id))
			return true
		}
		return false
	})
	if len(gone) > 0 {
		if err := g.S.Send(play.RemoveEntities{EntityIds: gone}); err != nil {
			return err
		}
	}
	// told: the explosion, the player's push in it
	tell := play.Explode{Center: types.Vec3{X: pk.Double(c[0]), Y: pk.Double(c[1]), Z: pk.Double(c[2])}, Radius: pk.Float(radius),
		BlockCount: pk.Int(len(order)), PlaySound: true}
	if knock != nil {
		tell.PlayerKnockback = pk.Option[types.Vec3, *types.Vec3]{Has: true, Val: types.Vec3{X: pk.Double(knock[0]), Y: pk.Double(knock[1]), Z: pk.Double(knock[2])}}
	}
	particle := "minecraft:explosion_emitter"
	if radius < 2 {
		particle = "minecraft:explosion"
	}
	tell.ExplosionParticle = types.ParticleOptions{Type: pk.VarInt(slices.Index(registryid.ParticleType, particle))}
	tell.ExplosionSound.ID = pk.VarInt(slices.Index(registryid.SoundEvent, "minecraft:entity.generic.explode") + 1)
	if err := g.S.Send(tell); err != nil {
		return err
	}
	if hurtPlayer {
		yaw := math.Atan2(-(c[0]-g.pos[0]), c[2]-g.pos[2]) * 180 / math.Pi
		if _, err := g.hurtPlayer(playerDamage, "minecraft:explosion", src.id, src.id, yaw, fmt.Sprintf("blown up by %s", src.kind)); err != nil {
			return err
		}
	}
	// the blocks destroyed, in an order of chance (the game shuffles them), their drops thrown out
	g.ai.Shuffle(len(order), func(a, b int) { order[a], order[b] = order[b], order[a] })
	for _, at := range order {
		state := g.S.World.At(at.X, at.Y, at.Z)
		drops, err := g.lootOf(state, loot.Tool{}, radius)
		if err != nil {
			return err
		}
		g.S.World.Set(at.X, at.Y, at.Z, "minecraft:air")
		if f, ok := g.furnaces[[3]int{at.X, at.Y, at.Z}]; ok {
			for _, s := range f.s {
				if s.n > 0 {
					drops = append(drops, s)
				}
			}
			delete(g.furnaces, [3]int{at.X, at.Y, at.Z})
		}
		for _, it := range drops {
			if err := g.popItem(at.X, at.Y, at.Z, it); err != nil {
				return err
			}
		}
		if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: at.X, Y: at.Y, Z: at.Z}, BlockState: 0}); err != nil {
			return err
		}
	}
	return nil
}

// isAir: the state is air (the plain, the cave's, the void's).
func isAir(s block.StateID) bool {
	return s == 0 || strings.HasSuffix(block.StateList[s].ID(), ":air") || strings.HasSuffix(block.StateList[s].ID(), "_air")
}

// LastMob is the id of the mob summoned last (0: none).
func (g *Game) LastMob() int32 {
	if len(g.mobs) == 0 {
		return 0
	}
	return g.mobs[len(g.mobs)-1].id
}

// MobAt says where a mob is (ok false: gone).
func (g *Game) MobAt(id int32) (x, y, z float64, ok bool) {
	for _, m := range g.mobs {
		if m.id == id {
			return m.x, m.y, m.z, true
		}
	}
	return 0, 0, 0, false
}
