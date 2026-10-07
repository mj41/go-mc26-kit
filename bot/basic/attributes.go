package basic

import (
	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Attribute is one of the player's attributes as the server last sent it: the
// base value and the modifiers on it (AttributeInstance).
type Attribute struct {
	Base      float64
	Modifiers []types.AttributeModifier
}

// Value is the attribute's value: the base plus the added values, plus the
// sum of the base multipliers times that, then each total multiplier
// (AttributeInstance.calculateValue), leaving out the modifiers named in skip.
func (a Attribute) Value(skip ...string) float64 {
	keep := func(m types.AttributeModifier) bool {
		for _, s := range skip {
			if string(m.ID) == s {
				return false
			}
		}
		return true
	}
	v := a.Base
	for _, m := range a.Modifiers {
		if keep(m) && m.Operation == types.AttributeModifierOperationAddValue {
			v += float64(m.Amount)
		}
	}
	r := v
	for _, m := range a.Modifiers {
		if keep(m) && m.Operation == types.AttributeModifierOperationAddMultipliedBase {
			r += v * float64(m.Amount)
		}
	}
	for _, m := range a.Modifiers {
		if keep(m) && m.Operation == types.AttributeModifierOperationAddMultipliedTotal {
			r *= 1 + float64(m.Amount)
		}
	}
	return r
}

// Attribute returns the player's attribute name ("minecraft:movement_speed")
// and whether the server sent it.
func (p *Player) Attribute(name string) (Attribute, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	a, ok := p.attributes[name]
	return a, ok
}

// handleUpdateAttributes keeps the player's own attributes; those of other
// entities are the entity tracker's.
func (p *Player) handleUpdateAttributes(packet pk.Packet) error {
	var u play.UpdateAttributes
	if err := packet.Scan(&u); err != nil {
		return Error{err}
	}
	if int32(u.EntityID) != int32(p.Login.PlayerID) {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.attributes == nil {
		p.attributes = make(map[string]Attribute)
	}
	for _, s := range u.Values {
		if int(s.Attribute) < 0 || int(s.Attribute) >= len(registryid.Attribute) {
			continue
		}
		p.attributes[registryid.Attribute[s.Attribute]] = Attribute{Base: float64(s.Base), Modifiers: []types.AttributeModifier(s.Modifiers)}
	}
	return nil
}

func (p *Player) attachAttributes() {
	p.c.Events.AddListener(bot.PacketHandler{Priority: 100, ID: packetid.ClientboundPlayUpdateAttributes, F: p.handleUpdateAttributes})
}
