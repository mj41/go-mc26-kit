package main

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level"
	"github.com/mj41/go-mc26/level/block"
)

// stateOf finds a state by the name the game writes it with.
func stateOf(t *testing.T, name string) block.StateID {
	for i := range block.StateList {
		if world.StateString(block.StateID(i)) == name {
			return block.StateID(i)
		}
	}
	t.Fatalf("no state %s", name)
	return 0
}

// a little land: stone, dirt, grass, a pond, a tree, a tunnel and stairs,
// one chunk column of the box left unloaded
func testWorld(t *testing.T) *world.World {
	w := &world.World{Columns: map[level.ChunkPos]*level.Chunk{}}
	for cx := -3; cx <= 2; cx++ {
		for cz := -3; cz <= 2; cz++ {
			if cx == 1 && cz == 1 {
				continue
			}
			w.Columns[level.ChunkPos{int32(cx), int32(cz)}] = level.EmptyChunk(24)
		}
	}
	stone, dirt, grass := stateOf(t, "minecraft:stone"), stateOf(t, "minecraft:dirt"), stateOf(t, "minecraft:grass_block[snowy=false]")
	water, log, leaves := stateOf(t, "minecraft:water[level=0]"), stateOf(t, "minecraft:oak_log[axis=y]"), stateOf(t, "minecraft:oak_leaves[distance=1,persistent=false,waterlogged=false]")
	stairs := stateOf(t, "minecraft:cobblestone_stairs[facing=east,half=bottom,shape=straight,waterlogged=false]")
	torch := stateOf(t, "minecraft:torch")
	for x := -40; x < 40; x++ {
		for z := -40; z < 40; z++ {
			top := 64 + (x*x+z*z)/300
			for y := 30; y <= top; y++ {
				s := stone
				if y > top-3 {
					s = dirt
				}
				if y == top {
					s = grass
				}
				w.SetBlock(world.BlockPos{X: x, Y: y, Z: z}, s)
			}
			if x > 6 && x < 12 && z > -4 && z < 3 {
				w.SetBlock(world.BlockPos{X: x, Y: top, Z: z}, water)
			}
		}
	}
	for y := 65; y < 70; y++ {
		w.SetBlock(world.BlockPos{X: -5, Y: y, Z: 4}, log)
	}
	for x := -7; x <= -3; x++ {
		for z := 2; z <= 6; z++ {
			for y := 69; y <= 71; y++ {
				if x != -5 || z != 4 || y > 69 {
					w.SetBlock(world.BlockPos{X: x, Y: y, Z: z}, leaves)
				}
			}
		}
	}
	for k := range 10 { // a tunnel down east, its stairs, a torch
		for y := 64 - k; y <= 66-k; y++ {
			w.SetBlock(world.BlockPos{X: k, Y: y, Z: 0}, 0)
		}
		w.SetBlock(world.BlockPos{X: k, Y: 63 - k, Z: 0}, stairs)
	}
	w.SetBlock(world.BlockPos{X: 4, Y: 61, Z: 1}, 0)
	w.SetBlock(world.BlockPos{X: 4, Y: 60, Z: 1}, stone)
	w.SetBlock(world.BlockPos{X: 4, Y: 61, Z: 1}, torch)
	return w
}

