// Package control is the client's tick: every 50 ms it sends for the player
// what the vanilla client sends (Minecraft.tick), so that a server cannot tell
// the bot from a person by what arrives:
//
//   - once after a spawn, when the server said the level is loading and the
//     chunk the player stands in has arrived, ServerboundPlayerLoaded
//     (LevelLoadTracker, ClientPacketListener.notifyPlayerLoaded);
//   - the keys pressed, when they changed (ServerboundPlayerInput);
//   - the sprint start or stop, when it changed (ServerboundPlayerCommand);
//   - one movement packet chosen by what changed, the position at least once a
//     second (LocalPlayer.sendPosition);
//   - the tick end (ServerboundClientTickEnd).
//
// It also answers what the server asks of a client's player: a teleport is
// confirmed with the position it put the player at, a forced rotation with a
// rotation packet (ClientPacketListener.handleMovePlayer, handleRotatePlayer).
//
// Moving the player is a [Controller.Step] function's: it runs at the start of
// every tick once the player is loaded, reads the input and changes the
// position through [basic.Player.UpdatePosition]. Without one the player stands
// where the server put it.
package control

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/constants"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/level"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// TickInterval is the client's tick: 20 a second.
const TickInterval = 50 * time.Millisecond

// clientLoadTimeout is how long the client waits for its own chunk before it
// declares itself loaded anyway (LevelLoadTracker.CLIENT_WAIT_TIMEOUT_MS).
const clientLoadTimeout = 30 * time.Second

// gameEventLevelChunksLoadStart is ClientboundGameEventPacket.LEVEL_CHUNKS_LOAD_START:
// the server has sent what the client needs to start loading the level.
const gameEventLevelChunksLoadStart = 13

// Input is the keys a person holds (net.minecraft.world.entity.player.Input).
type Input struct {
	Forward, Backward, Left, Right, Jump, Shift, Sprint bool
}

// flags packs the keys as ServerboundPlayerInput sends them (Input.STREAM_CODEC).
func (in Input) flags() pk.Byte {
	var f pk.Byte
	for _, k := range []struct {
		on  bool
		bit pk.Byte
	}{
		{in.Forward, constants.InputFlagForward}, {in.Backward, constants.InputFlagBackward},
		{in.Left, constants.InputFlagLeft}, {in.Right, constants.InputFlagRight},
		{in.Jump, constants.InputFlagJump}, {in.Shift, constants.InputFlagShift}, {in.Sprint, constants.InputFlagSprint},
	} {
		if k.on {
			f |= k.bit
		}
	}
	return f
}

// State is what the controller knows of the player beyond its position: the
// keys, the flags the movement packets carry, and whether the client has
// loaded the level.
type State struct {
	Input               Input
	Sprinting           bool
	OnGround            bool
	HorizontalCollision bool
	// Loaded is the client's own "level loaded" (ClientPacketListener.hasClientLoaded):
	// false from a spawn until ServerboundPlayerLoaded is sent; the client
	// neither moves nor sends its position before.
	Loaded bool
	// Ticks counts the ticks since the controller started.
	Ticks uint64
	// Teleports counts the teleports the server sent and the client
	// confirmed: a server correcting a move it did not accept is one.
	Teleports int
	// Riding: the player is a passenger this tick (Controller.Ride).
	Riding bool
	// Vehicle, set by Ride for a vehicle the player controls (a boat), is
	// where it went this tick, which the client tells the server after the
	// rotation (ServerboundMoveVehicle); nil when the server moves it.
	Vehicle *VehicleMove
	// Paddles, set by Ride for a boat the player rows, are sent during the
	// tick (ServerboundPaddleBoat): left, right.
	Paddles *[2]bool
}

// VehicleMove is where a vehicle the client controls is.
type VehicleMove struct {
	X, Y, Z    float64
	Yaw, Pitch float32
	OnGround   bool
}

