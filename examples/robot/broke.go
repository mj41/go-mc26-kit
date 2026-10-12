package main

import (
	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// brokeListener tells a tool that broke in a hand, as the server says it: the entity event 47 (main hand) or 48
// (off hand) for the robot's own entity (the game's client plays the break's sound and bits on it). The item is read
// from that hand as the word arrives — the server syncs the slot only at the tick's end — and is "" when the slot
// is empty already. A break told, not guessed from an item gone (a death, a drop): EVENTS.md "broke".
func (r *robot) brokeListener() bot.PacketHandler {
	return bot.PacketHandler{ID: packetid.ClientboundPlayEntityEvent, F: func(p pk.Packet) error {
		var e play.EntityEvent
		if err := p.Scan(&e); err != nil || int32(e.EntityID) != int32(r.player.Login.PlayerID) {
			return nil
		}
		item := ""
		hand := ""
		r.screens.Lock()
		switch e.EventID {
		case 47:
			hand = "main"
			if s := r.screens.Inventory.Slots[36+r.screens.HeldSlot]; s.Count > 0 {
				item = itemName(int32(s.Item))
			}
		case 48:
			hand = "off"
			if s := *r.screens.Inventory.Offhand(); s.Count > 0 {
				item = itemName(int32(s.Item))
			}
		}
		r.screens.Unlock()
		if hand != "" {
			events.emit("broke", map[string]any{"item": item, "hand": hand})
		}
		return nil
	}}
}
