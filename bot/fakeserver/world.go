package fakeserver

import (
	"fmt"
	"sync"

	"github.com/mj41/go-mc26/level"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// World is the fake server's world, made by hand: a flat ground (stone up
// to 60, dirt 61 and 62, grass at 63 — the bot stands at 64) unless a test
// says otherwise, and the blocks a test sets over it. Its chunks are sent to
// the bot round the spawn when it joins.
type World struct {
	mu     sync.Mutex
	set    map[[3]int]block.StateID
	ground func(y int) block.StateID // the flat world's layer at y
	Radius int                       // chunks sent round the spawn's (2: five by five)
}

// NewWorld is the flat world.
func NewWorld() *World {
	stone, dirt, grass := State("minecraft:stone"), State("minecraft:dirt"), State("minecraft:grass_block")
	bedrock := State("minecraft:bedrock")
	return &World{set: map[[3]int]block.StateID{}, Radius: 2, ground: func(y int) block.StateID {
		switch {
		case y == -64:
			return bedrock
		case y <= 60:
			return stone
		case y <= 62:
			return dirt
		case y == 63:
			return grass
		}
		return 0 // air
	}}
}

// State is the block's state by its name, its plain state (no properties
// set): minecraft:stone, minecraft:oak_log.
func State(name string) block.StateID {
	b, ok := block.FromID[name]
	if !ok {
		panic(fmt.Sprintf("fakeserver: no block %q", name))
	}
	return block.ToStateID[b]
}

// Set puts the block at x, y, z (before the bot joins; after it, Server.SetBlock).
func (w *World) Set(x, y, z int, name string) { w.setState(x, y, z, State(name)) }

func (w *World) setState(x, y, z int, s block.StateID) {
	w.mu.Lock()
	w.set[[3]int{x, y, z}] = s
	w.mu.Unlock()
}

// SetState puts the block state at x, y, z (before the bot joins).
func (w *World) SetState(x, y, z int, s block.StateID) { w.setState(x, y, z, s) }

// Fill puts the block in every cell of the box (its corners in it).
func (w *World) Fill(x1, y1, z1, x2, y2, z2 int, name string) {
	s := State(name)
	for x := min(x1, x2); x <= max(x1, x2); x++ {
		for y := min(y1, y2); y <= max(y1, y2); y++ {
			for z := min(z1, z2); z <= max(z1, z2); z++ {
				w.setState(x, y, z, s)
			}
		}
	}
}

// Each calls fn for each block set by hand (over the flat ground), in no order.
func (w *World) Each(fn func(x, y, z int, s block.StateID)) {
	w.mu.Lock()
	cells := make(map[[3]int]block.StateID, len(w.set))
	for p, s := range w.set {
		cells[p] = s
	}
	w.mu.Unlock()
	for p, s := range cells {
		fn(p[0], p[1], p[2], s)
	}
}

// At is the block's state at x, y, z.
func (w *World) At(x, y, z int) block.StateID {
	w.mu.Lock()
	defer w.mu.Unlock()
	if s, ok := w.set[[3]int{x, y, z}]; ok {
		return s
	}
	return w.ground(y)
}

// Chunk is the chunk at cx, cz as its packet: the overworld's 24 sections
// (y −64 to 319), no light (the bot reads none), no block entities.
func (w *World) Chunk(cx, cz int) play.LevelChunkWithLight { return w.chunk(cx, cz) }

func (w *World) chunk(cx, cz int) play.LevelChunkWithLight {
	// the blocks set by hand in this chunk, once, by section: the rest is the ground's layers
	w.mu.Lock()
	var over [24][][4]int // x, y, z (in the chunk), state
	for p, s := range w.set {
		if p[0]>>4 != cx || p[2]>>4 != cz || p[1] < -64 || p[1] >= 320 {
			continue
		}
		i := (p[1] + 64) >> 4
		over[i] = append(over[i], [4]int{p[0] & 15, p[1], p[2] & 15, int(s)})
	}
	w.mu.Unlock()
	c := level.EmptyChunk(24)
	for i := range c.Sections {
		sec := &c.Sections[i]
		for dy := 0; dy < 16; dy++ {
			y := -64 + i*16 + dy
			s := w.ground(y)
			if s == 0 {
				continue
			}
			for k := 0; k < 256; k++ {
				sec.SetBlock(dy<<8|k, level.BlocksState(s))
			}
		}
		for _, o := range over[i] { // (a set block over the ground, air too)
			sec.SetBlock((o[1]+64-i*16)<<8|o[2]<<4|o[0], level.BlocksState(block.StateID(o[3])))
		}
	}
	var wire []level.LevelChunkSection
	for i := range c.Sections {
		wire = append(wire, c.Sections[i].ToWire())
	}
	data := types.LevelChunkPacketData{}
	data.Buffer.V = wire
	return play.LevelChunkWithLight{X: pk.Int(cx), Z: pk.Int(cz), ChunkData: data}
}
