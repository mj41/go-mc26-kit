package bot

import (
	"errors"
	"testing"

	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
)

// The census counts packets by id, marks an id no handler is registered for, and keeps the first handler error.
func TestCensusCountsAndKeepsTheFirstError(t *testing.T) {
	c := &Client{}
	c.Events.handlers = make([][]PacketHandler, 400)
	c.Events.AddListener(PacketHandler{ID: packetid.ClientboundPlayKeepAlive, F: func(pk.Packet) error { return nil }})
	c.Events.AddListener(PacketHandler{ID: packetid.ClientboundPlaySetTime, F: func(pk.Packet) error { return errors.New("short read") }})
	for i := 0; i < 3; i++ {
		_ = c.handlePacket(pk.Packet{ID: int32(packetid.ClientboundPlayKeepAlive), Data: make([]byte, 8)})
	}
	for i := 0; i < 2; i++ {
		_ = c.handlePacket(pk.Packet{ID: int32(packetid.ClientboundPlaySetTime), Data: make([]byte, 4)})
	}
	_ = c.handlePacket(pk.Packet{ID: int32(packetid.ClientboundPlayLevelEvent), Data: make([]byte, 1)})
	got := map[packetid.ClientboundPacketID]CensusEntry{}
	for _, e := range c.Census() {
		got[e.ID] = e
	}
	if e := got[packetid.ClientboundPlayKeepAlive]; e.Read != 3 || e.Bytes != 24 || e.NoHandler || e.Errors != 0 {
		t.Errorf("keep-alive %+v", e)
	}
	if e := got[packetid.ClientboundPlaySetTime]; e.Read != 2 || e.Errors != 2 || e.FirstError != "handle packet "+e.Name+" error: short read" && e.FirstError != "short read" {
		t.Errorf("set time %+v", e)
	}
	if e := got[packetid.ClientboundPlayLevelEvent]; !e.NoHandler || e.Read != 1 {
		t.Errorf("level event %+v", e)
	}
}

// A handler's error does not stop the handlers after it for the same packet: the second still runs, and the
// error comes back as a PacketHandlerError.
func TestAFailingHandlerDoesNotHideThePacketFromTheOthers(t *testing.T) {
	c := &Client{}
	c.Events.handlers = make([][]PacketHandler, 400)
	ran := 0
	c.Events.AddListener(
		PacketHandler{ID: packetid.ClientboundPlaySetTime, Priority: 10, F: func(pk.Packet) error { return errors.New("short read") }},
		PacketHandler{ID: packetid.ClientboundPlaySetTime, Priority: 0, F: func(pk.Packet) error { ran++; return nil }},
	)
	c.Events.AddGeneric(PacketHandler{F: func(pk.Packet) error { ran += 10; return errors.New("generic failed") }})
	err := c.handlePacket(pk.Packet{ID: int32(packetid.ClientboundPlaySetTime)})
	var he PacketHandlerError
	if !errors.As(err, &he) || he.ID != packetid.ClientboundPlaySetTime {
		t.Fatalf("err %v, want a PacketHandlerError", err)
	}
	if ran != 11 {
		t.Errorf("ran %d, want the generic handler and the second specific one (11)", ran)
	}
	if got := err.Error(); !(contains(got, "short read") && contains(got, "generic failed")) {
		t.Errorf("the error %q lacks one of the two", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
