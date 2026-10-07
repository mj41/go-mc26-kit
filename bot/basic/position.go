package basic

import (
	"math"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// Position is where the client has its player: what the last teleport
// (ClientboundPlayerPosition) said, and from then on what whoever moves the
// player sets with [Player.SetPosition].
type Position struct {
	X, Y, Z    float64
	Yaw, Pitch float32
	// VX, VY, VZ is the delta movement, in blocks per tick.
	VX, VY, VZ float64
}

// The bits of ClientboundPlayerPosition's relatives (net.minecraft.world.entity.Relative).
const (
	RelativeX = 1 << iota
	RelativeY
	RelativeZ
	RelativeYaw
	RelativePitch
	RelativeDeltaX
	RelativeDeltaY
	RelativeDeltaZ
	RelativeRotateDelta
)

// Position returns where the client has its player.
func (p *Player) Position() Position {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pos
}

// SetPosition moves the player on the client's side; what is sent to the
// server is up to the caller (bot/control sends it every tick).
func (p *Player) SetPosition(pos Position) {
	p.mu.Lock()
	p.pos = pos
	p.mu.Unlock()
}

// UpdatePosition changes the position with f, atomically with respect to a
// teleport arriving: a tick that moves the player cannot overwrite a teleport
// that came in while it computed.
func (p *Player) UpdatePosition(f func(Position) Position) {
	p.mu.Lock()
	p.pos = f(p.pos)
	p.mu.Unlock()
}

// handlePlayerPosition applies a teleport to the tracked position, the way
// PositionMoveRotation.calculateAbsolute does: each axis, angle and velocity
// component either replaces the current one or is added to it. It runs before
// the [EventsListener.Teleported] handler, so that handler sees the new
// position and [Player.AcceptTeleportation] sends it.
func (p *Player) handlePlayerPosition(packet pk.Packet) error {
	var pp play.PlayerPosition
	if err := packet.Scan(&pp); err != nil {
		return Error{err}
	}
	p.mu.Lock()
	p.pos = absolutePosition(p.pos, pp)
	p.mu.Unlock()
	return nil
}

func absolutePosition(cur Position, pp play.PlayerPosition) Position {
	rel := int32(pp.Relatives)
	c := pp.Change
	pick := func(bit int32, cur, change float64) float64 {
		if rel&bit != 0 {
			return cur + change
		}
		return change
	}
	next := Position{
		X:     pick(RelativeX, cur.X, float64(c.Position.X)),
		Y:     pick(RelativeY, cur.Y, float64(c.Position.Y)),
		Z:     pick(RelativeZ, cur.Z, float64(c.Position.Z)),
		Yaw:   float32(pick(RelativeYaw, float64(cur.Yaw), float64(c.YRot))),
		Pitch: float32(min(90, max(-90, pick(RelativePitch, float64(cur.Pitch), float64(c.XRot))))),
	}
	vx, vy, vz := cur.VX, cur.VY, cur.VZ
	if rel&RelativeRotateDelta != 0 {
		// Vec3.xRot then Vec3.yRot, by the change of the angles
		dPitch := float64(cur.Pitch-next.Pitch) * math.Pi / 180
		dYaw := float64(cur.Yaw-next.Yaw) * math.Pi / 180
		cosP, sinP := math.Cos(dPitch), math.Sin(dPitch)
		vy, vz = vy*cosP+vz*sinP, vz*cosP-vy*sinP
		cosY, sinY := math.Cos(dYaw), math.Sin(dYaw)
		vx, vz = vx*cosY+vz*sinY, vz*cosY-vx*sinY
	}
	next.VX = pick(RelativeDeltaX, vx, float64(c.DeltaMovement.X))
	next.VY = pick(RelativeDeltaY, vy, float64(c.DeltaMovement.Y))
	next.VZ = pick(RelativeDeltaZ, vz, float64(c.DeltaMovement.Z))
	return next
}

// handlePlayerRotation applies a rotation-only teleport
// (ClientPacketListener.handleRotatePlayer): each angle replaces the current
// one or is added to it.
func (p *Player) handlePlayerRotation(packet pk.Packet) error {
	var pr play.PlayerRotation
	if err := packet.Scan(&pr); err != nil {
		return Error{err}
	}
	p.mu.Lock()
	if pr.RelativeY {
		p.pos.Yaw += float32(pr.YRot)
	} else {
		p.pos.Yaw = float32(pr.YRot)
	}
	pitch := float32(pr.XRot)
	if pr.RelativeX {
		pitch += p.pos.Pitch
	}
	p.pos.Pitch = min(90, max(-90, pitch))
	p.mu.Unlock()
	return nil
}

// handleSetEntityMotion applies a velocity the server gives the player itself
// (knockback, a launch): ClientPacketListener.handleSetEntityMotion.
func (p *Player) handleSetEntityMotion(packet pk.Packet) error {
	var m play.SetEntityMotion
	if err := packet.Scan(&m); err != nil {
		return Error{err}
	}
	if int32(m.ID) != int32(p.Login.PlayerID) {
		return nil
	}
	p.mu.Lock()
	p.pos.VX, p.pos.VY, p.pos.VZ = m.Movement.X, m.Movement.Y, m.Movement.Z
	p.mu.Unlock()
	return nil
}

func (p *Player) attachPosition() {
	p.c.Events.AddListener(
		bot.PacketHandler{Priority: 100, ID: packetid.ClientboundPlaySetEntityMotion, F: p.handleSetEntityMotion},
		bot.PacketHandler{Priority: 100, ID: packetid.ClientboundPlayPlayerPosition, F: p.handlePlayerPosition},
		bot.PacketHandler{Priority: 100, ID: packetid.ClientboundPlayPlayerRotation, F: p.handlePlayerRotation},
	)
}
