// Package ride is the player as a passenger: it sits where its vehicle
// carries it (Entity.positionRider), and a boat it steers is the client's to
// move (AbstractBoat on the controlling client): the keys row it, its physics
// run here, and where it went goes to the server every tick; the server's
// corrections (ClientboundMoveVehicle) put it back. Set [Rider.Ride] as the
// controller's Ride.
package ride

import (
	"math"
	"strings"
	"sync"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26/data/entity"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// Rider seats the player and steers what it controls.
type Rider struct {
	Entities *entities.Manager
	World    physics.Blocks

	mu       sync.Mutex
	boat     *physics.Boat // the boat the player steers, by id
	boatID   int32
	correct  *play.ClientboundMoveVehicle
	vehicle  int32 // the last vehicle, for a new boat
	steering bool
	// Corrections counts the moves of a vehicle the client steers that the
	// server did not accept and put back (ClientboundMoveVehicle).
	Corrections int
}

// New creates a rider and registers for the server's corrections.
func New(c *bot.Client, e *entities.Manager, w physics.Blocks) *Rider {
	r := &Rider{Entities: e, World: w}
	c.Events.AddListener(bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayMoveVehicle, F: r.onMoveVehicle})
	return r
}

func (r *Rider) onMoveVehicle(p pk.Packet) error {
	var m play.ClientboundMoveVehicle
	if err := p.Scan(&m); err != nil {
		return err
	}
	r.mu.Lock()
	r.correct = &m
	r.Corrections++
	r.mu.Unlock()
	return nil
}

// CorrectionCount is Corrections, read safely.
func (r *Rider) CorrectionCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Corrections
}

// Boat is the boat the player steers now, if it does (a copy).
func (r *Rider) Boat() (physics.Boat, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.boat == nil || !r.steering {
		return physics.Boat{}, false
	}
	return *r.boat, true
}

func isBoat(typ string) bool {
	return strings.HasSuffix(typ, "_boat") || strings.HasSuffix(typ, "_raft")
}

// Ride is control.Controller.Ride.
func (r *Rider) Ride(s *control.State, p *basic.Player) bool {
	me := int32(p.Login.PlayerID)
	v, index, ok := r.Entities.Vehicle(me)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steering = false
	if !ok {
		r.boat, r.vehicle = nil, 0
		return false
	}
	// the first passenger of a boat steers it (getControllingPassenger)
	if isBoat(v.Type) && index == 0 {
		if r.boat == nil || r.boatID != v.ID {
			w, h := 1.375, 0.5625
			if t := entity.ByName[v.Type]; t != nil {
				w, h = t.Width, t.Height
			}
			r.boat, r.boatID = physics.NewBoat(physics.Vec3{X: v.X, Y: v.Y, Z: v.Z}, v.Yaw, w, h), v.ID
			r.correct = nil
		}
		b := r.boat
		if c := r.correct; c != nil {
			b.Pos = physics.Vec3{X: float64(c.Position.X), Y: float64(c.Position.Y), Z: float64(c.Position.Z)}
			b.Yaw = float32(c.YRot)
			b.Vel = physics.Vec3{}
			r.correct = nil
		}
		in := s.Input
		b.Tick(r.World, in.Left, in.Right, in.Forward, in.Backward)
		r.steering = true
		// the passenger turns with the boat, at most 105° from its heading
		p.UpdatePosition(func(pos basic.Position) basic.Position {
			yaw := pos.Yaw + b.DeltaRotation
			delta := wrapDegrees(yaw - b.Yaw)
			clamped := float32(math.Max(-105, math.Min(105, float64(delta))))
			pos.Yaw = yaw + clamped - delta
			v.X, v.Y, v.Z, v.Yaw = b.Pos.X, b.Pos.Y, b.Pos.Z, b.Yaw
			pos.X, pos.Y, pos.Z = entities.Seat(v, index, "minecraft:player")
			return pos
		})
		paddles := b.Paddles
		s.Paddles = &paddles
		s.Vehicle = &control.VehicleMove{X: b.Pos.X, Y: b.Pos.Y, Z: b.Pos.Z, Yaw: b.Yaw, OnGround: b.OnGround}
	} else {
		r.boat = nil
		x, y, z := entities.Seat(v, index, "minecraft:player")
		p.UpdatePosition(func(pos basic.Position) basic.Position {
			pos.X, pos.Y, pos.Z = x, y, z
			return pos
		})
	}
	r.vehicle = v.ID
	s.OnGround = false
	return true
}

// wrapDegrees is Mth.wrapDegrees: an angle in [-180, 180).
func wrapDegrees(a float32) float32 {
	a = float32(math.Mod(float64(a), 360))
	if a >= 180 {
		a -= 360
	}
	if a < -180 {
		a += 360
	}
	return a
}