// Controller ticks the player. Create it with [New] before joining, then run
// [Controller.Run] next to bot.Client.HandleGame.
type Controller struct {
	c *bot.Client
	p *basic.Player
	w *world.World

	// Act does what the hands do this tick — dig, place, use — before Think,
	// with the controller locked (Minecraft.tick: the key bindings, then the
	// entities).
	Act func(s *State, p *basic.Player)
	// Think decides the keys and where to look, once a tick before Step, with
	// the controller locked: it sets s.Input and turns with
	// basic.Player.UpdatePosition. What a person does between two frames.
	Think func(s *State, p *basic.Player)
	// Step moves the player, once a tick while it is loaded, with the
	// controller locked; it changes the position with
	// basic.Player.UpdatePosition and the flags through s.
	Step func(s *State, p *basic.Player)
	// Ride, when set, tells each tick whether the player rides something, and
	// if so puts it where its vehicle carries it (Entity.positionRider). A
	// passenger does not walk — Think and Step do not run — and sends its
	// rotation every tick instead of its position (LocalPlayer.sendChanges);
	// the vehicle moves itself, or the server moves it.
	Ride func(s *State, p *basic.Player) bool
	// Loaded, when set, is called (from the tick goroutine, unlocked) once the
	// client has told the server it loaded the level.
	Loaded func()

	mu    sync.Mutex
	state State

	inLevel     bool      // a Login or a Respawn arrived: there is a level to tick
	loadStarted bool      // LEVEL_CHUNKS_LOAD_START arrived (LevelLoadTracker.WaitingForPlayerChunk)
	loadUntil   time.Time // when the client stops waiting for its chunk

	sent             sent
	positionReminder int
}

// sent is what LocalPlayer.sendChanges remembers of what it sent last.
type sent struct {
	x, y, z             float64
	yaw, pitch          float32
	onGround, hCollided bool
	input               Input
	sprinting           bool
}

// New creates the controller of p's player and registers its packet handlers.
// w is where it looks for the chunk the player stands in.
func New(c *bot.Client, p *basic.Player, w *world.World) *Controller {
	ctl := &Controller{c: c, p: p, w: w}
	c.Events.AddListener(
		bot.PacketHandler{Priority: 50, ID: packetid.ClientboundPlayLogin, F: ctl.onSpawn},
		bot.PacketHandler{Priority: 50, ID: packetid.ClientboundPlayRespawn, F: ctl.onSpawn},
		bot.PacketHandler{Priority: 50, ID: packetid.ClientboundPlayGameEvent, F: ctl.onGameEvent},
		// after basic.Player applied them (priority 100)
		bot.PacketHandler{Priority: 50, ID: packetid.ClientboundPlayPlayerPosition, F: ctl.onTeleport},
		bot.PacketHandler{Priority: 50, ID: packetid.ClientboundPlayPlayerRotation, F: ctl.onRotation},
	)
	return ctl
}

// State returns a copy of the controller's state.
func (ctl *Controller) State() State {
	ctl.mu.Lock()
	defer ctl.mu.Unlock()
	return ctl.state
}

// SetInput sets the keys held from the next tick on.
func (ctl *Controller) SetInput(in Input) {
	ctl.mu.Lock()
	ctl.state.Input = in
	ctl.mu.Unlock()
}

// Look turns the player; the next tick sends it.
func (ctl *Controller) Look(yaw, pitch float32) {
	ctl.p.UpdatePosition(func(pos basic.Position) basic.Position {
		pos.Yaw, pos.Pitch = yaw, min(90, max(-90, pitch))
		return pos
	})
}

// WaitTicks returns after n more ticks, or when ctx is done.
func (ctl *Controller) WaitTicks(ctx context.Context, n int) error {
	until := ctl.State().Ticks + uint64(n)
	for ctl.State().Ticks < until {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(TickInterval / 5):
		}
	}
	return nil
}

// Run ticks until ctx is done or the connection cannot take another packet.
func (ctl *Controller) Run(ctx context.Context) error {
	t := time.NewTicker(TickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		loaded, err := ctl.tick()
		if err != nil {
			return err
		}
		if loaded && ctl.Loaded != nil {
			ctl.Loaded()
		}
	}
}

