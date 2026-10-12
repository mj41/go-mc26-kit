package simworld

import (
	"math"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// bucketReach is how far from the eyes a bucket reaches (the player's block interaction range).
const bucketReach = 4.5

// useBucket is a UseItem with a bucket in hand (BucketItem.use): the look's ray from the eyes, along
// the yaw and pitch the packet carries, to the first block it meets within reach — an empty bucket
// takes up the water source it meets first (the ray stops at a block with a shape), and is a water
// bucket; a water bucket pours a source into the cell before the block it meets (air), and is a
// bucket. A source with two sources beside it and a source or a block under it fills again at once
// (the game's infinite water: the fluid's next tick).
func (g *Game) useBucket(u play.UseItem) error {
	held := g.heldItem()
	yaw, pitch := float64(u.YRot)*math.Pi/180, float64(u.XRot)*math.Pi/180
	dx, dy, dz := -math.Sin(yaw)*math.Cos(pitch), -math.Sin(pitch), math.Cos(yaw)*math.Cos(pitch)
	ex, ey, ez := g.pos[0], g.pos[1]+1.62, g.pos[2]
	water := fakeserver.State("minecraft:water")
	prev := [3]int{int(math.Floor(ex)), int(math.Floor(ey)), int(math.Floor(ez))}
	for d := 0.0; d <= bucketReach; d += 0.05 {
		c := [3]int{int(math.Floor(ex + dx*d)), int(math.Floor(ey + dy*d)), int(math.Floor(ez + dz*d))}
		if c == prev && d > 0 {
			continue
		}
		s := g.S.World.At(c[0], c[1], c[2])
		if held.item == "minecraft:bucket" && s == water {
			if !g.refills(c) {
				g.S.World.Set(c[0], c[1], c[2], "minecraft:air")
				if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: c[0], Y: c[1], Z: c[2]}, BlockState: 0}); err != nil {
					return err
				}
			}
			g.swapHeld("minecraft:water_bucket")
			return g.ack(u.Sequence)
		}
		if len(block.CollisionShape(s)) > 0 {
			if held.item == "minecraft:water_bucket" && g.S.World.At(prev[0], prev[1], prev[2]) == 0 && !g.inPlayer(prev[0], prev[1], prev[2]) {
				g.S.World.SetState(prev[0], prev[1], prev[2], water)
				if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: prev[0], Y: prev[1], Z: prev[2]}, BlockState: pk.VarInt(water)}); err != nil {
					return err
				}
				g.swapHeld("minecraft:bucket")
			}
			return g.ack(u.Sequence)
		}
		prev = c
	}
	return g.ack(u.Sequence) // (nothing in reach: the use does nothing)
}

// refills: a water source taken up comes again — two sources beside it, a source or a block under it.
func (g *Game) refills(c [3]int) bool {
	water := fakeserver.State("minecraft:water")
	n := 0
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if g.S.World.At(c[0]+d[0], c[1], c[2]+d[1]) == water {
			n++
		}
	}
	under := g.S.World.At(c[0], c[1]-1, c[2])
	return n >= 2 && (under == water || len(block.CollisionShape(under)) > 0)
}

// swapHeld turns one of the item in hand into the other (a bucket filled or emptied): the slot itself
// when it held one, else one taken from it and the other given.
func (g *Game) swapHeld(to string) {
	slot := &g.slots[36+g.held]
	if slot.n == 1 {
		*slot = slotItem{item: to, n: 1}
	} else {
		slot.n--
		g.add(to, 1)
	}
	g.broadcast()
}
