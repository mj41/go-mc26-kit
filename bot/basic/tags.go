package basic

import pk "github.com/mj41/go-mc26/net/packet"

// handleUpdateTags applies tags the server sends during play (after a
// /reload), as the configuration phase does.
func (p *Player) handleUpdateTags(packet pk.Packet) error {
	if err := p.c.ReadUpdateTags(packet.Data); err != nil {
		return Error{err}
	}
	return nil
}
