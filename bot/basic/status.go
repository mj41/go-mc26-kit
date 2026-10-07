package basic

import (
	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// Status is what the client shows of the player itself: health, food,
// experience and the effects on it.
type Status struct {
	Health     float32
	Food       int
	Saturation float32
	XPLevel    int
	XPTotal    int
	XPProgress float32
	// Effects by name (minecraft:speed): amplifier (0 is level I) and the
	// ticks left (-1 for infinite).
	Effects map[string]Effect
}

// Effect is one mob effect on the player.
type Effect struct {
	Amplifier int
	Duration  int
}

// Status returns a copy of the player's status.
func (p *Player) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.status
	s.Effects = make(map[string]Effect, len(p.status.Effects))
	for k, v := range p.status.Effects {
		s.Effects[k] = v
	}
	return s
}

func (p *Player) handleSetHealthStatus(packet pk.Packet) error {
	var h play.SetHealth
	if err := packet.Scan(&h); err != nil {
		return Error{err}
	}
	p.mu.Lock()
	p.status.Health, p.status.Food, p.status.Saturation = float32(h.Health), int(h.Food), float32(h.Saturation)
	p.mu.Unlock()
	return nil
}

func (p *Player) handleSetExperience(packet pk.Packet) error {
	var x play.SetExperience
	if err := packet.Scan(&x); err != nil {
		return Error{err}
	}
	p.mu.Lock()
	p.status.XPLevel, p.status.XPTotal, p.status.XPProgress = int(x.ExperienceLevel), int(x.TotalExperience), float32(x.ExperienceProgress)
	p.mu.Unlock()
	return nil
}

func effectName(id pk.VarInt) string {
	if int(id) >= 0 && int(id) < len(registryid.MobEffect) {
		return registryid.MobEffect[id]
	}
	return ""
}

func (p *Player) handleUpdateMobEffect(packet pk.Packet) error {
	var u play.UpdateMobEffect
	if err := packet.Scan(&u); err != nil {
		return Error{err}
	}
	if int32(u.EntityID) != int32(p.Login.PlayerID) {
		return nil
	}
	p.mu.Lock()
	if p.status.Effects == nil {
		p.status.Effects = make(map[string]Effect)
	}
	p.status.Effects[effectName(u.Effect)] = Effect{Amplifier: int(u.EffectAmplifier), Duration: int(u.EffectDurationTicks)}
	p.mu.Unlock()
	return nil
}

func (p *Player) handleRemoveMobEffect(packet pk.Packet) error {
	var r play.RemoveMobEffect
	if err := packet.Scan(&r); err != nil {
		return Error{err}
	}
	if int32(r.EntityID) != int32(p.Login.PlayerID) {
		return nil
	}
	p.mu.Lock()
	delete(p.status.Effects, effectName(r.Effect))
	p.mu.Unlock()
	return nil
}

// clearOnRespawn: a respawn drops the effects (the server sends those it keeps again).
func (p *Player) clearStatusOnRespawn(pk.Packet) error {
	p.mu.Lock()
	p.status.Effects = nil
	p.mu.Unlock()
	return nil
}

func (p *Player) attachStatus() {
	p.c.Events.AddListener(
		bot.PacketHandler{Priority: 100, ID: packetid.ClientboundPlaySetHealth, F: p.handleSetHealthStatus},
		bot.PacketHandler{Priority: 100, ID: packetid.ClientboundPlaySetExperience, F: p.handleSetExperience},
		bot.PacketHandler{Priority: 100, ID: packetid.ClientboundPlayUpdateMobEffect, F: p.handleUpdateMobEffect},
		bot.PacketHandler{Priority: 100, ID: packetid.ClientboundPlayRemoveMobEffect, F: p.handleRemoveMobEffect},
		bot.PacketHandler{Priority: 100, ID: packetid.ClientboundPlayRespawn, F: p.clearStatusOnRespawn},
	)
}
