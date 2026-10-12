package basic

import (
	"time"

	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// KeepAliveTimeout is how long, on the wall clock, the connection waits for the server's next keep-alive
// (or its first, from the join) before it gives up: a server sends one every 15 s. A player's own
// Player.KeepAliveTimeout overrides it.
var KeepAliveTimeout = time.Second * 20

// keepAliveDeadline is when the connection gives up if no keep-alive comes before it: the player's own timeout
// (0: the package's; negative: never, the zero time — a game on a stepped clock has no wall time to keep) from now.
func (p *Player) keepAliveDeadline(now time.Time) time.Time {
	d := p.KeepAliveTimeout
	if d == 0 {
		d = KeepAliveTimeout
	}
	if d <= 0 {
		return time.Time{}
	}
	return now.Add(d)
}

func (p *Player) resetKeepAliveDeadline() {
	p.c.Conn.Socket.SetDeadline(p.keepAliveDeadline(time.Now()))
}

// answerKeepAlive is the keep-alive's answer, run by the connection's reader as the packet is read (Events.AddReadHook):
// the read deadline moved on and the id sent back at once, whatever HandleGame is doing — a handler that waits
// 30 s (a slow consumer, a lock held through a long round) must neither make the client miss its answer nor
// let the deadline fall while the keep-alive lies unhandled in the queue.
func (p *Player) answerKeepAlive(packet pk.Packet) error {
	var ka play.ClientboundKeepAlive
	if err := packet.Scan(&ka); err != nil {
		return Error{err}
	}
	p.resetKeepAliveDeadline()
	//lint:ignore S1016 a struct literal, not a conversion: the kit is assembled for each version, and a version whose two packets differ would not build
	resp := play.ServerboundKeepAlive{ID: ka.ID}
	return p.c.Conn.WritePacket(pk.Marshal(resp.PacketID(), resp))
}

// handleKeepAlivePacket is the ordinary listener for the packet: nothing left to do (the reader answered it),
// and it is there so that the packet counts as handled (the census).
func (p *Player) handleKeepAlivePacket(pk.Packet) error { return nil }
