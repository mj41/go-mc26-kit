package simworld

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	itemdata "github.com/mj41/go-mc26/data/item"
	"github.com/mj41/go-mc26/data/loot"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/data/tags"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Digging, as the server plays it (ServerPlayerGameMode.handleBlockBreakAction):
// the client says when it starts and when it thinks the block is gone; the
// server counts the ticks since the start and takes the break if the
// progress those ticks make has reached 0.7 — else it puts the block back.
// What the break gives is the block's loot (data/loot), dropped as an item
// that lies where it fell, picked up when the player is near.

// digging is the dig the player has begun.
type digging struct {
	pos   world.BlockPos
	state block.StateID
	since int // the server tick it began
}

// item is an item entity lying in the world.
type item struct {
	id         int32
	x, y, z    float64
	vx, vy, vz float64 // its motion a tick (the game's blocks a tick)
	onGround   bool
	what       slotItem
	age        int
	delay      int // the ticks it lies before it can be picked up
}

// itemLife is how many ticks an item lies before it is gone (ItemEntity's 6000: five minutes).
const itemLife = 6000

// pickupDelay is how many ticks an item lies before it can be picked up
// (ItemEntity.pickupDelay of a block's drop: 10).
const pickupDelay = 10

// progress is what one tick of digging the state adds with what the player
// holds: the tool's speed (its rules naming block ids), 1 for anything else,
// over the hardness and 30 — 100 when the block needs a tool the player does
// not hold — a fifth of it off the ground. It is the server's own
// computation, not the client's (act.Hands.DestroyProgress): the two must agree.
func (g *Game) progress(state block.StateID) (step float32, correct bool, err error) {
	hardness := block.DestroySpeed(state)
	if hardness < 0 {
		return 0, false, fmt.Errorf("unbreakable")
	}
	correct = !block.RequiresCorrectTool(state)
	speed := float32(1)
	if h := g.heldItem(); h.n > 0 {
		if tool := act.Component[*component.Tool](itemStack(h)); tool != nil {
			speed = float32(tool.DefaultMiningSpeed)
			id, name := world.BlockID(state), block.StateList[state].ID()
			// (a rule names the blocks by ids or by a tag: the tag from the data pack, not from what the client was told)
			named := func(r component.ToolRule) bool {
				if r.Blocks.IDs != nil {
					return slices.Contains(r.Blocks.IDs, pk.VarInt(id))
				}
				return tags.BlockHas(string(r.Blocks.Tag), name)
			}
			for _, r := range tool.Rules {
				if bool(r.Speed.Has) && named(r) {
					speed = float32(r.Speed.Val)
					break
				}
			}
			if !correct {
				for _, r := range tool.Rules {
					if bool(r.CorrectForDrops.Has) && named(r) {
						correct = bool(r.CorrectForDrops.Val)
						break
					}
				}
			}
		}
	}
	if g.inWater() {
		return 0, false, fmt.Errorf("simworld: not modelled: digging with the eyes under water")
	}
	if !g.onGround {
		speed /= 5
	}
	if hardness == 0 {
		return 1, correct, nil
	}
	modifier := float32(100)
	if correct {
		modifier = 30
	}
	return speed / hardness / modifier, correct, nil
}

// bodyInWater reports whether the player's box is in water: its feet's or its middle's cell (Entity.isInWater,
// the box against the fluid's height, as far as a cell tells it).
func (g *Game) bodyInWater() bool {
	x, z := int(math.Floor(g.pos[0])), int(math.Floor(g.pos[2]))
	for _, dy := range []float64{0.01, 0.9} {
		f := block.FluidOf(g.S.World.At(x, int(math.Floor(g.pos[1]+dy)), z))
		if f != nil && (f.Name == "minecraft:water" || f.Name == "minecraft:flowing_water") {
			return true
		}
	}
	return false
}

// inWater reports whether the player's eyes are in water.
func (g *Game) inWater() bool {
	x, y, z := int(math.Floor(g.pos[0])), int(math.Floor(g.pos[1]+1.62)), int(math.Floor(g.pos[2]))
	f := block.FluidOf(g.S.World.At(x, y, z))
	return f != nil && (f.Name == "minecraft:water" || f.Name == "minecraft:flowing_water")
}

