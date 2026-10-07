package act

import (
	"errors"
	"math"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Dig is the result of a dig: how many ticks it took, from the start packet
// to the stop packet, and the block it broke.
type Dig struct {
	Ticks int
	Was   block.StateID
	// Airborne counts the ticks of digging not standing on the ground, each
	// worth a fifth of a tick on it.
	Airborne int
	Err      error
}

// ErrUnbreakable: the block has no destroy time (bedrock).
var ErrUnbreakable = errors.New("act: the block cannot be broken")

// Dig breaks the block at pos with the held item, as a person holding the
// attack key on it does (MultiPlayerGameMode.startDestroyBlock and
// continueDestroyBlock): it looks at it, starts, adds the progress of every
// tick, swings the arm, and stops when the progress reaches one. done is
// called when the block is broken or the dig failed.
func (h *Hands) Dig(pos world.BlockPos, done func(Dig)) {
	h.start(&dig{pos: pos, done: done})
}

// ErrCancelled ends a dig given up with [Hands.CancelDig].
var ErrCancelled = errors.New("act: the dig was given up")

// CancelDig gives up the dig in progress, as a player who lets go of the
// button: the server is told (abort_destroy_block) and done gets
// [ErrCancelled] on the next tick. Nothing happens when the hands are not
// digging.
func (h *Hands) CancelDig() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d, ok := h.job.(*dig); ok {
		d.cancel = true
	}
}

type dig struct {
	pos      world.BlockPos
	done     func(Dig)
	aimed    bool
	started  bool
	was      block.StateID
	face     Face
	progress float32
	ticks    int
	airborne int
	cancel   bool
}

func (d *dig) finish(r Dig) bool {
	if d.done != nil {
		d.done(r)
	}
	return true
}

func (d *dig) tick(h *Hands, s *control.State, bp *basic.Player) bool {
	if d.cancel {
		if d.started {
			if err := h.send(play.PlayerAction{Action: types.PlayerActionActionAbortDestroyBlock, Pos: d.pk(), Direction: pk.UnsignedByte(d.face), Sequence: h.nextSequence()}); err != nil {
				return d.finish(Dig{Err: err})
			}
		}
		return d.finish(Dig{Err: ErrCancelled, Ticks: d.ticks})
	}
	state, ok := h.World.BlockAt(d.pos)
	if !ok {
		return d.finish(Dig{Err: errors.New("act: the block is not loaded")})
	}
	if !d.aimed {
		// a person looks at the block first; the rotation goes out with this tick
		ex, ey, ez := Eye(bp.Position(), s.Input.Shift)
		d.face = facing(ex, ey, ez, d.pos)
		LookAt(bp, s.Input.Shift, float64(d.pos.X)+0.5, float64(d.pos.Y)+0.5, float64(d.pos.Z)+0.5)
		d.aimed = true
		return false
	}
	if block.IsAir(state) {
		if d.started {
			return d.finish(Dig{Ticks: d.ticks, Was: d.was})
		}
		return d.finish(Dig{Err: errors.New("act: nothing to dig")})
	}
	if block.DestroySpeed(state) < 0 {
		return d.finish(Dig{Err: ErrUnbreakable})
	}
	step := h.DestroyProgress(state, s, bp)
	if !d.started {
		d.started, d.was = true, state
		if err := h.send(play.PlayerAction{Action: types.PlayerActionActionStartDestroyBlock, Pos: d.pk(), Direction: pk.UnsignedByte(d.face), Sequence: h.nextSequence()}); err != nil {
			return d.finish(Dig{Err: err})
		}
		if step >= 1 {
			h.World.SetBlock(d.pos, emptyOf(state)) // broken at once, as the client predicts
			return d.finish(Dig{Ticks: 0, Was: state})
		}
		return false
	}
	d.progress += step
	d.ticks++
	if !s.OnGround {
		d.airborne++
	}
	if err := h.swing(); err != nil {
		return d.finish(Dig{Err: err})
	}
	if d.progress >= 1 {
		if err := h.send(play.PlayerAction{Action: types.PlayerActionActionStopDestroyBlock, Pos: d.pk(), Direction: pk.UnsignedByte(d.face), Sequence: h.nextSequence()}); err != nil {
			return d.finish(Dig{Err: err})
		}
		h.World.SetBlock(d.pos, emptyOf(state))
		return d.finish(Dig{Ticks: d.ticks, Was: d.was, Airborne: d.airborne})
	}
	return false
}

func (d *dig) pk() pk.Position { return pk.Position{X: d.pos.X, Y: d.pos.Y, Z: d.pos.Z} }

