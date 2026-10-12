package simworld

import (
	"math"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/chat"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// die is the player's death (ServerPlayer.die): what it carries is thrown down where it fell — the pack, the
// hotbar, the offhand, the crafting grid and the cursor, each stack up and round (Player.drop: a pick-up delay
// of 40 ticks) — and the bot is told its health is 0 and the death screen's message. It stays dead till it asks
// to respawn.
func (g *Game) die(why string) error {
	g.dead = true
	g.deaths++
	var drops []slotItem
	for i := range g.slots {
		if g.slots[i].n > 0 {
			drops = append(drops, g.slots[i])
			g.slots[i] = slotItem{}
		}
	}
	for c := 1; c <= 4; c++ { // (the 2x2 grid of its own menu)
		if s := g.at(g.menu0, c); s.n > 0 {
			drops = append(drops, *s)
			*s = slotItem{}
		}
	}
	if g.cursor.n > 0 {
		drops = append(drops, g.cursor)
		g.cursor = slotItem{}
	}
	for _, d := range drops {
		// (Player.drop thrown randomly: from the eyes less 0.3, a speed up to 0.5 any way round, a little up)
		f, a := 0.5*g.motion.Float64(), g.motion.Float64()*2*math.Pi
		if err := g.addItem(g.pos[0], g.pos[1]+1.62-0.3, g.pos[2], -math.Sin(a)*f, 0.2, math.Cos(a)*f, d, 40); err != nil {
			return err
		}
	}
	g.broadcast()
	return g.S.Send(play.PlayerCombatKill{PlayerID: 1, Message: chat.Text(why)})
}

// respawn is the bot's ask to live again (ClientCommand perform respawn): full health and food, put at the
// spawn, the level sent again (the client drops its own at a respawn), nothing in the pack.
func (g *Game) respawn() error {
	if !g.dead {
		return nil
	}
	g.dead = false
	g.Health, g.Food, g.sat, g.exhaustion, g.healTimer = 20, 20, 5, 0, 0
	g.invuln, g.fall, g.toldAttr = 0, 0, nil
	g.pos = [3]float64{fakeserver.Spawn[0], fakeserver.Spawn[1], fakeserver.Spawn[2]}
	g.onGround = true
	g.sent = map[[2]int]bool{}
	g.centre = [2]int{1 << 30, 1 << 30}
	for _, m := range []*menu{g.menu0} {
		for i := 0; i < m.size(); i++ {
			m.snap[i] = slotItem{}
		}
	}
	g.snapCursor = slotItem{}
	g.open = nil
	err := g.S.Send(play.Respawn{CommonPlayerSpawnInfo: types.CommonPlayerSpawnInfo{DimensionType: 0, Dimension: "minecraft:overworld",
		GameType: 0, PreviousGameType: -1, SeaLevel: 63}})
	if err != nil {
		return err
	}
	if err := g.S.Send(play.GameEvent{Event: 13}); err != nil { // LEVEL_CHUNKS_LOAD_START: the client waits for its chunk
		return err
	}
	g.tp++
	g.tpWait = true
	at := play.PlayerPosition{ID: pk.VarInt(g.tp), Change: types.PositionMoveRotation{Position: types.Vec3{X: pk.Double(g.pos[0]), Y: pk.Double(g.pos[1]), Z: pk.Double(g.pos[2])}}}
	if err := g.S.Send(at); err != nil {
		return err
	}
	g.toldHealth = [3]float32{}
	return g.tellHealth([3]float32{})
}

// Dead says whether the player is dead and has not yet asked to respawn.
func (g *Game) Dead() bool { return g.dead }

// Deaths is how many times the player died.
func (g *Game) Deaths() int { return g.deaths }
