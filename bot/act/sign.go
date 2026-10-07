package act

import (
	"bytes"
	"sync"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/version"
	pk "github.com/mj41/go-mc26/net/packet"
)

// Signs answers the sign editor the server opens on a placed sign.
type Signs struct {
	c      *bot.Client
	mu     sync.Mutex
	opened chan world.BlockPos
	front  bool
}

// NewSigns registers the handler of the sign editor.
func NewSigns(c *bot.Client) *Signs {
	s := &Signs{c: c, opened: make(chan world.BlockPos, 1)}
	c.Events.AddListener(bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayOpenSignEditor, F: s.onOpen})
	return s
}

// onOpen is ClientboundOpenSignEditor: the sign's position, then which side
// — a boolean "front" before 26.3, the SignTextSlot enum (back 0, front 1)
// from it: the byte is 1 for the front either way.
func (s *Signs) onOpen(p pk.Packet) error {
	r := bytes.NewReader(p.Data)
	var pos pk.Position
	var side pk.Byte
	if _, err := (pk.Tuple{&pos, &side}).ReadFrom(r); err != nil {
		return err
	}
	s.mu.Lock()
	s.front = side == 1
	s.mu.Unlock()
	select {
	case s.opened <- world.BlockPos{X: pos.X, Y: pos.Y, Z: pos.Z}:
	default:
	}
	return nil
}

// Opened is where the server opened the sign editor, once it did.
func (s *Signs) Opened() <-chan world.BlockPos { return s.opened }

// Write fills the open sign's side with four lines (ServerboundSignUpdate):
// the position, then before 26.3 the side and the four lines, from 26.3 the
// four lines and the side.
func (s *Signs) Write(pos world.BlockPos, lines [4]string) error {
	s.mu.Lock()
	front := s.front
	s.mu.Unlock()
	fields := []pk.FieldEncoder{pk.Position{X: pos.X, Y: pos.Y, Z: pos.Z}}
	ls := []pk.FieldEncoder{pk.String(lines[0]), pk.String(lines[1]), pk.String(lines[2]), pk.String(lines[3])}
	if version.ProtocolVersion >= 777 {
		side := pk.VarInt(0)
		if front {
			side = 1
		}
		fields = append(append(fields, ls...), side)
	} else {
		fields = append(append(fields, pk.Boolean(front)), ls...)
	}
	return s.c.Conn.WritePacket(pk.Marshal(packetid.ServerboundPlaySignUpdate, fields...))
}
