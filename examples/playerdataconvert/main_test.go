package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/offline"
	"github.com/mj41/go-mc26/data/version"
	"github.com/mj41/go-mc26/nbt"
	"github.com/mj41/go-mc26/save"
	"github.com/mj41/go-mc26/save/region"
)

// entityChunk is an entity region's chunk (save.EntityStorage) with its entities typed: here, wolves.
type entityChunk struct {
	DataVersion int32       `nbt:"DataVersion"`
	Entities    []save.Wolf `nbt:"Entities"`
	Position    []int32     `nbt:"Position"`
}

// ints is a uuid as the save files keep it: four big-endian ints.
func ints(id uuid.UUID) []int32 {
	out := make([]int32, 4)
	for i := range out {
		out[i] = int32(binary.BigEndian.Uint32(id[i*4:]))
	}
	return out
}

// A save of an offline server, written with the save package — Steve's player file, his tamed wolf, his
// advancements, all under his offline uuid — converted: each now his online uuid (the lookup Mojang's API
// would answer given by the test).
func TestConvert(t *testing.T) {
	dir := t.TempDir()
	offlineID, onlineID := offline.NameToUUID("Steve"), uuid.MustParse("a3f1c0de-0000-4000-8000-000000000001")
	lookup = func(name string) (string, uuid.UUID, error) { return name, onlineID, nil }
	t.Cleanup(func() { lookup = usernameToUUID })

	cache, _ := json.Marshal([]UserCache{{Name: "Steve", UUID: offlineID}})
	write(t, filepath.Join(dir, "usercache.json"), cache)

	var player bytes.Buffer
	gz := gzip.NewWriter(&player)
	if err := nbt.NewEncoder(gz).Encode(save.PlayerData{DataVersion: version.DataVersion, UUID: ints(offlineID)}, ""); err != nil {
		t.Fatal(err)
	}
	gz.Close()
	write(t, filepath.Join(dir, "world", "playerdata", offlineID.String()+".dat"), player.Bytes())

	wolves := entityChunk{DataVersion: version.DataVersion, Position: []int32{0, 0},
		Entities: []save.Wolf{{ID: "minecraft:wolf", Owner: ints(offlineID)}}}
	var sector bytes.Buffer
	sector.WriteByte(2) // zlib, as the game writes
	zw := zlib.NewWriter(&sector)
	if err := nbt.NewEncoder(zw).Encode(wolves, ""); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	if err := os.MkdirAll(filepath.Join(dir, "world", "entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := region.Create(filepath.Join(dir, "world", "entities", "r.0.0.mca"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.WriteSector(0, 0, sector.Bytes()); err != nil {
		t.Fatal(err)
	}
	r.Close()

	advancements := []byte(`{"minecraft:story/root":{"done":true},"DataVersion":1}`)
	write(t, filepath.Join(dir, "world", "advancements", offlineID.String()+".json"), advancements)

	if err := run(dir, true, true, true); err != nil {
		t.Fatal(err)
	}

	// the player file under the online uuid, with it inside
	f, err := os.Open(filepath.Join(dir, "world", "playerdata", onlineID.String()+".dat"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	p, err := save.ReadPlayerData(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.UUID, ints(onlineID)) {
		t.Errorf("the new player file's UUID is %v, want %v", p.UUID, ints(onlineID))
	}

	// the wolf's owner
	r, err = region.OpenReadOnly(filepath.Join(dir, "world", "entities", "r.0.0.mca"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, err := r.ReadSector(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	body, err := decompressed(data)
	if err != nil {
		t.Fatal(err)
	}
	var got entityChunk
	if err := nbt.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entities) != 1 {
		t.Fatalf("%d entities in the chunk, want the wolf", len(got.Entities))
	}
	if owner := got.Entities[0].Owner; !slices.Equal(owner, ints(onlineID)) {
		t.Errorf("the wolf's owner is %v, want %v", owner, ints(onlineID))
	}

	// the advancements under the online uuid
	if adv, err := os.ReadFile(filepath.Join(dir, "world", "advancements", onlineID.String()+".json")); err != nil || !bytes.Equal(adv, advancements) {
		t.Errorf("the new advancements file: %q, %v", adv, err)
	}
}

// decompressed is a stored sector's NBT (the first byte its compression).
func decompressed(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	rest := bytes.NewReader(data[1:])
	switch data[0] {
	case 1:
		zr, err := gzip.NewReader(rest)
		if err != nil {
			return nil, err
		}
		_, err = buf.ReadFrom(zr)
		return buf.Bytes(), err
	case 2:
		zr, err := zlib.NewReader(rest)
		if err != nil {
			return nil, err
		}
		_, err = buf.ReadFrom(zr)
		return buf.Bytes(), err
	}
	return data[1:], nil
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