// emptyOf is what is left of a broken block: its fluid (a waterlogged slab
// leaves water), air otherwise.
func emptyOf(state block.StateID) block.StateID {
	if f := block.FluidOf(state); f != nil && f.Source {
		if f.Name == "minecraft:water" {
			return block.ToStateID[block.Water{}]
		}
	}
	return block.ToStateID[block.Air{}]
}

// ToolSpeed is how fast a stack mines a block state, before the player's own
// modifiers, and whether it gets the block's drop (Tool.getMiningSpeed,
// ItemStack.isCorrectToolForDrops): the first tool rule naming the block,
// else the tool's default speed; 1 for anything that is not a tool.
func ToolSpeed(tags *bot.Tags, s types.ItemStack, state block.StateID) (float32, bool) {
	speed, correct := float32(1), !block.RequiresCorrectTool(state)
	tool := Component[*component.Tool](s)
	if tool == nil {
		return speed, correct
	}
	speed = float32(tool.DefaultMiningSpeed)
	id := world.BlockID(state)
	for _, r := range tool.Rules {
		if bool(r.Speed.Has) && inSet(tags, r.Blocks.Tag, r.Blocks.IDs, id) {
			speed = float32(r.Speed.Val)
			break
		}
	}
	if !correct {
		for _, r := range tool.Rules {
			if bool(r.CorrectForDrops.Has) && inSet(tags, r.Blocks.Tag, r.Blocks.IDs, id) {
				correct = bool(r.CorrectForDrops.Val)
				break
			}
		}
	}
	return speed, correct
}

// DestroyProgress is what one tick of digging adds (BlockBehaviour.getDestroyProgress
// with Player.getDestroySpeed): the tool's speed for the block, the mining
// efficiency, haste and mining fatigue, the break speed attribute, the eye
// under water, not standing on the ground; over the block's hardness, and 30
// with the right tool for its drops, 100 without.
func (h *Hands) DestroyProgress(state block.StateID, s *control.State, bp *basic.Player) float32 {
	hardness := block.DestroySpeed(state)
	if hardness < 0 {
		return 0
	}
	speed, correct := ToolSpeed(&h.C.Tags, h.held(), state)
	attr := func(name string, def float64) float64 {
		if a, ok := bp.Attribute(name); ok {
			return a.Value()
		}
		return def
	}
	if speed > 1 {
		speed += float32(attr("minecraft:mining_efficiency", 0))
	}
	st := bp.Status()
	if amp, ok := maxAmplifier(st, "minecraft:haste", "minecraft:conduit_power"); ok {
		speed *= 1 + float32(amp+1)*0.2
	}
	if e, ok := st.Effects["minecraft:mining_fatigue"]; ok {
		speed *= float32(math.Pow(0.3, float64(e.Amplifier+1)))
	}
	speed *= float32(attr("minecraft:block_break_speed", 1))
	if h.eyeInWater(bp, s.Input.Shift) {
		speed *= float32(attr("minecraft:submerged_mining_speed", 0.2))
	}
	if !s.OnGround {
		speed /= 5
	}
	modifier := float32(100)
	if correct {
		modifier = 30
	}
	if hardness == 0 {
		return 1 // breaks at once
	}
	return speed / hardness / modifier
}

func maxAmplifier(st basic.Status, names ...string) (int, bool) {
	best, found := -1, false
	for _, n := range names {
		if e, ok := st.Effects[n]; ok && e.Amplifier > best {
			best, found = e.Amplifier, true
		}
	}
	return best, found
}

// inSet reports whether block id is in a tool rule's block set: a tag the
// server sent, or a list of ids.
func inSet(tags *bot.Tags, tag pk.Identifier, ids []pk.VarInt, id int32) bool {
	if ids == nil {
		return tags.Has("minecraft:block", string(tag), id)
	}
	for _, i := range ids {
		if int32(i) == id {
			return true
		}
	}
	return false
}

// eyeInWater is Entity.isEyeInFluid(WATER): the eye below the water's surface.
func (h *Hands) eyeInWater(bp *basic.Player, crouching bool) bool {
	ex, ey, ez := Eye(bp.Position(), crouching)
	pos := world.BlockPos{X: int(math.Floor(ex)), Y: int(math.Floor(ey)), Z: int(math.Floor(ez))}
	s, ok := h.World.BlockAt(pos)
	if !ok {
		return false
	}
	f := block.FluidOf(s)
	if f == nil || (f.Name != "minecraft:water" && f.Name != "minecraft:flowing_water") {
		return false
	}
	return ey < float64(pos.Y)+float64(f.Height)+1.0/9
}
