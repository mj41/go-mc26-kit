package main

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// The blocks round the robot, every block state of a box about it, for a
// reader to build the world in 3D from (a viewer draws it from the robot's
// eye): a palette of the states in it — each named with its
// properties, with its map colour and the boxes a cursor hits (its outline
// shape), and its fluid — and a cell per block, the index of its state in the
// palette. EVENTS.md, "blocks", is the format.

// The box: blocksSize each way along x and z, blocksDown under its feet to
// blocksUp over them.
const blocksSize, blocksDown, blocksUp = 24, 16, 15

// notLoaded is the cell of a block not known (its chunk not loaded).
const notLoaded = 0xFFFF

// paletteEntry is a block state as a reader is told it.
type paletteEntry struct {
	Name  string       `json:"name"`
	C     uint32       `json:"c"`
	Cube  bool         `json:"cube,omitempty"`  // a full cube: no boxes
	Boxes [][6]float64 `json:"boxes,omitempty"` // else its boxes, none for air, water
	Fluid *fluidEntry  `json:"fluid,omitempty"`
}

type fluidEntry struct {
	Name   string  `json:"name"`
	Height float32 `json:"height"`
}

// entries holds each state's entry once made: they do not change.
var entries sync.Map // block.StateID → paletteEntry

func entryOf(s block.StateID) paletteEntry {
	if e, ok := entries.Load(s); ok {
		return e.(paletteEntry)
	}
	e := paletteEntry{Name: world.StateString(s), C: block.MapColor(s)}
	shape := block.OutlineShape(s)
	if len(shape) == 1 && shape[0] == (block.AABB{MaxX: 1, MaxY: 1, MaxZ: 1}) {
		e.Cube = true
	} else {
		for _, b := range shape {
			r := func(v float64) float64 { return math.Round(v*1e4) / 1e4 }
			e.Boxes = append(e.Boxes, [6]float64{r(b.MinX), r(b.MinY), r(b.MinZ), r(b.MaxX), r(b.MaxY), r(b.MaxZ)})
		}
	}
	if f := block.FluidOf(s); f != nil {
		e.Fluid = &fluidEntry{Name: f.Name, Height: float32(math.Round(float64(f.Height)*1e4) / 1e4)}
	}
	entries.Store(s, e)
	return e
}

// blocksEvent is the box round the robot's feet x, y, z; last is the cells
// of the event before, nil when unchanged.
func (r *robot) blocksEvent(x, y, z int, last []byte) (map[string]any, []byte) {
	const w, h = 2*blocksSize + 1, blocksDown + blocksUp + 1
	x0, y0, z0 := x-blocksSize, y-blocksDown, z-blocksSize
	index := map[block.StateID]int{}
	var palette []paletteEntry
	cells := make([]byte, 2*w*w*h)
	i := 0
	for dy := range h {
		for dz := range w {
			for dx := range w {
				c := notLoaded
				if s, ok := r.world.BlockAt(world.BlockPos{X: x0 + dx, Y: y0 + dy, Z: z0 + dz}); ok {
					k, seen := index[s]
					if !seen {
						k = len(palette)
						index[s] = k
						palette = append(palette, entryOf(s))
					}
					c = k
				}
				binary.LittleEndian.PutUint16(cells[i:], uint16(c))
				i += 2
			}
		}
	}
	bio, bioCells := r.biomesOf(x0, y0, z0, w, h)
	// the place and the cells the same as last time: nothing new to tell
	head := fmt.Appendf(nil, "%d,%d,%d;", x0, y0, z0)
	for _, p := range palette {
		head = append(head, p.Name...)
		head = append(head, ';')
	}
	for _, b := range bio["palette"].([]string) {
		head = append(head, b...)
		head = append(head, ';')
	}
	now := append(append(head, cells...), bioCells...)
	if bytes.Equal(now, last) {
		return nil, last
	}
	return map[string]any{
		"x0": x0, "y0": y0, "z0": z0, "w": w, "h": h, "d": w,
		"palette": palette, "cells": deflated(cells),
		"biomes": bio,
	}, now
}

