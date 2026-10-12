package bot

import (
	"errors"
	"fmt"

	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// HandleGame reads the play packets and calls the handlers registered on
// Events until the connection ends: it blocks, and it never returns nil. A
// handler's error comes back as a PacketHandlerError, after which HandleGame
// can be called again (the connection is fine); any other error is the
// connection's.
func (c *Client) HandleGame() error {
	for {
		var p pk.Packet
		// Read packets
		if err := c.Conn.ReadPacket(&p); err != nil {
			return err
		}

		if p.ID == int32(packetid.ClientboundPlayBundleDelimiter) {
			err := c.handleBundlePackets()
			if err != nil {
				return err
			}
		} else if p.ID == int32(packetid.ClientboundPlayStartConfiguration) {
			// MC 1.21.2+: server-initiated config-phase re-entry during play.
			if err := c.handleStartConfiguration(); err != nil {
				return err
			}
		} else {
			// handle packets
			err := c.handlePacket(p)
			if err != nil {
				return err
			}

			// return the packet buffer
			//lint:ignore SA6002 a slice header's allocation: a pointer to p.Data would put the loop's packet on the heap instead, the same cost
			c.Conn.pool.Put(p.Data)
		}
	}
}

// PacketHandlerError is a handler's error, with the id of the packet it handled;
// errors.As reaches the handler's own error through it.
type PacketHandlerError struct {
	ID  packetid.ClientboundPacketID
	Err error
}

func (d PacketHandlerError) Error() string {
	return fmt.Sprintf("handle packet %v error: %v", d.ID, d.Err)
}

func (d PacketHandlerError) Unwrap() error {
	return d.Err
}

// handleBundlePackets reads a bundle's packets up to its closing delimiter and handles them together, as the
// game's client does (PacketBundlePacker): a mob's spawn and its data are never seen apart.
func (c *Client) handleBundlePackets() (err error) {
	var packets []pk.Packet
	for i := 0; i < 4096; i++ {
		var p pk.Packet
		// Read packets
		if err := c.Conn.ReadPacket(&p); err != nil {
			return err
		}

		if p.ID == int32(packetid.ClientboundPlayBundleDelimiter) {
			// bundle finished
			goto handlePackets
		}

		packets = append(packets, p)
	}
	return errors.New("packet number of a bundle out of limit")

handlePackets:
	// every packet of the bundle handled, as each would be outside one: a packet its handler cannot read must
	// not lose the others (a mob's spawn after its broken data); the errors come back after, joined
	var errs []error
	for i := range packets {
		if err := c.handlePacket(packets[i]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// handleStartConfiguration handles a server-initiated config-phase re-entry (MC 1.21.2+).
// The server sends ClientboundStartConfiguration during play; the client must ACK in play
// phase, then run the config loop, and the server resumes play when done.
//
// From the acknowledgement till the configuration finishes the connection speaks configuration only: the play
// phase's writers (the controller's ticks and moves, a chat) wait in Conn.WritePacket, as the game's client
// sends no play packet then; a server reading configuration would take them for configuration packets of
// unknown ids and disconnect.
func (c *Client) handleStartConfiguration() error {
	c.Conn.configure(true)
	defer c.Conn.configure(false)
	ack := play.ConfigurationAcknowledged{}
	if err := c.Conn.push(pk.Marshal(ack.PacketID(), ack)); err != nil {
		return err
	}
	// c.Conn is queue-based: the background goroutine pushes config-phase packets into
	// the queue, so reading from c.Conn here is safe and correct.
	return c.joinConfiguration(configWriter{c.Conn})
}

func (c *Client) handlePacket(p pk.Packet) (err error) {
	packetID := packetid.ClientboundPacketID(p.ID)
	defer func() { c.census.note(packetID, len(p.Data), len(c.Events.handlers[packetID]), err) }()
	// every handler sees the packet: one that fails (a packet it could not read) must not hide it from the
	// others — the world's handler losing a chunk because a recorder's failed would be a game of its own. The
	// first error is returned, the rest joined to it.
	var errs []error
	for _, handler := range c.Events.generic {
		if e := handler.F(p); e != nil {
			errs = append(errs, e)
		}
	}
	for _, handler := range c.Events.handlers[packetID] {
		if e := handler.F(p); e != nil {
			errs = append(errs, e)
		}
	}
	if len(errs) > 0 {
		err = PacketHandlerError{ID: packetID, Err: errors.Join(errs...)}
	}
	return
}
