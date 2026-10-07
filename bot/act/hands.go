package act

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/types"
)

// Hands acts for a player: set a control.Controller's Act to its Act. One
// action at a time, as a person has one mouse.
//
// An action's done (nil: none) is called on the controller's tick goroutine
// while the hands and the controller are held: it must not call back into
// Hands or the Controller (Busy, Dig, State… would wait for themselves). Hand
// the result on instead — a channel, as waiting for an action does.
type Hands struct {
	C       *bot.Client
	World   *world.World
	Screens *screen.Manager

	mu          sync.Mutex
	sequence    int32 // BlockStatePredictionHandler.currentSequenceNr
	job         job
	attackTicks int          // Player.attackStrengthTicker: ticks since the last attack
	swungAt     atomic.Int64 // when the arm last swung (Swinging), Unix nanoseconds
}

// job is the action in progress; its tick returns true when it is over.
type job interface {
	tick(h *Hands, s *control.State, bp *basic.Player) (done bool)
}

// Target is the block the hands are working on now — being dug ("dig"), a
// block being put at ("place"), a block being used ("use") — for a watcher
// to see; ok is false when they are idle or on something else (an attack,
// eating).
func (h *Hands) Target() (pos world.BlockPos, what string, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch j := h.job.(type) {
	case *dig:
		return j.pos, "dig", true
	case *use:
		if j.place {
			return j.target, "place", true
		}
		return j.target, "use", true
	}
	return world.BlockPos{}, "", false
}

// Act is a control.Controller's Act: one tick of the action in progress,
// before the player moves (Minecraft.tick: handleKeybinds, then the entities).
func (h *Hands) Act(s *control.State, bp *basic.Player) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.attackTicks++
	if h.job != nil && h.job.tick(h, s, bp) {
		h.job = nil
	}
}

func (h *Hands) start(j job) {
	h.mu.Lock()
	h.job = j
	h.mu.Unlock()
}

// DigProgress is how far the dig in progress has got (0–1, the client's
// destroyProgress), and whether one is.
func (h *Hands) DigProgress() (float32, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d, ok := h.job.(*dig); ok {
		return min(d.progress, 1), true
	}
	return 0, false
}

// Busy reports whether an action is in progress.
func (h *Hands) Busy() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.job != nil
}

// nextSequence is startPredicting: every action the server confirms with
// block_changed_ack carries the next number.
func (h *Hands) nextSequence() pk.VarInt {
	h.sequence++
	return pk.VarInt(h.sequence)
}

// held is the stack in the selected hotbar slot.
func (h *Hands) held() types.ItemStack {
	h.Screens.Lock()
	defer h.Screens.Unlock()
	slot, _ := screen.MenuSlot(h.Screens.HeldSlot)
	return h.Screens.Inventory.Slots[slot]
}

type packet interface {
	PacketID() packetid.ServerboundPacketID
	pk.FieldEncoder
}

func pkVarInt(v int32) pk.VarInt { return pk.VarInt(v) }

func (h *Hands) send(p packet) error {
	return h.C.Conn.WritePacket(pk.Marshal(p.PacketID(), p))
}

// swing sends the arm swing of the main hand: ServerboundPunch from 26.3, an
// empty packet; ServerboundSwing with the hand before. Neither type exists in
// every version, so the packet is found by its name.
// Swinging reports whether its arm swung within the last third of a second
// (a dig, an attack, a placing): for a watcher to draw.
func (h *Hands) Swinging() bool {
	return time.Since(time.Unix(0, h.swungAt.Load())) < 300*time.Millisecond
}

func (h *Hands) swing() error {
	h.swungAt.Store(time.Now().UnixNano())
	if id, ok := serverboundID("ServerboundPunch"); ok {
		return h.C.Conn.WritePacket(pk.Marshal(id))
	}
	id, _ := serverboundID("ServerboundSwing")
	return h.C.Conn.WritePacket(pk.Marshal(id, pk.VarInt(0))) // InteractionHand.MAIN_HAND
}

var serverboundIDs = sync.OnceValue(func() map[string]packetid.ServerboundPacketID {
	m := map[string]packetid.ServerboundPacketID{}
	for i := packetid.ServerboundPacketID(0); ; i++ {
		name := i.String()
		if strings.HasPrefix(name, "ServerboundPacketID(") {
			if i > 512 {
				return m
			}
			continue
		}
		m[name] = i
	}
})

func serverboundID(name string) (packetid.ServerboundPacketID, bool) {
	id, ok := serverboundIDs()[name]
	return id, ok
}

// Eye is the position of the player's eyes (1.62 above the feet standing,
// 1.27 sneaking).
func Eye(p basic.Position, crouching bool) (x, y, z float64) {
	h := 1.62
	if crouching {
		h = 1.27
	}
	return p.X, p.Y + h, p.Z
}

// LookAt turns the player toward a point, as a person moving the mouse there.
func LookAt(bp *basic.Player, crouching bool, x, y, z float64) {
	bp.UpdatePosition(func(p basic.Position) basic.Position {
		ex, ey, ez := Eye(p, crouching)
		dx, dy, dz := x-ex, y-ey, z-ez
		p.Yaw = float32(-math.Atan2(dx, dz) * 180 / math.Pi)
		p.Pitch = float32(-math.Atan2(dy, math.Hypot(dx, dz)) * 180 / math.Pi)
		return p
	})
}

// Face is a block face, as Direction orders them: down, up, north, south, west, east.
type Face uint8

const (
	Down Face = iota
	Up
	North
	South
	West
	East
)

// facing returns the face of the block at pos a ray from the eye toward the
// block's centre enters through (the face a person aiming at it sees).
func facing(ex, ey, ez float64, pos world.BlockPos) Face {
	cx, cy, cz := float64(pos.X)+0.5, float64(pos.Y)+0.5, float64(pos.Z)+0.5
	dx, dy, dz := cx-ex, cy-ey, cz-ez
	ax, ay, az := math.Abs(dx), math.Abs(dy), math.Abs(dz)
	switch {
	case ay >= ax && ay >= az:
		if dy > 0 {
			return Down
		}
		return Up
	case ax >= az:
		if dx > 0 {
			return West
		}
		return East
	default:
		if dz > 0 {
			return North
		}
		return South
	}
}