// playAction is a PlayerAction packet: the dig's start, stop or abort.
func (g *Game) playAction(p play.PlayerAction) error {
	pos := world.BlockPos{X: p.Pos.X, Y: p.Pos.Y, Z: p.Pos.Z}
	switch p.Action {
	case types.PlayerActionActionStartDestroyBlock:
		state := g.S.World.At(pos.X, pos.Y, pos.Z)
		step, _, err := g.progress(state)
		if err != nil {
			return err
		}
		g.dig = &digging{pos: pos, state: state, since: g.Tick}
		if step >= 1 { // broken at once
			return g.breakBlock(p, state)
		}
		return g.ack(p.Sequence)
	case types.PlayerActionActionAbortDestroyBlock:
		g.dig = nil
		return g.ack(p.Sequence)
	case types.PlayerActionActionStopDestroyBlock:
		d := g.dig
		g.dig = nil
		if d == nil || d.pos != pos {
			return g.revert(p, pos)
		}
		step, _, err := g.progress(d.state)
		if err != nil {
			return err
		}
		// (a client that stops on the tick its progress reaches one has counted i ticks since the start)
		if step*float32(g.Tick-d.since+1) >= 0.7 {
			return g.breakBlock(p, d.state)
		}
		// (ServerPlayerGameMode: a stop with less than 0.7 is not turned back; the dig goes on as a delayed destroy,
		// counted on from the same start at the tool the player holds, and the block breaks when it reaches one —
		// the ack comes with no update)
		if g.delayed == nil {
			g.delayed = d
		}
		return g.ack(p.Sequence)
	}
	return fmt.Errorf("simworld: not modelled: player action %d", p.Action)
}

// delayedTick is the delayed destroy's tick (ServerPlayerGameMode.tick): the block is gone, or the progress
// at the tool in hand has reached one, and it breaks.
func (g *Game) delayedTick() error {
	d := g.delayed
	if d == nil {
		return nil
	}
	if g.S.World.At(d.pos.X, d.pos.Y, d.pos.Z) != d.state {
		g.delayed = nil
		return nil
	}
	step, _, err := g.progress(d.state)
	if err != nil {
		return err
	}
	if step*float32(g.Tick-d.since+1) >= 1 {
		g.delayed = nil
		return g.breakAt(pk.Position{X: d.pos.X, Y: d.pos.Y, Z: d.pos.Z}, d.state)
	}
	return nil
}

// ack tells the bot the block changes up to the sequence are all told.
func (g *Game) ack(seq pk.VarInt) error { return g.S.Send(play.BlockChangedAck{Sequence: seq}) }

// revert puts the block back as the server has it: the bot's prediction was wrong.
func (g *Game) revert(p play.PlayerAction, pos world.BlockPos) error {
	s := g.S.World.At(pos.X, pos.Y, pos.Z)
	if err := g.S.Send(play.BlockUpdate{Pos: p.Pos, BlockState: pk.VarInt(s)}); err != nil {
		return err
	}
	return g.ack(p.Sequence)
}

// breakBlock takes the block away, tells the bot, and drops what it gives.
func (g *Game) breakBlock(p play.PlayerAction, state block.StateID) error {
	if err := g.breakAt(p.Pos, state); err != nil {
		return err
	}
	return g.ack(p.Sequence)
}

// breakAt is the break itself: the drops told, then the block's update.
func (g *Game) breakAt(at pk.Position, state block.StateID) error {
	_, correct, err := g.progress(state)
	if err != nil {
		return err
	}
	drops, err := g.dropsOf(state, correct)
	if err != nil {
		return err
	}
	x, y, z := at.X, at.Y, at.Z
	g.exhaust(exhaustBreak)
	g.wearHeld(state)
	g.S.World.Set(x, y, z, "minecraft:air")
	if f, ok := g.furnaces[[3]int{x, y, z}]; ok { // a furnace broken lets go of what it held
		for _, s := range f.s {
			if s.n > 0 {
				drops = append(drops, s)
			}
		}
		delete(g.furnaces, [3]int{x, y, z})
	}
	// (the bench's server tells the drops' entities, then the block's update: a trace has them in that order)
	for _, it := range drops {
		if err := g.popItem(x, y, z, it); err != nil {
			return err
		}
	}
	return g.S.Send(play.BlockUpdate{Pos: at, BlockState: 0})
}