// tick is one client tick; loaded reports that this tick sent ServerboundPlayerLoaded.
func (ctl *Controller) tick() (loaded bool, err error) {
	ctl.mu.Lock()
	defer ctl.mu.Unlock()
	if !ctl.inLevel {
		return false, nil
	}
	ctl.state.Ticks++
	// ClientPacketListener.tick: the level-load tracker
	if !ctl.state.Loaded && ctl.loadStarted && ctl.levelReady() {
		if err := ctl.send(play.PlayerLoaded{}); err != nil {
			return false, err
		}
		ctl.state.Loaded, loaded = true, true
	}
	// Minecraft.tick: the entities (the player moves), then LocalPlayer.sendChanges
	if ctl.state.Loaded {
		ctl.state.Vehicle, ctl.state.Paddles = nil, nil
		ctl.state.Riding = ctl.Ride != nil && ctl.Ride(&ctl.state, ctl.p)
		if p := ctl.state.Paddles; p != nil {
			if err := ctl.send(play.PaddleBoat{Left: pk.Boolean(p[0]), Right: pk.Boolean(p[1])}); err != nil {
				return loaded, err
			}
		}
		if ctl.Act != nil {
			ctl.Act(&ctl.state, ctl.p)
		}
		if ctl.Think != nil && !ctl.state.Riding {
			ctl.Think(&ctl.state, ctl.p)
		}
		if ctl.Step != nil && !ctl.state.Riding {
			ctl.Step(&ctl.state, ctl.p)
		}
		if err := ctl.sendChanges(); err != nil {
			return loaded, err
		}
	}
	return loaded, ctl.send(play.ClientTickEnd{})
}

// levelReady is LevelLoadTracker.WaitingForPlayerChunk.isReady: the chunk the
// player is in has arrived, or the wait timed out.
func (ctl *Controller) levelReady() bool {
	if time.Now().After(ctl.loadUntil) {
		return true
	}
	pos := ctl.p.Position()
	return ctl.w.HasChunk(level.ChunkPos{int32(math.Floor(pos.X)) >> 4, int32(math.Floor(pos.Z)) >> 4})
}

// sendChanges is LocalPlayer.sendChanges: the keys, the sprint, and the
// position — or, for a passenger, the rotation.
func (ctl *Controller) sendChanges() error {
	s := &ctl.state
	if s.Input != ctl.sent.input {
		if err := ctl.send(play.PlayerInput{Input: s.Input.flags()}); err != nil {
			return err
		}
		ctl.sent.input = s.Input
	}
	// LocalPlayer.sendIsSprintingIfNeeded
	if s.Sprinting != ctl.sent.sprinting {
		action := types.PlayerCommandActionStopSprinting
		if s.Sprinting {
			action = types.PlayerCommandActionStartSprinting
		}
		if err := ctl.send(play.PlayerCommand{ID: pk.VarInt(ctl.p.Login.PlayerID), Action: action}); err != nil {
			return err
		}
		ctl.sent.sprinting = s.Sprinting
	}
	if s.Riding {
		// a passenger's position is its vehicle's business
		pos := ctl.p.Position()
		flags := types.MovePlayerPacked{OnGround: s.OnGround, HorizontalCollision: s.HorizontalCollision}
		if err := ctl.send(play.MovePlayerRot{YRot: pk.Float(pos.Yaw), XRot: pk.Float(pos.Pitch), Flags: flags}); err != nil {
			return err
		}
		ctl.sent.yaw, ctl.sent.pitch = pos.Yaw, pos.Pitch
		ctl.sent.onGround, ctl.sent.hCollided = s.OnGround, s.HorizontalCollision
		if v := s.Vehicle; v != nil {
			// ServerboundMoveVehicle: the same bytes in every 26.x, a record of
			// its own (PositionAndRotation) from 26.3
			return ctl.sendRaw(pk.Marshal(packetid.ServerboundPlayMoveVehicle,
				pk.Double(v.X), pk.Double(v.Y), pk.Double(v.Z), pk.Float(v.Yaw), pk.Float(v.Pitch), pk.Boolean(v.OnGround)))
		}
		return nil
	}
	// LocalPlayer.sendPosition
	pos := ctl.p.Position()
	dx, dy, dz := pos.X-ctl.sent.x, pos.Y-ctl.sent.y, pos.Z-ctl.sent.z
	ctl.positionReminder++
	move := dx*dx+dy*dy+dz*dz > 2e-4*2e-4 || ctl.positionReminder >= 20
	rot := pos.Yaw != ctl.sent.yaw || pos.Pitch != ctl.sent.pitch
	flags := types.MovePlayerPacked{OnGround: s.OnGround, HorizontalCollision: s.HorizontalCollision}
	var err error
	switch {
	case move && rot:
		err = ctl.send(play.MovePlayerPosRot{X: pk.Double(pos.X), Y: pk.Double(pos.Y), Z: pk.Double(pos.Z), YRot: pk.Float(pos.Yaw), XRot: pk.Float(pos.Pitch), Flags: flags})
	case move:
		err = ctl.send(play.MovePlayerPos{X: pk.Double(pos.X), Y: pk.Double(pos.Y), Z: pk.Double(pos.Z), Flags: flags})
	case rot:
		err = ctl.send(play.MovePlayerRot{YRot: pk.Float(pos.Yaw), XRot: pk.Float(pos.Pitch), Flags: flags})
	case s.OnGround != ctl.sent.onGround || s.HorizontalCollision != ctl.sent.hCollided:
		err = ctl.send(play.MovePlayerStatusOnly{Value: flags})
	}
	if err != nil {
		return err
	}
	if move {
		ctl.sent.x, ctl.sent.y, ctl.sent.z = pos.X, pos.Y, pos.Z
		ctl.positionReminder = 0
	}
	if rot {
		ctl.sent.yaw, ctl.sent.pitch = pos.Yaw, pos.Pitch
	}
	ctl.sent.onGround, ctl.sent.hCollided = s.OnGround, s.HorizontalCollision
	return nil
}