// biomesOf are the biomes of the box (corner x0, y0, z0; w wide and deep, h
// high) as the game keeps them, one a 4×4×4 cell ("quart"): the quarts
// covering the box from qx0, qy0, qz0 (floor(x0/4)…), qw×qh×qd of them, each
// a byte of the palette's names (minecraft: left off) at (qy*qd+qz)*qw+qx,
// 255 not known (not loaded). The cells raw too, to see whether they changed.
func (r *robot) biomesOf(x0, y0, z0, w, h int) (map[string]any, []byte) {
	const unknown = 255
	qx0, qy0, qz0 := x0>>2, y0>>2, z0>>2
	qw, qh, qd := (x0+w-1)>>2-qx0+1, (y0+h-1)>>2-qy0+1, (z0+w-1)>>2-qz0+1
	index := map[string]int{}
	palette := []string{}
	cells := make([]byte, qw*qh*qd)
	i := 0
	for qy := range qh {
		for qz := range qd {
			for qx := range qw {
				c := unknown
				if b, ok := r.world.BiomeAt(world.BlockPos{X: (qx0 + qx) * 4, Y: (qy0 + qy) * 4, Z: (qz0 + qz) * 4}); ok {
					b = strings.TrimPrefix(b, "minecraft:")
					k, seen := index[b]
					if !seen && len(palette) < unknown {
						k = len(palette)
						index[b] = k
						palette = append(palette, b)
						seen = true
					}
					if seen {
						c = k
					}
				}
				cells[i] = byte(c)
				i++
			}
		}
	}
	return map[string]any{
		"palette": palette, "qx0": qx0, "qy0": qy0, "qz0": qz0, "qw": qw, "qh": qh, "qd": qd,
		"cells": deflated(cells),
	}, cells
}

// deflated is b raw-deflated, in base64.
func deflated(b []byte) string {
	var packed bytes.Buffer
	fw, _ := flate.NewWriter(&packed, flate.BestSpeed)
	fw.Write(b)
	fw.Close()
	return base64.StdEncoding.EncodeToString(packed.Bytes())
}

// The blocks changed as they happen (a dig, a placement, a crop grown, sand
// fallen): every tick the server changed some in the box, a changes event,
// each change its block's palette entry and its place — so a reader sees a
// tree come down log by log, not gone between two blocks events.

// change is a block changed: its palette entry and where.
type change struct {
	paletteEntry
	X int `json:"x"`
	Y int `json:"y"`
	Z int `json:"z"`
}

// noteChange keeps a change the world was told (the packet goroutine).
func (r *robot) noteChange(pos world.BlockPos, s block.StateID) {
	r.changeMu.Lock()
	if r.changed == nil {
		r.changed = map[world.BlockPos]block.StateID{}
	}
	r.changed[pos] = s // the last one of a place in a tick
	r.changeMu.Unlock()
}

// reportChanges tells, each tick, the changes in the box round the robot.
func (r *robot) reportChanges() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
		r.changeMu.Lock()
		got := r.changed
		r.changed = nil
		r.changeMu.Unlock()
		if len(got) == 0 {
			continue
		}
		p := r.player.Position()
		x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
		var out []change
		for pos, s := range got {
			if abs(pos.X-x) > blocksSize || abs(pos.Z-z) > blocksSize || pos.Y < y-blocksDown || pos.Y > y+blocksUp {
				continue // outside the box a reader draws
			}
			out = append(out, change{paletteEntry: entryOf(s), X: pos.X, Y: pos.Y, Z: pos.Z})
		}
		if len(out) == 0 {
			continue
		}
		sort.Slice(out, func(i, j int) bool {
			a, b := out[i], out[j]
			if a.Y != b.Y {
				return a.Y < b.Y
			}
			if a.Z != b.Z {
				return a.Z < b.Z
			}
			return a.X < b.X
		})
		events.emit("changes", map[string]any{"changes": out})
	}
}
