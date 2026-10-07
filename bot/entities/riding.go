package entities

import (
	"math"
	"strings"

	"github.com/mj41/go-mc26/data/entity"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// onPassengers is ClientboundSetPassengersPacket: the vehicle's passengers
// now, in order; any entity id, the player's own included.
func (m *Manager) onPassengers(packet pk.Packet) error {
	var s play.SetPassengers
	if err := packet.Scan(&s); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	vehicle := int32(s.Vehicle)
	for p, v := range m.vehicleOf {
		if v == vehicle {
			delete(m.vehicleOf, p)
		}
	}
	riders := make([]int32, len(s.Passengers))
	for i, p := range s.Passengers {
		riders[i] = int32(p)
		m.vehicleOf[int32(p)] = vehicle
	}
	if e, ok := m.m[vehicle]; ok {
		e.Passengers = riders
	}
	return nil
}

// forget drops what rides and is ridden by an entity that left (locked).
func (m *Manager) forget(id int32) {
	delete(m.vehicleOf, id)
	for p, v := range m.vehicleOf {
		if v == id {
			delete(m.vehicleOf, p)
		}
	}
}

// Vehicle returns a copy of the entity id rides and id's place among its
// passengers.
func (m *Manager) Vehicle(id int32) (Entity, int, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.vehicleOf[id]
	if !ok {
		return Entity{}, 0, false
	}
	e, ok := m.m[v]
	if !ok {
		return Entity{}, 0, false
	}
	index := 0
	for i, p := range e.Passengers {
		if p == id {
			index = i
		}
	}
	return e.clone(), index, true
}

// Seat is where a passenger of type passengerType, index among the vehicle's
// passengers, has its position (Entity.positionRider): the vehicle's passenger
// point for it, turned with the vehicle, less the passenger's own vehicle
// point. A boat places its passengers itself (AbstractBoat).
func Seat(vehicle Entity, index int, passengerType string) (x, y, z float64) {
	var point [3]float64
	vt := entity.ByName[vehicle.Type]
	switch {
	case strings.HasSuffix(vehicle.Type, "_boat") || strings.HasSuffix(vehicle.Type, "_raft"):
		var h float64
		if vt != nil {
			h = vt.Height
		}
		ride := float64(float32(h) / 3)
		if strings.HasSuffix(vehicle.Type, "_raft") {
			ride = float64(float32(h) * 0.8888889)
		}
		offset := float32(0)
		if strings.HasSuffix(vehicle.Type, "_chest_boat") || strings.HasSuffix(vehicle.Type, "_chest_raft") {
			offset = 0.15
		}
		if len(vehicle.Passengers) > 1 {
			offset = 0.2
			if index > 0 {
				offset = -0.6
			}
		}
		point = [3]float64{0, ride, float64(offset)}
	case vt != nil && len(vt.Passengers) > 0:
		point = vt.Passengers[min(index, len(vt.Passengers)-1)]
	}
	px, py, pz := yRot(point, -vehicle.Yaw)
	var own [3]float64
	if pt := entity.ByName[passengerType]; pt != nil {
		own = pt.Vehicle
	}
	return vehicle.X + px - own[0], vehicle.Y + py - own[1], vehicle.Z + pz - own[2]
}

// yRot is Vec3.yRot: the point turned by degrees about the vertical axis.
func yRot(p [3]float64, degrees float32) (x, y, z float64) {
	a := float64(degrees) * math.Pi / 180
	c, s := math.Cos(a), math.Sin(a)
	return p[0]*c + p[2]*s, p[1], p[2]*c - p[0]*s
}
