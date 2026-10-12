package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mj41/go-mc26/data/version"
	"github.com/mj41/go-mc26/nbt"
	"github.com/mj41/go-mc26/save"
	"github.com/mj41/go-mc26/save/region"
)

// writeRegion writes r.1.2.mca into dir with one chunk the save package makes, chunk (33, 66), and returns
// the sector as stored.
func writeRegion(t *testing.T, dir string) []byte {
	t.Helper()
	c := save.Chunk{DataVersion: version.DataVersion, XPos: 33, ZPos: 66, Status: "minecraft:full"}
	data, err := c.Data(2) // zlib, as the game writes
	if err != nil {
		t.Fatal(err)
	}
	r, err := region.Create(filepath.Join(dir, "r.1.2.mca"))
	if err != nil {
		t.Fatal(err)
	}
	x, z := region.In(33, 66)
	if err := r.WriteSector(x, z, data); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return data
}

// A region unpacked and packed again is the region it was: the chunk's sector, byte for byte.
func TestMcadumpRoundTrip(t *testing.T) {
	src, chunks, back := t.TempDir(), t.TempDir(), t.TempDir()
	stored := writeRegion(t, src)

	if err := run([]string{filepath.Join(src, "r.*.mca")}, chunks, false, false); err != nil {
		t.Fatal(err)
	}
	mcc, err := os.ReadFile(filepath.Join(chunks, "c.33.66.mcc"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mcc, stored) {
		t.Fatalf("c.33.66.mcc is %d bytes, not the %d stored", len(mcc), len(stored))
	}

	if err := run([]string{filepath.Join(chunks, "c.*.mcc")}, back, false, true); err != nil {
		t.Fatal(err)
	}
	r, err := region.OpenReadOnly(filepath.Join(back, "r.1.2.mca"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	x, z := region.In(33, 66)
	got, err := r.ReadSector(x, z)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, stored) {
		t.Fatalf("the repacked sector is %d bytes, not the %d stored", len(got), len(stored))
	}
}

// -x writes each chunk's NBT: the chunk the save package wrote.
func TestMcadumpDecompress(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	writeRegion(t, src)
	if err := run([]string{filepath.Join(src, "r.1.2.mca")}, out, true, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "c.33.66.mcc.nbt"))
	if err != nil {
		t.Fatal(err)
	}
	var c save.Chunk
	if err := nbt.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if c.XPos != 33 || c.ZPos != 66 || c.Status != "minecraft:full" {
		t.Fatalf("the NBT is chunk (%d, %d) %q, want (33, 66) minecraft:full", c.XPos, c.ZPos, c.Status)
	}
}
