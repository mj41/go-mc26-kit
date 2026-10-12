package simworld

import (
	"fmt"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// UseItemOn, as the server plays it (ServerPlayerGameMode.useItemOn): on a
// crafting table it opens the table's menu; with a hoe it tills earth, with
// seeds it sows the cell over farmland; with a block in hand it puts the block against the
// face. The packet is read field by field, as the bot writes
// it (the hit's field is named differently across versions): the hand, the
// block, the face, where on it, inside, the world border, the sequence.
func (g *Game) useItemOn(p fakeserver.Packet) error {
	var hand, face, seq pk.VarInt
	var pos pk.Position
	var cx, cy, cz pk.Float
	var inside, border pk.Boolean
	if err := p.Scan(&hand, &pos, &face, &cx, &cy, &cz, &inside, &border, &seq); err != nil {
		return err
	}
	at := g.S.World.At(pos.X, pos.Y, pos.Z)
	name := block.StateList[at].ID()
	switch {
	case name == "minecraft:crafting_table":
		if err := g.openTable(); err != nil {
			return err
		}
	case name == "minecraft:furnace":
		if err := g.openFurnace([3]int{pos.X, pos.Y, pos.Z}); err != nil {
			return err
		}
	default:
		held := g.heldItem()
		if held.n == 0 {
			return g.ack(seq) // (an empty hand on a block that has no use: nothing happens)
		}
		// a hoe on earth with air over it tills it (HoeItem.useOn); seeds on
		// farmland's top with air over it are sown (the wheat's first age)
		over := g.S.World.At(pos.X, pos.Y+1, pos.Z) == 0
		switch {
		case strings.HasSuffix(held.item, "_hoe"):
			if over && (name == "minecraft:dirt" || name == "minecraft:grass_block" || name == "minecraft:dirt_path") && int(face) != 0 {
				return g.setAndAck(pos.X, pos.Y, pos.Z, "minecraft:farmland", seq)
			}
			return g.ack(seq)
		case held.item == "minecraft:wheat_seeds":
			// the seed goes where the face looks (BlockItem.place: the block
			// clicked and its face — a farmland's top, or a side of a block
			// beside the cell), if that is air and farmland is under it
			// (CropBlock.canSurvive)
			d := faceOffset(int(face))
			x, y, z := pos.X+d[0], pos.Y+d[1], pos.Z+d[2]
			if g.S.World.At(x, y, z) == 0 && block.StateList[g.S.World.At(x, y-1, z)].ID() == "minecraft:farmland" {
				g.takeHeld()
				g.crops[[3]int{x, y, z}] = true
				return g.setAndAck(x, y, z, "minecraft:wheat", seq)
			}
			return g.ack(seq)
		}
		// a pickaxe or a sword has no use on a block (no useOn): nothing happens
		if strings.HasSuffix(held.item, "_pickaxe") || strings.HasSuffix(held.item, "_sword") {
			return g.ack(seq)
		}
		blockName, ok := placeable(held.item)
		if !ok {
			return fmt.Errorf("simworld: not modelled: using %s on %s", held, name)
		}
		d := faceOffset(int(face))
		x, y, z := pos.X+d[0], pos.Y+d[1], pos.Z+d[2]
		if g.S.World.At(x, y, z) != 0 { // (replaceable blocks — water, grass — are not modelled)
			return g.ack(seq)
		}
		if g.inPlayer(x, y, z) { // a block where the player stands is refused (BlockItem: the shape collides with an entity)
			if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x, Y: y, Z: z}, BlockState: pk.VarInt(g.S.World.At(x, y, z))}); err != nil {
				return err
			}
			return g.ack(seq)
		}
		switch blockName {
		case "minecraft:furnace":
			g.placeFurnace(x, y, z)
		case "minecraft:chest": // a single chest facing the player (one beside another is not joined: not modelled)
			g.S.World.SetState(x, y, z, stateByText("minecraft:chest[facing="+g.facingOf()+",type=single,waterlogged=false]"))
		case "minecraft:torch": // on the ground a torch; on a wall a wall torch pointing away from it
			if int(face) == 0 {
				return g.ack(seq) // (hung from a ceiling: it does not hold)
			}
			if int(face) == 1 {
				g.S.World.Set(x, y, z, "minecraft:torch")
			} else {
				g.S.World.SetState(x, y, z, stateByText("minecraft:wall_torch[facing="+[...]string{"", "", "north", "south", "west", "east"}[int(face)]+"]"))
			}
		default:
			g.S.World.Set(x, y, z, blockName)
		}
		g.takeHeld()
		if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x, Y: y, Z: z}, BlockState: pk.VarInt(g.S.World.At(x, y, z))}); err != nil {
			return err
		}
		g.broadcast()
	}
	return g.ack(seq)
}

// inPlayer: the block's cube overlaps the player's box (0.6 wide, 1.8 high).
func (g *Game) inPlayer(x, y, z int) bool {
	p := g.pos
	return float64(x) < p[0]+playerHalf && float64(x+1) > p[0]-playerHalf &&
		float64(y) < p[1]+playerHigh && float64(y+1) > p[1] &&
		float64(z) < p[2]+playerHalf && float64(z+1) > p[2]-playerHalf
}

// takeHeld uses up one of the item in hand.
func (g *Game) takeHeld() {
	g.slots[36+g.held].n--
	if g.slots[36+g.held].n == 0 {
		g.slots[36+g.held] = slotItem{}
	}
}

// setAndAck changes a block (its plain state), tells the player and the
// others, and answers the use.
func (g *Game) setAndAck(x, y, z int, name string, seq pk.VarInt) error {
	g.S.World.Set(x, y, z, name)
	if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x, Y: y, Z: z}, BlockState: pk.VarInt(g.S.World.At(x, y, z))}); err != nil {
		return err
	}
	g.broadcast()
	return g.ack(seq)
}

// placeable is the block an item puts down: the blocks whose plain state is
// the right one (no facing, no half) — the others are not modelled.
func placeable(item string) (string, bool) {
	switch {
	case item == "minecraft:torch", item == "minecraft:crafting_table", item == "minecraft:furnace", item == "minecraft:chest", item == "minecraft:cobblestone", item == "minecraft:dirt", item == "minecraft:stone",
		strings.HasSuffix(item, "_planks"):
		return item, true
	}
	return "", false
}

// faceOffset is the way a face (down, up, north, south, west, east) looks.
func faceOffset(face int) [3]int {
	return [...][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}[face]
}

// stateByText is the state the game writes as text (all properties).
func stateByText(text string) block.StateID {
	for i := range block.StateList {
		if world.StateString(block.StateID(i)) == text {
			return block.StateID(i)
		}
	}
	panic("no state " + text)
}
