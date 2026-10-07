// Package entities tracks the entities the server shows the client: added,
// moved, teleported, synced, given data and equipment, removed — what
// ClientPacketListener does with them, without the interpolation a renderer
// needs: an entity is where its last packet put it.
package entities

import (
	"bytes"
	"io"
	"math"
	"sort"
	"sync"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/data/version"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Entity is one tracked entity.
type Entity struct {
	ID       int32
	UUID     uuid.UUID
	Type     string // minecraft:cow
	X, Y, Z  float64
	VX, VY   float64 // the velocity the server last sent, blocks per tick
	VZ       float64
	Yaw      float32
	Pitch    float32
	HeadYaw  float32
	OnGround bool
	// Data is the entity's synched data by index, as the server sent it
	// (data/entitydata names the indices).
	Data map[uint8]types.EntityDataValue
	// Equipment by slot (EquipmentSlot ordinal: mainhand, offhand, feet, legs,
	// chest, head, body, saddle).
	Equipment map[int]types.ItemStack
	// SpawnData is the add_entity packet's data: a falling block's block
	// state, an item frame's facing, a projectile's owner (by type).
	SpawnData int32
	// Passengers are the ids of the entities riding it, in order
	// (set_passengers); the player's own id among them when it rides.
	Passengers []int32

	base [3]float64 // the position codec's base (VecDeltaCodec)
}

// DistanceSqr is the squared distance from the entity's feet to (x, y, z).
func (e *Entity) DistanceSqr(x, y, z float64) float64 {
	dx, dy, dz := e.X-x, e.Y-y, e.Z-z
	return dx*dx + dy*dy + dz*dz
}

// Events are called on the packet goroutine, the manager unlocked.
type Events struct {
	Added   func(Entity)
	Removed func(Entity)
	// PickedUp: the collector picked up amount of the item entity item.
	PickedUp func(item, collector int32, amount int)
}

// Manager keeps the entities of the level the player is in.
type Manager struct {
	events    Events
	mu        sync.RWMutex
	m         map[int32]*Entity
	vehicleOf map[int32]int32 // a passenger's vehicle
}

// New creates the tracker and registers its handlers.
func New(c *bot.Client, events Events) *Manager {
	m := &Manager{events: events, m: make(map[int32]*Entity), vehicleOf: make(map[int32]int32)}
	c.Events.AddListener(
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayLogin, F: m.clear},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayRespawn, F: m.clear},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayAddEntity, F: m.onAdd},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayRemoveEntities, F: m.onRemove},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayMoveEntityPos, F: m.onMove(false)},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayMoveEntityPosRot, F: m.onMove(true)},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayMoveEntityRot, F: m.onRot},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayTeleportEntity, F: m.onTeleport},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayEntityPositionSync, F: m.onPositionSync},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlaySetEntityData, F: m.onData},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlaySetEquipment, F: m.onEquipment},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlaySetEntityMotion, F: m.onMotion},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayRotateHead, F: m.onHead},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayTakeItemEntity, F: m.onTake},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlaySetPassengers, F: m.onPassengers},
	)
	return m
}

// Get returns a copy of the entity id.
func (m *Manager) Get(id int32) (Entity, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.m[id]
	if !ok {
		return Entity{}, false
	}
	return e.clone(), true
}

// clone is a copy of e that shares nothing with the tracker: its maps and
// slices are written by the packet handlers while a caller reads the copy.
func (e *Entity) clone() Entity {
	c := *e
	if e.Data != nil {
		c.Data = make(map[uint8]types.EntityDataValue, len(e.Data))
		for k, v := range e.Data {
			c.Data[k] = v
		}
	}
	if e.Equipment != nil {
		c.Equipment = make(map[int]types.ItemStack, len(e.Equipment))
		for k, v := range e.Equipment {
			c.Equipment[k] = v
		}
	}
	c.Passengers = append([]int32(nil), e.Passengers...)
	return c
}

// ByUUID returns a copy of the entity with the UUID (a player, by its profile id).
func (m *Manager) ByUUID(id uuid.UUID) (Entity, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.m {
		if e.UUID == id {
			return e.clone(), true
		}
	}
	return Entity{}, false
}