// The blocks event says every block of its box: decoded, each cell is the
// world's state there, or not loaded; told again only when changed.
func TestBlocksEvent(t *testing.T) {
	w := testWorld(t)
	r := &robot{world: w}
	x, y, z := 0, 65, 0
	ev, last := r.blocksEvent(x, y, z, nil)
	if ev == nil {
		t.Fatal("no event")
	}
	b, _ := json.Marshal(ev)
	t.Logf("blocks: %d bytes of JSON, %d palette entries", len(b), len(ev["palette"].([]paletteEntry)))
	if out := os.Getenv("ROBOT_BLOCKS_OUT"); out != "" { // a sample for a reader to draw
		line, _ := json.Marshal(map[string]any{"id": "0", "ts": "2026-10-05T12:00:00+02:00", "source": "mc:Test", "kind": "telemetry", "name": "blocks", "data": ev})
		os.WriteFile(out, append(line, '\n'), 0o644)
	}
	packed, _ := base64.StdEncoding.DecodeString(ev["cells"].(string))
	cells, err := io.ReadAll(flate.NewReader(bytes.NewReader(packed)))
	if err != nil {
		t.Fatal(err)
	}
	pal := ev["palette"].([]paletteEntry)
	wd, h := ev["w"].(int), ev["h"].(int)
	if len(cells) != 2*wd*wd*h {
		t.Fatalf("%d cells, want %d", len(cells)/2, wd*wd*h)
	}
	x0, y0, z0 := ev["x0"].(int), ev["y0"].(int), ev["z0"].(int)
	unloaded := 0
	for i := 0; i < len(cells); i += 2 {
		c := int(binary.LittleEndian.Uint16(cells[i:]))
		n := i / 2
		pos := world.BlockPos{X: x0 + n%wd, Y: y0 + n/(wd*wd), Z: z0 + n/wd%wd}
		s, ok := w.BlockAt(pos)
		switch {
		case !ok && c != notLoaded:
			t.Fatalf("%v: not loaded, cell %d", pos, c)
		case !ok:
			unloaded++
		case pal[c].Name != world.StateString(s):
			t.Fatalf("%v: %s, cell says %s", pos, world.StateString(s), pal[c].Name)
		}
	}
	if unloaded == 0 {
		t.Error("the unloaded column not told")
	}
	for _, p := range pal {
		switch p.Name {
		case "minecraft:air":
			if p.Cube || len(p.Boxes) != 0 || p.C != 0 {
				t.Errorf("air: %+v, want nothing", p)
			}
		case "minecraft:stone":
			if !p.Cube || p.Boxes != nil || p.C == 0 {
				t.Errorf("stone: %+v, want a full cube with a colour", p)
			}
		case "minecraft:water[level=0]":
			if p.Fluid == nil || p.Cube || len(p.Boxes) != 0 {
				t.Errorf("water: %+v, want a fluid and no boxes", p)
			}
		case "minecraft:cobblestone_stairs[facing=east,half=bottom,shape=straight,waterlogged=false]":
			if p.Cube || len(p.Boxes) != 2 {
				t.Errorf("stairs: %+v, want two boxes", p)
			}
		}
	}
	if again, _ := r.blocksEvent(x, y, z, last); again != nil {
		t.Error("told again unchanged")
	}
	w.SetBlock(world.BlockPos{X: 1, Y: 60, Z: 1}, 0)
	if again, _ := r.blocksEvent(x, y, z, last); again == nil {
		t.Error("a block dug, not told")
	}
}

// The blocks event's biomes: the quarts covering the box, a byte each — not
// known (255) where nothing names them (a world with no server's registry).
func TestBlocksEventBiomes(t *testing.T) {
	r := &robot{world: testWorld(t)}
	ev, _ := r.blocksEvent(-3, 65, 2, nil)
	b := ev["biomes"].(map[string]any)
	x0, y0, z0 := ev["x0"].(int), ev["y0"].(int), ev["z0"].(int)
	if b["qx0"] != x0>>2 || b["qy0"] != y0>>2 || b["qz0"] != z0>>2 {
		t.Errorf("corner %v %v %v for the box at %d %d %d", b["qx0"], b["qy0"], b["qz0"], x0, y0, z0)
	}
	qw, qh, qd := b["qw"].(int), b["qh"].(int), b["qd"].(int)
	if qw != 13 || qh != 9 || qd != 13 { // 49 blocks across 13 quarts always; y0 49 to 80 across 9
		t.Errorf("quarts %d×%d×%d", qw, qh, qd)
	}
	packed, _ := base64.StdEncoding.DecodeString(b["cells"].(string))
	cells, err := io.ReadAll(flate.NewReader(bytes.NewReader(packed)))
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != qw*qh*qd {
		t.Fatalf("%d cells, want %d", len(cells), qw*qh*qd)
	}
	for i, c := range cells {
		if c != 255 {
			t.Fatalf("cell %d: %d, want 255 (no registry)", i, c)
		}
	}
}
