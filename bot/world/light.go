package world

import (
	"github.com/mj41/go-mc26/level"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
	"reflect"
)

// columnLight is a chunk column's light as the server sends it: one 2048-byte
// array of nibbles per section, for the sky and for blocks, from the section
// under the dimension's lowest to the one over its highest (two more than the
// column's sections). A nil array is a section the server sent nothing for.
type columnLight struct {
	sky, block [][]byte
}

// putLight applies a chunk's or a light update's light data: a section in a
// mask gets the next array, one in an empty mask gets zeros, the rest keep
// what they had.
func (l *columnLight) put(d *types.LightUpdatePacketData) {
	apply := func(layers [][]byte, maskSet, emptySet any, arrays []pk.ByteArray) {
		mask, empty := bitsOf(maskSet), bitsOf(emptySet)
		next := 0
		for i := range layers {
			switch {
			case mask(i):
				if next < len(arrays) && len(arrays[next]) == 2048 {
					layers[i] = append([]byte(nil), arrays[next]...)
				}
				next++
			case empty(i):
				layers[i] = make([]byte, 2048)
			}
		}
	}
	apply(l.sky, d.SkyYMask, d.EmptySkyYMask, d.SkyUpdates)
	apply(l.block, d.BlockYMask, d.EmptyBlockYMask, d.BlockUpdates)
}

// bitsOf reads a light mask as each library version has it — a bit set of
// longs (Get) before 26.3, of bytes (Has) from 26.3 — so this one source
// builds against every supported version.
func bitsOf(set any) func(int) bool {
	switch b := set.(type) {
	case interface{ Has(int) bool }:
		return b.Has
	case interface{ Get(int) bool }:
		// a set of longs: Get panics past its end, a set shorter than the
		// sections is usual (the high ones unset)
		n := 64 * reflect.ValueOf(set).Len()
		return func(i int) bool { return i >= 0 && i < n && b.Get(i) }
	}
	return func(int) bool { return false }
}

func newColumnLight(sections int) *columnLight {
	return &columnLight{sky: make([][]byte, sections+2), block: make([][]byte, sections+2)}
}

func nibble(a []byte, pos BlockPos) int {
	i := (pos.Y&15)<<8 | (pos.Z&15)<<4 | pos.X&15
	return int(a[i>>1]>>(4*(i&1))) & 15
}

// LightAt returns the sky and block light (0–15) at pos, as the server last
// sent them; false when its chunk or its section's light is not known. Over
// every section the sky light was sent for, the sky is open: 15.
func (w *World) LightAt(pos BlockPos) (sky, block int, ok bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	l, found := w.light[pos.Chunk()]
	if !found {
		return 0, 0, false
	}
	i := (pos.Y-w.minY)>>4 + 1 // the section under the lowest is 0
	if i < 0 || i >= len(l.sky) {
		return 0, 0, false
	}
	switch {
	case l.sky[i] != nil:
		sky = nibble(l.sky[i], pos)
	case func() bool { // nothing sent here or anywhere over it
		for j := i; j < len(l.sky); j++ {
			if l.sky[j] != nil {
				return false
			}
		}
		return true
	}():
		sky = 15
	default:
		return 0, 0, false
	}
	if l.block[i] != nil {
		block = nibble(l.block[i], pos)
	}
	return sky, block, true
}

// BiomeAt returns the biome at pos (its id, minecraft:plains), from the
// chunk's biomes and the server's biome registry; false when not known.
func (w *World) BiomeAt(pos BlockPos) (string, bool) {
	w.mu.RLock()
	sec, _, ok := w.section(pos)
	var id int32
	if ok && sec.Biomes != nil {
		// biomes are kept per 4×4×4 cell of the section
		id = int32(sec.Biomes.Get((pos.Y&15)>>2<<4 | (pos.Z&15)>>2<<2 | (pos.X&15)>>2))
	}
	w.mu.RUnlock()
	if !ok || sec.Biomes == nil || w.c == nil {
		return "", false // no client: no registry to name it by (a world made in a test)
	}
	return w.c.Registries.Biome.KeyOf(id)
}

// TemperatureAt returns the temperature at pos as the game works it out
// (Biome.getHeightAdjustedTemperature): its biome's, less a little for each
// block over y 80, the mountains' cold (the game's noise of ±0.01 aside).
// Under 0.15 water open to the sky freezes and snow falls; false when not
// known.
func (w *World) TemperatureAt(pos BlockPos) (float32, bool) {
	w.mu.RLock()
	sec, _, ok := w.section(pos)
	var id int32
	if ok && sec.Biomes != nil {
		id = int32(sec.Biomes.Get((pos.Y&15)>>2<<4 | (pos.Z&15)>>2<<2 | (pos.X&15)>>2))
	}
	w.mu.RUnlock()
	if !ok || sec.Biomes == nil || w.c == nil {
		return 0, false
	}
	b := w.c.Registries.Biome.GetByID(id)
	if b == nil {
		return 0, false
	}
	t := b.Temperature
	if pos.Y > 80 {
		t -= float32(pos.Y-80) * 0.05 / 40
	}
	return t, true
}

func (w *World) handleLightUpdatePacket(packet pk.Packet) error {
	var p play.LightUpdate
	if err := packet.Scan(&p); err != nil {
		return err
	}
	pos := level.ChunkPos{int32(p.X), int32(p.Z)}
	w.mu.Lock()
	defer w.mu.Unlock()
	l, ok := w.light[pos]
	if !ok {
		c, loaded := w.Columns[pos]
		if !loaded {
			return nil // light for a chunk not (yet) here: the chunk packet brings its own
		}
		l = newColumnLight(len(c.Sections))
		w.light[pos] = l
	}
	l.put(&p.LightData)
	return nil
}
