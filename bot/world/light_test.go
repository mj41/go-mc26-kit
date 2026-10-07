package world

import (
	"reflect"
	"testing"

	"github.com/mj41/go-mc26/level"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/types"
)

func TestLightAt(t *testing.T) {
	// a column of 4 sections from y -64: light sections -80..-1 … 0..15 (6)
	w := &World{light: map[level.ChunkPos]*columnLight{}, minY: -64}
	l := newColumnLight(4)
	w.light[level.ChunkPos{0, 0}] = l
	sec := make([]byte, 2048)
	// block (3, -60, 5): section index 1, nibble (4<<8 | 5<<4 | 3), odd: high half
	i := 4<<8 | 5<<4 | 3
	sec[i>>1] = 0x9 << 4
	d := &types.LightUpdatePacketData{SkyUpdates: []pk.ByteArray{sec}, BlockUpdates: []pk.ByteArray{sec}}
	dv := reflect.ValueOf(d).Elem()
	setBits(dv.FieldByName("SkyYMask"), 1)
	setBits(dv.FieldByName("EmptySkyYMask"), 2)
	setBits(dv.FieldByName("BlockYMask"), 1)
	l.put(d)
	if sky, block, ok := w.LightAt(BlockPos{3, -60, 5}); !ok || sky != 9 || block != 9 {
		t.Errorf("at the lit block: %d %d %v, want 9 9 true", sky, block, ok)
	}
	if sky, _, ok := w.LightAt(BlockPos{4, -60, 5}); !ok || sky != 0 {
		t.Errorf("beside it: %d %v, want 0 true", sky, ok)
	}
	if sky, _, ok := w.LightAt(BlockPos{3, -40, 5}); !ok || sky != 0 {
		t.Errorf("in the empty section: %d %v, want 0 true", sky, ok)
	}
	if sky, _, ok := w.LightAt(BlockPos{3, 0, 5}); !ok || sky != 15 {
		t.Errorf("over all sent: %d %v, want 15 true", sky, ok)
	}
	if _, _, ok := w.LightAt(BlockPos{3, -70, 5}); ok {
		t.Error("under the lowest section sent: known")
	}
	if _, _, ok := w.LightAt(BlockPos{20, -60, 5}); ok {
		t.Error("another chunk: known")
	}
}

// setBits sets the bits of a light mask field as its library version has
// it: a set of longs before 26.3, of bytes from it.
func setBits(f reflect.Value, bits ...int) {
	v := reflect.MakeSlice(f.Type(), 1, 1)
	for _, b := range bits {
		switch e := v.Index(0); e.Kind() {
		case reflect.Int64:
			e.SetInt(e.Int() | 1<<b)
		case reflect.Uint64, reflect.Uint8:
			e.SetUint(e.Uint() | 1<<b)
		}
	}
	f.Set(v)
}