// Nearby returns copies of the entities within radius of (x, y, z), closest first.
func (m *Manager) Nearby(x, y, z, radius float64) []Entity {
	m.mu.RLock()
	var out []Entity
	for _, e := range m.m {
		if e.DistanceSqr(x, y, z) <= radius*radius {
			out = append(out, e.clone())
		}
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		di, dj := out[i].DistanceSqr(x, y, z), out[j].DistanceSqr(x, y, z)
		if di != dj {
			return di < dj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (m *Manager) clear(pk.Packet) error {
	m.mu.Lock()
	m.m = make(map[int32]*Entity)
	m.vehicleOf = make(map[int32]int32)
	m.mu.Unlock()
	return nil
}

// angle is a rotation byte in degrees (Mth.unpackDegrees).
func angle(b pk.Byte) float32 { return float32(b) * 360 / 256 }

func (m *Manager) onAdd(packet pk.Packet) error {
	var a play.AddEntity
	if err := packet.Scan(&a); err != nil {
		return err
	}
	e := &Entity{
		ID: int32(a.ID), UUID: uuid.UUID(a.UUID), X: float64(a.X), Y: float64(a.Y), Z: float64(a.Z),
		VX: a.Movement.X, VY: a.Movement.Y, VZ: a.Movement.Z,
		Yaw: angle(a.YRot), Pitch: angle(a.XRot), HeadYaw: angle(a.YHeadRot), SpawnData: int32(a.Data),
		Data: map[uint8]types.EntityDataValue{}, Equipment: map[int]types.ItemStack{},
	}
	if int(a.Type) >= 0 && int(a.Type) < len(registryid.EntityType) {
		e.Type = registryid.EntityType[a.Type]
	}
	e.base = [3]float64{e.X, e.Y, e.Z}
	m.mu.Lock()
	m.m[e.ID] = e
	m.mu.Unlock()
	if m.events.Added != nil {
		m.events.Added(*e)
	}
	return nil
}

func (m *Manager) onRemove(packet pk.Packet) error {
	var r play.RemoveEntities
	if err := packet.Scan(&r); err != nil {
		return err
	}
	var gone []Entity
	m.mu.Lock()
	for _, id := range r.EntityIds {
		if e, ok := m.m[int32(id)]; ok {
			gone = append(gone, *e)
			delete(m.m, int32(id))
		}
		m.forget(int32(id))
	}
	m.mu.Unlock()
	if m.events.Removed != nil {
		for _, e := range gone {
			m.events.Removed(e)
		}
	}
	return nil
}

// with runs f on entity id, locked; an entity the client does not know is
// ignored, as the client does.
func (m *Manager) with(id int32, f func(e *Entity)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.m[id]; ok {
		f(e)
	}
}

// decode is VecDeltaCodec.decode: a delta in 1/4096 of a block from the
// codec's base, an axis without a delta keeping the base exactly.
func decode(base float64, delta int64) float64 {
	if delta == 0 {
		return base
	}
	return float64(int64(math.Round(base*4096))+delta) / 4096
}

// onMove handles ClientboundMoveEntity.Pos and PosRot. Their position delta
// is read field by field: 26.3 packs the on-ground flag with a step count and
// may send several deltas, each for a number of ticks; the entity ends where
// the last one puts it.
func (m *Manager) onMove(hasRot bool) func(pk.Packet) error {
	return func(packet pk.Packet) error {
		r := bytes.NewReader(packet.Data)
		var id pk.VarInt
		if _, err := id.ReadFrom(r); err != nil {
			return err
		}
		var deltas [][3]int64
		var onGround bool
		stepped := version.ProtocolVersion >= 777
		if stepped {
			var err error
			if deltas, onGround, err = readSteppedDelta(r); err != nil {
				return err
			}
		} else {
			d, err := readShorts(r)
			if err != nil {
				return err
			}
			deltas = [][3]int64{d}
		}
		var yRot, xRot pk.Byte
		if hasRot {
			if _, err := (pk.Tuple{&yRot, &xRot}).ReadFrom(r); err != nil {
				return err
			}
		}
		if !stepped {
			var g pk.Boolean
			if _, err := g.ReadFrom(r); err != nil {
				return err
			}
			onGround = bool(g)
		}
		m.with(int32(id), func(e *Entity) {
			for _, d := range deltas {
				e.X, e.Y, e.Z = decode(e.base[0], d[0]), decode(e.base[1], d[1]), decode(e.base[2], d[2])
				e.base = [3]float64{e.X, e.Y, e.Z}
			}
			if hasRot {
				e.Yaw, e.Pitch = angle(yRot), angle(xRot)
			}
			e.OnGround = onGround
		})
		return nil
	}
}

// onRot is ClientboundMoveEntity.Rot (its fields are named alike in every
// version, whatever their order).
func (m *Manager) onRot(packet pk.Packet) error {
	var mr play.MoveEntityRot
	if err := packet.Scan(&mr); err != nil {
		return err
	}
	m.with(int32(mr.EntityID), func(e *Entity) {
		e.Yaw, e.Pitch, e.OnGround = angle(mr.YRot), angle(mr.XRot), bool(mr.OnGround)
	})
	return nil
}

func readShorts(r io.Reader) ([3]int64, error) {
	var x, y, z pk.Short
	_, err := pk.Tuple{&x, &y, &z}.ReadFrom(r)
	return [3]int64{int64(x), int64(y), int64(z)}, err
}

// readSteppedDelta reads 26.3's packed flags (bit 0 on ground, the rest the
// step count) and VecDelta: one linear delta, or the steps.
func readSteppedDelta(r io.Reader) ([][3]int64, bool, error) {
	var flags pk.VarInt
	if _, err := flags.ReadFrom(r); err != nil {
		return nil, false, err
	}
	onGround, steps := flags&1 != 0, int(uint32(flags)>>1)
	if steps == 0 {
		d, err := readShorts(r)
		return [][3]int64{d}, onGround, err
	}
	deltas := make([][3]int64, 0, steps)
	for range steps {
		var ticks pk.VarInt
		if _, err := ticks.ReadFrom(r); err != nil {
			return nil, onGround, err
		}
		d, err := readShorts(r)
		if err != nil {
			return nil, onGround, err
		}
		deltas = append(deltas, d)
	}
	return deltas, onGround, nil
}

// onTeleport is ClientboundTeleportEntity: absolute or relative per axis.
func (m *Manager) onTeleport(packet pk.Packet) error {
	var t play.TeleportEntity
	if err := packet.Scan(&t); err != nil {
		return err
	}
	rel := int32(t.Relatives)
	pick := func(bit int32, cur, v float64) float64 {
		if rel&bit != 0 {
			return cur + v
		}
		return v
	}
	c := t.Change
	m.with(int32(t.ID), func(e *Entity) {
		e.X = pick(1, e.X, float64(c.Position.X))
		e.Y = pick(2, e.Y, float64(c.Position.Y))
		e.Z = pick(4, e.Z, float64(c.Position.Z))
		e.Yaw = float32(pick(8, float64(e.Yaw), float64(c.YRot)))
		e.Pitch = float32(pick(16, float64(e.Pitch), float64(c.XRot)))
		e.OnGround = bool(t.OnGround)
		e.base = [3]float64{e.X, e.Y, e.Z}
	})
	return nil
}

// onPositionSync is ClientboundEntityPositionSync: the absolute position, a
// path of steps from 26.3 (the entity ends at the last one).
func (m *Manager) onPositionSync(packet pk.Packet) error {
	r := bytes.NewReader(packet.Data)
	var id pk.VarInt
	if _, err := id.ReadFrom(r); err != nil {
		return err
	}
	var x, y, z pk.Double
	var vx, vy, vz pk.Double
	var hasVel bool
	if version.ProtocolVersion >= 777 {
		var kind pk.VarInt
		if _, err := kind.ReadFrom(r); err != nil {
			return err
		}
		switch kind {
		case 0: // LINEAR
			if _, err := (pk.Tuple{&x, &y, &z}).ReadFrom(r); err != nil {
				return err
			}
		case 1: // STEPPED
			var n pk.VarInt
			if _, err := n.ReadFrom(r); err != nil {
				return err
			}
			for range int(n) {
				var offset pk.VarInt
				if _, err := (pk.Tuple{&x, &y, &z, &offset}).ReadFrom(r); err != nil {
					return err
				}
			}
		}
	} else {
		if _, err := (pk.Tuple{&x, &y, &z, &vx, &vy, &vz}).ReadFrom(r); err != nil {
			return err
		}
		hasVel = true
	}
	var yRot, xRot pk.Float
	var g pk.Boolean
	if _, err := (pk.Tuple{&yRot, &xRot, &g}).ReadFrom(r); err != nil {
		return err
	}
	m.with(int32(id), func(e *Entity) {
		e.X, e.Y, e.Z = float64(x), float64(y), float64(z)
		if hasVel {
			e.VX, e.VY, e.VZ = float64(vx), float64(vy), float64(vz)
		}
		e.Yaw, e.Pitch, e.OnGround = float32(yRot), float32(xRot), bool(g)
		e.base = [3]float64{e.X, e.Y, e.Z}
	})
	return nil
}

func (m *Manager) onData(packet pk.Packet) error {
	var d play.SetEntityData
	if err := packet.Scan(&d); err != nil {
		return err
	}
	m.with(int32(d.ID), func(e *Entity) {
		for _, v := range d.PackedItems {
			e.Data[v.Index] = v
		}
	})
	return nil
}

func (m *Manager) onEquipment(packet pk.Packet) error {
	var s play.SetEquipment
	if err := packet.Scan(&s); err != nil {
		return err
	}
	m.with(int32(s.Entity), func(e *Entity) {
		for _, slot := range s.Slots {
			e.Equipment[int(slot.SlotID)] = slot.ItemStack
		}
	})
	return nil
}

func (m *Manager) onMotion(packet pk.Packet) error {
	var s play.SetEntityMotion
	if err := packet.Scan(&s); err != nil {
		return err
	}
	m.with(int32(s.ID), func(e *Entity) {
		e.VX, e.VY, e.VZ = s.Movement.X, s.Movement.Y, s.Movement.Z
	})
	return nil
}

func (m *Manager) onHead(packet pk.Packet) error {
	var h play.RotateHead
	if err := packet.Scan(&h); err != nil {
		return err
	}
	m.with(int32(h.EntityID), func(e *Entity) { e.HeadYaw = angle(h.YHeadRot) })
	return nil
}

func (m *Manager) onTake(packet pk.Packet) error {
	var t play.TakeItemEntity
	if err := packet.Scan(&t); err != nil {
		return err
	}
	if m.events.PickedUp != nil {
		m.events.PickedUp(int32(t.ItemID), int32(t.PlayerID), int(t.Amount))
	}
	return nil
}
