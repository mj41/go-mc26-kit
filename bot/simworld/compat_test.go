package simworld

import (
	"bytes"
	"testing"

	"github.com/mj41/go-mc26/protocol/play"
)

// The version-agnostic builders make the packets of whatever shape the library has (26.1 and 26.2: one shape, 26.3: another):
// the packet encodes and reads back the same, with its entity and its ground flag; an explosion tells its sound
// where the version has the field.
func TestVersionAgnosticPackets(t *testing.T) {
	p := positionSync(42, 1.5, 64, -3.25, 90, 10, true)
	if int32(p.ID) != 42 || !bool(p.OnGround) {
		t.Fatalf("the packet: %+v", p)
	}
	var buf bytes.Buffer
	if _, err := p.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	var back play.EntityPositionSync
	if _, err := back.ReadFrom(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	var again bytes.Buffer
	back.WriteTo(&again)
	if !bytes.Equal(buf.Bytes(), again.Bytes()) || int32(back.ID) != 42 || !bool(back.OnGround) {
		t.Errorf("read back %+v", back)
	}
	// the position is in it: another place is another packet
	var other bytes.Buffer
	q := positionSync(42, 2.5, 64, -3.25, 90, 10, true)
	q.WriteTo(&other)
	if bytes.Equal(buf.Bytes(), other.Bytes()) {
		t.Error("the position is not in the packet")
	}
	var e bytes.Buffer
	if _, err := withSound(play.Explode{}).WriteTo(&e); err != nil {
		t.Fatal(err)
	}
}
