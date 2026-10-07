package basic

import (
	"math"
	"testing"

	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

func TestAbsolutePosition(t *testing.T) {
	cur := Position{X: 10, Y: -60, Z: 5, Yaw: 90, Pitch: 10, VX: 0.1, VY: -0.08, VZ: 0}
	change := types.PositionMoveRotation{
		Position:      types.Vec3{X: 1, Y: 2, Z: 3},
		DeltaMovement: types.Vec3{X: 0, Y: 0, Z: 0},
		YRot:          30,
		XRot:          85,
	}
	// absolute position, relative yaw and pitch (pitch clamped), velocity kept
	got := absolutePosition(cur, play.PlayerPosition{ID: 1, Change: change,
		Relatives: pk.Int(RelativeYaw | RelativePitch | RelativeDeltaX | RelativeDeltaY | RelativeDeltaZ)})
	want := Position{X: 1, Y: 2, Z: 3, Yaw: 120, Pitch: 90, VX: 0.1, VY: -0.08, VZ: 0}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	// relative position (a `tp ~ ~5 ~`), absolute angles, velocity replaced
	got = absolutePosition(cur, play.PlayerPosition{ID: 2, Change: change, Relatives: pk.Int(RelativeX | RelativeY | RelativeZ)})
	want = Position{X: 11, Y: -58, Z: 8, Yaw: 30, Pitch: 85}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	// ROTATE_DELTA: turning by -90° of yaw turns the velocity with it
	got = absolutePosition(Position{Yaw: 0, VX: 1}, play.PlayerPosition{Change: types.PositionMoveRotation{YRot: 90},
		Relatives: pk.Int(RelativeX | RelativeY | RelativeZ | RelativeDeltaX | RelativeDeltaY | RelativeDeltaZ | RelativeRotateDelta)})
	if math.Abs(got.VX) > 1e-9 || math.Abs(got.VZ-1) > 1e-9 {
		t.Errorf("rotated velocity %v %v %v, want 0 0 1", got.VX, got.VY, got.VZ)
	}
}
