package world

import (
	"fmt"
	"sync"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/level"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

type World struct {
	c      *bot.Client
	p      *basic.Player
	events EventsListener

	// mu guards Columns: the packet handlers write it, a controller's tick
	// goroutine reads it. Code outside the package that touches Columns
	// directly holds it too ([World.Lock], [World.RLock]).
	mu      sync.RWMutex
	Columns map[level.ChunkPos]*level.Chunk
	light   map[level.ChunkPos]*columnLight // the columns' light (light.go)
	minY    int                             // the lowest block y of the dimension the player is in
}

// RLock and RUnlock hold the world for reading; Lock and Unlock for writing.
func (w *World) RLock()   { w.mu.RLock() }
func (w *World) RUnlock() { w.mu.RUnlock() }
func (w *World) Lock()    { w.mu.Lock() }
func (w *World) Unlock()  { w.mu.Unlock() }

// HasChunk reports whether the chunk column at pos is loaded.
func (w *World) HasChunk(pos level.ChunkPos) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.Columns[pos]
	return ok
}

func NewWorld(c *bot.Client, p *basic.Player, events EventsListener) (w *World) {
	w = &World{
		c: c, p: p,
		events:  events,
		Columns: make(map[level.ChunkPos]*level.Chunk),
		light:   make(map[level.ChunkPos]*columnLight),
	}
	c.Events.AddListener(
		bot.PacketHandler{Priority: 64, ID: packetid.ClientboundPlayLogin, F: w.onPlayerSpawn},
		bot.PacketHandler{Priority: 64, ID: packetid.ClientboundPlayRespawn, F: w.onPlayerSpawn},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayLevelChunkWithLight, F: w.handleLevelChunkWithLightPacket},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayForgetLevelChunk, F: w.handleForgetLevelChunkPacket},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayLightUpdate, F: w.handleLightUpdatePacket},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayChunkBatchFinished, F: w.handleChunkBatchFinishedPacket},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayBlockUpdate, F: w.handleBlockUpdate},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlaySectionBlocksUpdate, F: w.handleSectionBlocksUpdate},
	)
	return
}

// DefaultChunksPerTick is the chunk rate the client asks for after every chunk
// batch. Vanilla servers throttle chunk sending until the client acknowledges a
// batch, and each acknowledgement carries the rate the client is willing to
// receive; the vanilla client derives it from its own timing, a bot has no such
// limit.
const DefaultChunksPerTick = 64

// handleChunkBatchFinishedPacket answers ClientboundChunkBatchFinished with
// ServerboundChunkBatchReceived. Without the reply a vanilla server sends only
// the first batch of chunks (protocol 764+).
func (w *World) handleChunkBatchFinishedPacket(packet pk.Packet) error {
	var finished play.ChunkBatchFinished
	if err := packet.Scan(&finished); err != nil {
		return err
	}
	ack := play.ChunkBatchReceived{DesiredChunksPerTick: DefaultChunksPerTick}
	return w.c.Conn.WritePacket(pk.Marshal(ack.PacketID(), ack))
}

func (w *World) onPlayerSpawn(pk.Packet) error {
	// unload all chunks
	w.mu.Lock()
	w.Columns = make(map[level.ChunkPos]*level.Chunk)
	w.light = make(map[level.ChunkPos]*columnLight)
	w.mu.Unlock()
	return nil
}

func (w *World) handleLevelChunkWithLightPacket(packet pk.Packet) error {
	currentDimType := w.c.Registries.DimensionType.GetByID(int32(w.p.Spawn.DimensionType))
	if currentDimType == nil {
		return fmt.Errorf("dimension type %d not found", w.p.Spawn.DimensionType)
	}
	var p play.LevelChunkWithLight
	if err := packet.Scan(&p); err != nil {
		return err
	}
	// The section bytes are decoded with the dimension's height; the light
	// arrays are kept beside the column (LightAt).
	chunk := level.EmptyChunk(int(currentDimType.Height) / 16)
	minY := int(currentDimType.MinY)
	heightmaps := make(map[int32][]uint64, len(p.ChunkData.Heightmaps))
	for _, e := range p.ChunkData.Heightmaps {
		longs := make([]uint64, len(e.Val))
		for i, v := range e.Val {
			longs[i] = uint64(v)
		}
		heightmaps[int32(e.Key)] = longs
	}
	chunk.SetHeightmapData(heightmaps)
	if err := chunk.PutSections(p.ChunkData.Buffer.V); err != nil {
		return err
	}
	chunk.BlockEntity = []level.BlockEntity(p.ChunkData.BlockEntitiesData)
	pos := level.ChunkPos{int32(p.X), int32(p.Z)}
	w.mu.Lock()
	w.Columns[pos] = chunk
	l := newColumnLight(len(chunk.Sections))
	l.put(&p.LightData)
	w.light[pos] = l
	w.minY = minY
	w.mu.Unlock()
	if w.events.LoadChunk != nil {
		if err := w.events.LoadChunk(pos); err != nil {
			return err
		}
	}
	return nil
}

func (w *World) handleForgetLevelChunkPacket(packet pk.Packet) error {
	var forget play.ForgetLevelChunk
	if err := packet.Scan(&forget); err != nil {
		return err
	}
	pos := level.ChunkPos(forget.Pos)
	var err error
	if w.events.UnloadChunk != nil {
		err = w.events.UnloadChunk(pos)
	}
	w.mu.Lock()
	delete(w.Columns, pos)
	delete(w.light, pos)
	w.mu.Unlock()
	return err
}