// wearHeld is Item.mineBlock: a tool wears by its damage per block (the tool component's, 1 for a pick,
// axe or shovel) when it breaks a block that has any hardness, and is gone when its wear reaches what it
// stands (the max damage component) — the data's. The slot is told with the new wear, in the tick's broadcast.
// (The sound and particles of a tool breaking are not told.)
func (g *Game) wearHeld(state block.StateID) {
	h := g.heldItem()
	if h.n == 0 || block.DestroySpeed(state) == 0 {
		return
	}
	id := itemdata.ID(slices.Index(registryid.Item, h.item))
	max := itemdata.DefaultComponent[*component.MaxDamage](id)
	if max == nil {
		return
	}
	per := 1
	if tool := itemdata.DefaultComponent[*component.Tool](id); tool != nil {
		per = int(tool.DamagePerBlock)
	}
	h.damage += per
	if h.damage >= int(max.Value) {
		h = slotItem{}
	}
	g.slots[36+g.held] = h
}

// dropsOf is what breaking the state gives: the block's loot table played (data/loot) with the tool in hand,
// the block's properties and the game's chance — the same seed, the same drops. A condition or function the
// evaluator does not know stops it, naming it.
func (g *Game) dropsOf(state block.StateID, correct bool) ([]slotItem, error) {
	if block.RequiresCorrectTool(state) && !correct {
		return nil, nil
	}
	held := g.heldItem()
	tool := loot.Tool{}
	if held.n > 0 {
		tool.Item = held.item // (no enchantment is modelled: none is ever held)
	}
	return g.lootOf(state, tool, 0)
}

// lootOf is the block's loot table played with the tool, the block's properties and the game's chance; a
// blast's radius, when it broke it (0: none).
func (g *Game) lootOf(state block.StateID, tool loot.Tool, blast float64) ([]slotItem, error) {
	name := block.StateList[state].ID()
	props := map[string]string{}
	if text := world.StateString(state); strings.Contains(text, "[") {
		_, list, _ := strings.Cut(strings.TrimSuffix(text, "]"), "[")
		for _, p := range strings.Split(list, ",") {
			k, v, _ := strings.Cut(p, "=")
			props[k] = v
		}
	}
	got, err := loot.Drops(name, loot.Context{Tool: tool, Props: props, Rand: g.rng, ExplosionRadius: blast})
	if err != nil {
		return nil, fmt.Errorf("simworld: %w", err)
	}
	var out []slotItem
	for _, s := range got {
		out = append(out, slotItem{item: s.Item, n: s.Count})
	}
	return out, nil
}

// popItem is Block.popResource: the item appears in the broken block, a quarter of a block off its middle
// each way, and is thrown up a little (0.2 a tick, the sideways ±0.1); it falls and lands (itemsTick).
func (g *Game) popItem(bx, by, bz int, what slotItem) error {
	r := func(lo, hi float64) float64 { return lo + g.motion.Float64()*(hi-lo) }
	const h = 0.25 // the item's height
	return g.addItem(float64(bx)+0.5+r(-0.25, 0.25), float64(by)+0.5+r(-0.25, 0.25)-h/2, float64(bz)+0.5+r(-0.25, 0.25),
		r(-0.1, 0.1), 0.2, r(-0.1, 0.1), what, pickupDelay)
}

// spawnItem puts an item entity down at rest on the ground below: what the player throws lands there (its
// flight is not simulated), and the bot is told it came.
func (g *Game) spawnItem(x float64, y int, z float64, what slotItem, delay int) error {
	ground := float64(y)
	for by := y - 1; by > -64; by-- {
		if len(block.CollisionShape(g.S.World.At(int(math.Floor(x)), by, int(math.Floor(z))))) > 0 {
			ground = float64(by + 1)
			break
		}
	}
	return g.addItem(x, ground, z, 0, 0, 0, what, delay)
}

