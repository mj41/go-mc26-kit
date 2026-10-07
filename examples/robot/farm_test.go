package main

import (
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// TestFieldLayout: a pool two by two at the west end, two channels of ten
// (twenty water), the rows across the field seed, water, seed, seed, water,
// seed (forty farmland, each next to water), all of it shut in; the pool
// where the eight-long field of before had it.
func TestFieldLayout(t *testing.T) {
	c := world.BlockPos{X: 100, Y: 64, Z: -20}
	pool, water, soil := fieldPool(c), fieldWater(c), fieldSoil(c)
	if len(pool) != 4 || len(water) != 20 || len(soil) != 40 || len(fieldArea(c)) != 70 {
		t.Fatalf("pool %d, water %d, soil %d", len(pool), len(water), len(soil))
	}
	if pool[0] != (world.BlockPos{X: c.X - 6, Y: c.Y, Z: c.Z}) {
		t.Errorf("the pool moved: %v", pool[0])
	}
	// across the field, z from c-1 to c+4: seed, water, seed, seed, water, seed
	row := map[int]string{}
	seen := map[world.BlockPos]bool{}
	for _, w := range water {
		if w.Y != c.Y {
			t.Errorf("water %v off the level", w)
		}
		row[w.Z-c.Z] += "w"
		seen[w] = true
	}
	for _, s := range soil {
		if seen[s] {
			t.Errorf("%v both water and soil", s)
		}
		row[s.Z-c.Z] += "s"
		seen[s] = true
	}
	for dz, want := range map[int]byte{-1: 's', 0: 'w', 1: 's', 2: 's', 3: 'w', 4: 's'} {
		if len(row[dz]) != 10 || strings.Count(row[dz], string(want)) != 10 {
			t.Errorf("row %+d: %q, want ten %c", dz, row[dz], want)
		}
	}
	// every plant next to water (side by side)
	wetCell := map[world.BlockPos]bool{}
	for _, w := range water {
		wetCell[w] = true
	}
	for _, s := range soil {
		if !wetCell[world.BlockPos{X: s.X, Y: s.Y, Z: s.Z + 1}] && !wetCell[world.BlockPos{X: s.X, Y: s.Y, Z: s.Z - 1}] {
			t.Errorf("soil %v not next to water", s)
		}
	}
	// the pool: the first channel's west end touches it; its first two are opposite corners
	if a, b := pool[0], pool[1]; abs(a.X-b.X) != 1 || abs(a.Z-b.Z) != 1 {
		t.Errorf("pool corners %v %v not opposite", a, b)
	}
	touches := false
	for _, p := range pool {
		if seen[p] {
			t.Errorf("%v both pool and field", p)
		}
		if p.Z == water[0].Z && p.X == water[0].X-1 {
			touches = true
		}
		seen[p] = true
	}
	if !touches {
		t.Errorf("the channel's end %v does not touch the pool %v", water[0], pool)
	}
	if soil[0].Z != c.Z+1 { // the rows between the channels first
		t.Errorf("first soil %v", soil[0])
	}
	// every water cell's sides and floor are field or shut: no way out
	shut := map[world.BlockPos]bool{}
	for _, b := range fieldShut(c) {
		shut[b] = true
	}
	wet := map[world.BlockPos]bool{}
	for _, p := range append(pool, water...) {
		wet[p] = true
	}
	for p := range wet {
		for _, n := range []world.BlockPos{{X: p.X + 1, Y: p.Y, Z: p.Z}, {X: p.X - 1, Y: p.Y, Z: p.Z}, {X: p.X, Y: p.Y, Z: p.Z + 1}, {X: p.X, Y: p.Y, Z: p.Z - 1}, {X: p.X, Y: p.Y - 1, Z: p.Z}} {
			if !wet[n] && !shut[n] {
				t.Errorf("water at %v can run out to %v", p, n)
			}
		}
	}
	// the headland across the east end: next to every row's last cell, so
	// the rows between the channels have a way round to the others on foot
	head := map[world.BlockPos]bool{}
	for _, h := range fieldHeadland(c) {
		if seen[h] {
			t.Errorf("headland %v is also the field's", h)
		}
		head[h] = true
	}
	for _, dz := range []int{-1, 1, 2, 4} {
		if !head[world.BlockPos{X: c.X + 6, Y: c.Y, Z: c.Z + dz}] {
			t.Errorf("row %+d has no headland at its end", dz)
		}
	}
	// the ground prepared first holds every cell and every wall at the level
	ground := map[world.BlockPos]bool{}
	for _, g := range fieldGround(c) {
		ground[g] = true
	}
	for _, p := range append(fieldArea(c), fieldShut(c)...) {
		if p.Y == c.Y && !ground[p] {
			t.Errorf("%v is not on the ground prepared", p)
		}
	}
	// the box its own works keep out of holds all of it
	b := fieldBox(c, 0)
	for _, p := range append(append(fieldArea(c), fieldShut(c)...), fieldGround(c)...) {
		if p.X < b[0].X || p.X > b[1].X || p.Z < b[0].Z || p.Z > b[1].Z || p.Y < b[0].Y || p.Y > b[1].Y {
			t.Errorf("%v outside the field's box %v", p, *b)
		}
	}
}

// TestVegetation: trees are cut as a plot is prepared (no sign of rough
// ground); earth and stone are terrain.
func TestVegetation(t *testing.T) {
	for name, want := range map[string]bool{"minecraft:oak_log": true, "minecraft:birch_leaves": true, "minecraft:spruce_log": true,
		"minecraft:stone": false, "minecraft:dirt": false, "minecraft:grass_block": false} {
		if vegetation(name) != want {
			t.Errorf("vegetation(%s) = %v, want %v", name, vegetation(name), want)
		}
	}
}
