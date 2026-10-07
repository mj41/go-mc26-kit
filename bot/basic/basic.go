// Package basic provides some basic packet handler which client needs.
//
// # [Player]
//
// The [Player] is attached to a [Client] by calling [NewPlayer] before the client joins a server.
//
// There is 4 kinds of clientbound packet is handled by this package.
//   - LoginPacket, kept as [Player.Login]; its spawn info as [Player.Spawn].
//   - KeepAlivePacket, for avoid the client to be kicked by the server.
//   - PlayerPosition, is only received when server teleporting the player.
//   - Respawn, which replaces [Player.Spawn].
//
// # [EventsListener]
//
// Handles some basic event you probably need.
//   - GameStart
//   - Disconnect
//   - HealthChange
//   - Death
package basic

import (
	"sync"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/version"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

type Player struct {
	c        *bot.Client
	Settings Settings

	// Login is the last play.Login the server sent: the entity id, the level
	// names, the view and simulation distances, the game rules the client is
	// told about (hardcore, reduced debug info, the death screen, …).
	Login play.Login
	// Spawn is the spawn info of the dimension the player is in, from Login and
	// then from every Respawn: the dimension type and name, the game mode, the
	// last death location, the sea level.
	Spawn types.CommonPlayerSpawnInfo

	mu         sync.Mutex
	pos        Position // see [Player.Position]
	attributes map[string]Attribute
	status     Status
}

// NewPlayer create a new Player manager.
func NewPlayer(c *bot.Client, settings Settings, events EventsListener) *Player {
	p := &Player{c: c, Settings: settings}
	c.Events.AddListener(
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayLogin, F: p.handleLoginPacket},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayKeepAlive, F: p.handleKeepAlivePacket},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayRespawn, F: p.handleRespawnPacket},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayPing, F: p.handlePingPacket},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayCookieRequest, F: p.handleCookieRequestPacket},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayStoreCookie, F: p.handleStoreCookiePacket},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayUpdateTags, F: p.handleUpdateTags},
	)
	p.attachPosition()
	p.attachAttributes()
	p.attachStatus()
	events.attach(p)
	return p
}

// Respawn is used to send a respawn packet to the server.
// Typically, you should call this method when the player is dead (in the [Death] event handler).
func (p *Player) Respawn() error {
	cmd := play.ClientCommand{Action: types.ClientCommandActionPerformRespawn}
	if err := p.c.Conn.WritePacket(pk.Marshal(cmd.PacketID(), cmd)); err != nil {
		return Error{err}
	}
	return nil
}

// AcceptTeleportation confirms a teleport, as the client does right after it
// applied one (ClientPacketListener.handleMovePlayer). From 26.3 (protocol
// 777) the confirmation carries the position and rotation the client is at,
// and the server takes it as the player's first move after the teleport; it is
// the tracked [Player.Position], which the teleport has already updated.
// Written field by field so this one source builds against every library
// version the kit supports.
// Without a control.Controller, call it in the [Teleported] event handler; a
// Controller calls it itself, and a teleport confirmed twice disconnects.
func (p *Player) AcceptTeleportation(teleportID pk.VarInt) error {
	fields := []pk.FieldEncoder{teleportID}
	if version.ProtocolVersion >= 777 {
		pos := p.Position()
		fields = append(fields, pk.Double(pos.X), pk.Double(pos.Y), pk.Double(pos.Z), pk.Float(pos.Yaw), pk.Float(pos.Pitch))
	}
	if err := p.c.Conn.WritePacket(pk.Marshal(play.AcceptTeleportation{}.PacketID(), fields...)); err != nil {
		return Error{err}
	}
	return nil
}

type Error struct {
	Err error
}

func (e Error) Error() string {
	return "bot/basic: " + e.Err.Error()
}

func (e Error) Unwrap() error {
	return e.Err
}