// addItem puts an item entity at the place with the motion, and tells the bot it came. (Its moves are
// not told: the bot learns of the entity where it began.)
func (g *Game) addItem(x, y, z, vx, vy, vz float64, what slotItem, delay int) error {
	g.nextID++
	it := &item{id: g.nextID, x: x, y: y, z: z, vx: vx, vy: vy, vz: vz, what: what, delay: delay}
	g.items = append(g.items, it)
	var u pk.UUID
	u[15], u[14] = byte(it.id), byte(it.id>>8)
	if err := g.S.Send(play.AddEntity{ID: pk.VarInt(it.id), UUID: u, Type: pk.VarInt(slices.Index(registryid.EntityType, "minecraft:item")),
		X: pk.Double(x), Y: pk.Double(y), Z: pk.Double(z)}); err != nil {
		return err
	}
	return g.S.Send(play.SetEntityData{ID: pk.VarInt(it.id), PackedItems: types.EntityData{{Index: entitydata.ItemEntityItem, Serializer: 7, Value: ptr(itemStack(what))}}})
}

// itemsTick is ItemEntity.tick for each item: gravity (0.04 a tick), the move against the blocks, the drag
// (0.98 a tick, and the ground's friction sideways: the block's slipperiness 0.6 times 0.98), a bounce
// off the ground (the fall's speed halved and turned up — it is at rest by then). Merging of near items
// is not modelled (the bot's items lie apart).
func (g *Game) itemsTick() error {
	for _, it := range g.items {
		// in water an item rises and floats (ItemEntity.setUnderwaterMovement: no gravity there, a
		// little up, slowed) — it does not sink to the floor of the pond it fell in
		if f := block.FluidOf(g.S.World.At(int(math.Floor(it.x)), int(math.Floor(it.y+0.1)), int(math.Floor(it.z)))); f != nil &&
			(f.Name == "minecraft:water" || f.Name == "minecraft:flowing_water") {
			if it.vy < 0.06 {
				it.vy += 5e-4
			}
			it.vx, it.vz = it.vx*0.99, it.vz*0.99
			it.y, it.vy, it.onGround = g.moveAxis(it, 1, it.vy)
			it.x, it.vx, _ = g.moveAxis(it, 0, it.vx)
			it.z, it.vz, _ = g.moveAxis(it, 2, it.vz)
			// the water's drag (the game's item settles at the top with a small bob; its own rule, the
			// fluid's height against the item's eye, is not modelled): its fall back in from the top
			// slowed hard — with 0.98 alone it bobbed a block and more, out of a swimmer's reach — its
			// rise not (from a pond's floor it comes up in seconds)
			fall := 0.98
			if it.vy < 0 {
				fall = 0.8
			}
			it.vx, it.vy, it.vz = it.vx*0.98, it.vy*fall, it.vz*0.98
			if it.age%10 == 0 { // (where it floats told, as the game's tracker tells a moving item)
				if err := g.S.Send(positionSync(it.id, it.x, it.y, it.z, 0, 0, false)); err != nil {
					return err
				}
			}
			continue
		}
		it.vy -= 0.04
		// move: down or up first, then each way sideways (Entity.collide moves the axes in that order)
		it.y, it.vy, it.onGround = g.moveAxis(it, 1, it.vy)
		it.x, it.vx, _ = g.moveAxis(it, 0, it.vx)
		it.z, it.vz, _ = g.moveAxis(it, 2, it.vz)
		f := 0.98
		if it.onGround {
			f = 0.6 * 0.98 // (the ground is not ice, slime or soul sand in any world the bot is put in)
		}
		it.vx, it.vy, it.vz = it.vx*f, it.vy*0.98, it.vz*f
		if it.onGround && it.vy < 0 {
			it.vy *= -0.5
		}
	}
	return nil
}

// moveAxis moves the item along the axis (0 x, 1 y, 2 z) by d, stopped by the blocks' collision shapes: it
// returns where it is, the speed (0 when it hit something) and, for y, whether it came down on a block.
func (g *Game) moveAxis(it *item, axis int, d float64) (pos, v float64, ground bool) {
	at := [3]float64{it.x, it.y, it.z}
	moved := g.clip(at, 0.125, 0.25, axis, d)
	if moved != d {
		v = 0
	} else {
		v = d
	}
	return at[axis] + moved, v, axis == 1 && d < 0 && moved != d
}

