package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// TestMemory: what a robot knows comes back whole to one started again.
func TestMemory(t *testing.T) {
	a := &robot{}
	a.home = &room{front: world.BlockPos{X: 1, Y: 2, Z: 3}, d: [2]int{-1, 0}, y: 2}
	a.deep = &spot{at: world.BlockPos{X: 4, Y: -5, Z: 6}, d: [2]int{0, 1}}
	a.addStair(world.BlockPos{X: 1, Y: 2, Z: 3})
	a.addStair(world.BlockPos{X: 0, Y: 1, Z: 3})
	a.field = &world.BlockPos{X: 7, Y: 8, Z: 9}
	a.search = searchState{on: true, center: world.BlockPos{X: 1, Y: 70, Z: 2}, ring: 2, point: 5}
	a.atlas.put(keyOf(-1, 40), area{Logs: 12, Water: 3, Y: 70, SeenAt: 1})
	b, err := json.Marshal(a.recall())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "memory.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &robot{}
	if err := c.loadMemory(path); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.recall(), c.recall()) || !c.onStairs[world.BlockPos{X: 0, Y: 1, Z: 3}] {
		t.Errorf("remembered %+v, want %+v", c.recall(), a.recall())
	}
	if err := (&robot{}).loadMemory(filepath.Join(t.TempDir(), "none.json")); err != nil {
		t.Errorf("no memory yet: %v", err)
	}
}

// TestAreaKey: an area is sixteen by sixteen, west and north of zero too.
func TestAreaKey(t *testing.T) {
	for _, c := range []struct{ x, z, kx, kz int }{{0, 0, 0, 0}, {15, 16, 0, 1}, {-1, -16, -1, -1}, {-17, 40, -2, 2}} {
		if k := keyOf(c.x, c.z); k != (areaKey{c.kx, c.kz}) {
			t.Errorf("keyOf(%d, %d) = %v", c.x, c.z, k)
		}
	}
	if x, z := (areaKey{-1, 2}).centre(); x != -8 || z != 40 {
		t.Errorf("centre = %d %d", x, z)
	}
}