// onSpawn starts waiting for the level, as a new LocalPlayer does
// (handleLogin, handleRespawn → startWaitingForNewLevel).
func (ctl *Controller) onSpawn(pk.Packet) error {
	ctl.mu.Lock()
	defer ctl.mu.Unlock()
	ctl.inLevel = true
	ctl.loadStarted = false
	ctl.loadUntil = time.Now().Add(clientLoadTimeout)
	ctl.state.Loaded = false
	ctl.state.OnGround, ctl.state.HorizontalCollision, ctl.state.Sprinting = false, false, false
	ctl.sent = sent{input: ctl.sent.input} // a new LocalPlayer keeps the last sent input
	ctl.positionReminder = 0
	return nil
}

func (ctl *Controller) onGameEvent(packet pk.Packet) error {
	var ge play.GameEvent
	if err := packet.Scan(&ge); err != nil {
		return err
	}
	if ge.Event == gameEventLevelChunksLoadStart {
		ctl.mu.Lock()
		ctl.loadStarted = true
		ctl.mu.Unlock()
	}
	return nil
}

// onTeleport confirms a teleport basic.Player has applied.
func (ctl *Controller) onTeleport(packet pk.Packet) error {
	var pp play.PlayerPosition
	if err := packet.Scan(&pp); err != nil {
		return err
	}
	ctl.mu.Lock()
	ctl.state.Teleports++
	ctl.mu.Unlock()
	return ctl.p.AcceptTeleportation(pp.ID)
}

// onRotation answers a forced rotation with the rotation the player now has.
func (ctl *Controller) onRotation(pk.Packet) error {
	pos := ctl.p.Position()
	return ctl.send(play.MovePlayerRot{YRot: pk.Float(pos.Yaw), XRot: pk.Float(pos.Pitch)})
}

type packet interface {
	PacketID() packetid.ServerboundPacketID
	pk.FieldEncoder
}

// sendRaw writes a packet built field by field.
func (ctl *Controller) sendRaw(p pk.Packet) error {
	if err := ctl.c.Conn.WritePacket(p); err != nil {
		return errors.Join(errors.New("bot/control"), err)
	}
	return nil
}

func (ctl *Controller) send(p packet) error {
	if err := ctl.c.Conn.WritePacket(pk.Marshal(p.PacketID(), p)); err != nil {
		return errors.Join(errors.New("bot/control"), err)
	}
	return nil
}
