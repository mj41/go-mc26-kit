package simworld

import (
	"math"
	"strings"

	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/loot"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// The sun and fire (Mob.isSunBurnTick, Zombie.aiStep, Entity.baseTick): a mob that burns in daylight (its
// kind's sunburn) catches fire when the day says monsters burn, the light at its eyes is bright enough, it
// is not in water (there is no rain here), it wears no helmet (none does here) and the sky light at its
// eyes is full — nothing but air above them; then it burns 8 seconds, renewed while it stays in the sun. A
// burning mob takes a point every second of its fire, is put out by water, and is told on fire by its
// shared flags. A game with no clock (Options.Day nil) has no sun.

// The day as the data pack's timeline/day.json has it: monsters burn from 23460 to 12542 (the
// gameplay/monsters_burn track), and the sky's light is its level times these, between the keyframes
// in a line (gameplay/sky_light_level).
var (
	burnFrom, burnTo = int64(23460), int64(12542)
	skyLightLevel    = [][2]float64{{133, 1}, {11867, 1}, {13670, 0.26666668}, {22330, 0.26666668}}
)

// skyLevel is the sky light's level at the time of day (0 to 1), the keyframes round the day.
func skyLevel(t int64) float64 {
	k := skyLightLevel
	for i := range k {
		a, b := k[i], k[(i+1)%len(k)]
		bt := b[0]
		if bt <= a[0] {
			bt += 24000
		}
		tt := float64(t)
		if tt < a[0] {
			tt += 24000
		}
		if tt >= a[0] && tt <= bt {
			return a[1] + (b[1]-a[1])*(tt-a[0])/(bt-a[0])
		}
	}
	return k[0][1]
}

// monstersBurn: the time of day is one when monsters burn.
func monstersBurn(t int64) bool { return t >= burnFrom || t < burnTo }

// sunBurnTick is Mob.isSunBurnTick: whether the sun sets the mob on fire this tick.
func (g *Game) sunBurnTick(m *mob) bool {
	if g.day == nil || !monstersBurn(g.DayTime()) {
		return false
	}
	// the light at its eyes, under the open sky: the sky's (15 times its level), as the dimension's
	// brightness (no ambient light: f / (4 − 3f))
	f := math.Round(15*skyLevel(g.DayTime())) / 15
	bright := f / (4 - 3*f)
	if bright <= 0.5 || g.ai.Float32()*30 >= float32((bright-0.4)*2) {
		return false
	}
	x, y, z := int(math.Floor(m.x)), int(math.Floor(m.y+m.high*0.85)), int(math.Floor(m.z))
	return !g.mobWet(m) && g.fullSky(x, y, z)
}

// fullSky: the sky light at the block is full — nothing but air from it up (a leaf, glass or water above
// is counted as shade here: the light each lets through is not in the data).
func (g *Game) fullSky(x, y, z int) bool {
	for by := y; by < 320; by++ {
		if !isAir(g.S.World.At(x, by, z)) {
			return false
		}
	}
	return true
}

// mobWet: its box touches water (Entity.updateFluidHeightAndDoFluidPushing: a water block whose surface is
// above the box's bottom), there is no rain here.
func (g *Game) mobWet(m *mob) bool {
	for by := int(math.Floor(m.y)); by <= int(math.Floor(m.y+m.high)); by++ {
		f := block.FluidOf(g.S.World.At(int(math.Floor(m.x)), by, int(math.Floor(m.z))))
		if f != nil && strings.Contains(f.Name, "water") && float64(by)+float64(f.Height) > m.y {
			return true
		}
	}
	return false
}

// burnTick is the mob's fire for a tick, then the sun's: water puts it out; burning, a point of damage every
// fireEvery ticks of its fire; in the sun its fire set again. It says whether the mob died of it.
func (g *Game) burnTick(m *mob) (bool, error) {
	if m.fire > 0 && g.mobWet(m) {
		m.fire = 0
	}
	if m.fire > 0 {
		if m.fire%fireEvery == 0 {
			died, err := g.hurtMob(m, fireDamage, "minecraft:on_fire")
			if err != nil || died {
				return died, err
			}
		}
		m.fire--
	}
	if m.k.sunburn && g.sunBurnTick(m) && m.fire < sunFireTicks {
		m.fire = sunFireTicks
	}
	if on := m.fire > 0; on != m.toldFire {
		m.toldFire = on
		var flags pk.Byte
		if on {
			flags = 0x01 // (the shared flags' on-fire bit)
		}
		return false, g.S.Send(play.SetEntityData{ID: pk.VarInt(m.id), PackedItems: types.EntityData{
			{Index: entitydata.EntitySharedFlags, Serializer: 0, Value: ptr(flags)}}})
	}
	return false, nil
}

// hurtMob is damage to a mob with no one dealing it (fire): not taken in the ten ticks after a blow unless it
// is the greater, told as a damage event; at no health it dies, its loot as it died — burning, killed by the
// player when the player hit it within the last 100 ticks.
func (g *Game) hurtMob(m *mob, amount float32, damageType string) (bool, error) {
	if m.invuln > 10 {
		if amount <= m.lastHurt {
			return false, nil
		}
		amount -= m.lastHurt
	}
	m.lastHurt, m.invuln = amount, 20
	m.health -= amount
	if err := g.S.Send(play.DamageEvent{EntityID: pk.VarInt(m.id), SourceType: damageTypeID(damageType)}); err != nil {
		return false, err
	}
	if m.health > 0 {
		return false, nil
	}
	how := loot.Context{Rand: g.rng, This: g.lootEntity(m), DamageTags: []string{"minecraft:is_fire"}}
	if m.playerHitAt > 0 && g.Tick-m.playerHitAt <= 100 {
		how.KilledByPlayer = true
		how.Attacker = &loot.Entity{Type: "minecraft:player"}
	}
	return true, g.killMob(m, how)
}

// Burning says whether the mob is on fire (a test's look).
func (g *Game) Burning(id int32) bool {
	for _, m := range g.mobs {
		if m.id == id {
			return m.fire > 0
		}
	}
	return false
}

// MobHealth says the mob's health (ok false: gone).
func (g *Game) MobHealth(id int32) (float32, bool) {
	for _, m := range g.mobs {
		if m.id == id {
			return m.health, true
		}
	}
	return 0, false
}

// MobsOf are the ids of the living mobs of the kind (not dying).
func (g *Game) MobsOf(kind string) []int32 {
	var out []int32
	for _, m := range g.mobs {
		if m.kind == kind && m.dying == 0 {
			out = append(out, m.id)
		}
	}
	return out
}