// clip is how far a box (half its width, its height, standing at its feet) can move along the axis by d
// before a block's collision shape stops it: d when nothing does.
func (g *Game) clip(at [3]float64, half, height float64, axis int, d float64) float64 {
	lo := [3]float64{at[0] - half, at[1], at[2] - half}
	hi := [3]float64{at[0] + half, at[1] + height, at[2] + half}
	moved := d
	const eps = 1e-7
	a, b := lo[axis], hi[axis]
	sweepLo, sweepHi := math.Min(a, a+d), math.Max(b, b+d)
	var r0, r1 [3]int
	for i := 0; i < 3; i++ {
		l, h := lo[i], hi[i]
		if i == axis {
			l, h = sweepLo, sweepHi
		}
		r0[i], r1[i] = int(math.Floor(l+eps)), int(math.Floor(h-eps))
	}
	for bx := r0[0]; bx <= r1[0]; bx++ {
		for by := r0[1]; by <= r1[1]; by++ {
			for bz := r0[2]; bz <= r1[2]; bz++ {
				for _, c := range block.CollisionShape(g.S.World.At(bx, by, bz)) {
					cl := [3]float64{float64(bx) + c.MinX, float64(by) + c.MinY, float64(bz) + c.MinZ}
					ch := [3]float64{float64(bx) + c.MaxX, float64(by) + c.MaxY, float64(bz) + c.MaxZ}
					overlap := true
					for i := 0; i < 3; i++ {
						if i != axis && !(hi[i] > cl[i]+eps && lo[i] < ch[i]-eps) {
							overlap = false
						}
					}
					if !overlap {
						continue
					}
					if d > 0 && hi[axis] <= cl[axis]+eps {
						moved = math.Min(moved, cl[axis]-hi[axis])
					} else if d < 0 && lo[axis] >= ch[axis]-eps {
						moved = math.Max(moved, ch[axis]-lo[axis])
					}
				}
			}
		}
	}
	return moved
}

func ptr[T any](v T) *T { return &v }

// pickups lets the player take the items near it (Player.aiStep: its box grown
// by one block sideways and half a block up and down, an item that has lain
// ten ticks): each taken is told, the inventory slot set.
func (g *Game) pickups() error {
	for i := 0; i < len(g.items); i++ {
		it := g.items[i]
		it.age++
		if it.age >= itemLife { // gone, as the game's items are after five minutes (ItemEntity.tick: age 6000)
			g.items = slices.Delete(g.items, i, i+1)
			i--
			g.taken = append(g.taken, it.id)
			continue
		}
		if it.age < it.delay {
			continue
		}
		const hw = 0.125
		if math.Abs(it.x-g.pos[0]) > 0.3+1.0+hw || math.Abs(it.z-g.pos[2]) > 0.3+1.0+hw ||
			it.y+0.25 < g.pos[1]-0.5 || it.y > g.pos[1]+1.8+0.5 {
			continue
		}
		if left := g.add(it.what.item, it.what.n); left == it.what.n {
			continue // no room
		}
		g.items = slices.Delete(g.items, i, i+1)
		i--
		if err := g.S.Send(play.TakeItemEntity{ItemID: pk.VarInt(it.id), PlayerID: 1, Amount: pk.VarInt(it.what.n)}); err != nil {
			return err
		}
		g.taken = append(g.taken, it.id) // (told gone after the slot: the bench's order is the pick-up, the slot, the removal)
	}
	return nil
}

// removeTaken tells the bot the items it took are gone.
func (g *Game) removeTaken() error {
	if len(g.taken) == 0 {
		return nil
	}
	ids := make([]pk.VarInt, len(g.taken))
	for i, id := range g.taken {
		ids[i] = pk.VarInt(id)
	}
	g.taken = nil
	return g.S.Send(play.RemoveEntities{EntityIds: ids})
}

// Items is how many item entities lie in the world.
func (g *Game) Items() int { return len(g.items) }

// ItemsAt says the items lying in the world and where (a debugging aid).
func (g *Game) ItemsAt() string {
	var s string
	for _, it := range g.items {
		s += fmt.Sprintf("[%s %.1f %.1f %.1f]", it.what.item, it.x, it.y, it.z)
	}
	return s
}

// Drop puts n of the item in the world, popped out of the block at x y z as a broken block's drop (a test's
// item lying about: a drop that rolled off, an item thrown).
func (g *Game) Drop(item string, n, x, y, z int) error {
	return g.popItem(x, y, z, slotItem{item: item, n: n})
}
