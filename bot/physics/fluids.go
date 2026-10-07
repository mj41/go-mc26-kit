package physics

import (
	"math"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/version"
	"github.com/mj41/go-mc26/level/block"
)

// updateFluids is EntityFluidInteraction.update and Entity.updateFluidInteraction:
// whether the player is in water or lava and how deep, and the push of the
// current: the flow of every fluid block it touches, averaged for a player,
// scaled by 0.014 for water and 0.0023 for lava.
func (p *Player) updateFluids(w Blocks, tags Tags) {
	box := p.Box()
	search := box.Deflate(0.001)
	type current struct {
		height float64
		sum    Vec3
		n      int
	}
	heights := map[string]float64{}
	currents := map[string]*current{"water": {}, "lava": {}}
	for x := floor(search.MinX); x <= int(math.Ceil(search.MaxX))-1; x++ {
		for y := floor(search.MinY); y <= int(math.Ceil(search.MaxY))-1; y++ {
			for z := floor(search.MinZ); z <= int(math.Ceil(search.MaxZ))-1; z++ {
				pos := world.BlockPos{X: x, Y: y, Z: z}
				s, ok := w.BlockAt(pos)
				if !ok {
					continue
				}
				f := block.FluidOf(s)
				kind := fluidKind(f)
				if kind == "" {
					continue
				}
				top, _ := fluidSurface(w, pos, kind)
				if top < search.MinY {
					continue
				}
				heights[kind] = math.Max(top-box.MinY, heights[kind])
				c := currents[kind]
				flow := flowAt(w, tags, pos, f, kind)
				c.height = math.Max(heights[kind], c.height)
				if c.height < 0.4 {
					flow = flow.Scale(c.height)
				}
				c.sum = c.sum.Add(flow)
				c.n++
			}
		}
	}
	p.waterHeight, p.InWater = heights["water"], heights["water"] > 0
	p.lavaHeight, p.InLava = heights["lava"], heights["lava"] > 0
	if p.InWater {
		p.FallDistance = 0
	}
	push := func(c *current, scale float64) {
		if c.n == 0 || c.sum.LengthSqr() < float64(float32(1.0e-5)) {
			return
		}
		impulse := c.sum.Scale(1 / float64(c.n)).Scale(scale) // a player: the average, not the direction
		if math.Abs(p.Vel.X) < 0.003 && math.Abs(p.Vel.Z) < 0.003 && math.Sqrt(impulse.LengthSqr()) < 0.0045000000000000005 {
			impulse = normalize(impulse).Scale(0.0045000000000000005)
		}
		p.Vel = p.Vel.Add(impulse)
	}
	if p.InWater {
		push(currents["water"], 0.014)
	}
	if p.InLava {
		push(currents["lava"], 0.0023333333333333335) // 0.007 where lava is fast (the nether)
	}
}

// normalize is Vec3.normalize: a vector shorter than 1e-5 has no direction.
func normalize(v Vec3) Vec3 {
	l := math.Sqrt(v.LengthSqr())
	if l < float64(float32(1.0e-5)) {
		return Vec3{}
	}
	return v.Scale(1 / l)
}

// horizontal is Direction.Plane.HORIZONTAL: north, east, south, west, and
// the face index (Direction ordinal) of each.
var horizontal = [4]struct {
	dx, dz int
	face   int
}{{0, -1, 2}, {1, 0, 5}, {0, 1, 3}, {-1, 0, 4}}

// flowAt is FlowingFluid.getFlow: toward the lower neighbours, by how much
// lower they are; down the side of a falling fluid that runs along a wall.
func flowAt(w Blocks, tags Tags, pos world.BlockPos, f *block.Fluid, kind string) Vec3 {
	affects := func(nf *block.Fluid) bool { return nf == nil || fluidKind(nf) == kind }
	own := f.Height
	var fx, fz float64
	for _, d := range horizontal {
		n := world.BlockPos{X: pos.X + d.dx, Y: pos.Y, Z: pos.Z + d.dz}
		ns, _ := w.BlockAt(n)
		nf := block.FluidOf(ns)
		if !affects(nf) {
			continue
		}
		nh := float32(0)
		if nf != nil {
			nh = nf.Height
		}
		dist := float32(0)
		if nh == 0 {
			if !blocksFluidFlow(tags, ns) {
				below, _ := w.BlockAt(world.BlockPos{X: n.X, Y: n.Y - 1, Z: n.Z})
				if bf := block.FluidOf(below); affects(bf) && bf != nil && bf.Height > 0 {
					dist = own - (bf.Height - 0.8888889)
				}
			}
		} else {
			dist = own - nh
		}
		if dist != 0 {
			fx += float64(d.dx) * float64(dist)
			fz += float64(d.dz) * float64(dist)
		}
	}
	flow := Vec3{fx, 0, fz}
	if f.Falling {
		for _, d := range horizontal {
			n := world.BlockPos{X: pos.X + d.dx, Y: pos.Y, Z: pos.Z + d.dz}
			up := world.BlockPos{X: n.X, Y: n.Y + 1, Z: n.Z}
			if solidFace(w, n, d.face, kind) || solidFace(w, up, d.face, kind) {
				flow = normalize(flow).Add(Vec3{0, -6, 0})
				break
			}
		}
	}
	return normalize(flow)
}

// blocksFluidFlow: the block a flow cannot look under — the block tag
// minecraft:blocks_fluid_flow from 26.3, the legacy "blocks motion" before
// (a solid block other than cobweb and bamboo sapling).
func blocksFluidFlow(tags Tags, s block.StateID) bool {
	if version.ProtocolVersion >= 777 {
		return tags != nil && tags.BlockIn(s, "minecraft:blocks_fluid_flow")
	}
	switch block.StateList[s].ID() {
	case "minecraft:cobweb", "minecraft:bamboo_sapling":
		return false
	}
	return block.Solid(s)
}

// solidFace is FlowingFluid.isSolidFace: a sturdy face of another block (not
// the same fluid, not ice).
func solidFace(w Blocks, pos world.BlockPos, face int, kind string) bool {
	s, ok := w.BlockAt(pos)
	if !ok || fluidKind(block.FluidOf(s)) == kind {
		return false
	}
	if face == 1 { // up
		return true
	}
	switch block.StateList[s].ID() {
	case "minecraft:ice", "minecraft:packed_ice", "minecraft:blue_ice", "minecraft:frosted_ice":
		return false
	}
	return block.SturdyFaces(s)&(1<<face) != 0
}

// fluidKind is the fluid tag a block state's fluid belongs to: "water" for
// minecraft:water and minecraft:flowing_water, "lava" likewise.
func fluidKind(f *block.Fluid) string {
	if f == nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimPrefix(f.Name, "minecraft:"), "flowing_")
}

// fluidSurface is the height of the fluid of kind at pos (FluidState.getHeight):
// the whole block when the same fluid is above it, its own height otherwise.
func fluidSurface(w Blocks, pos world.BlockPos, kind string) (float64, bool) {
	s, ok := w.BlockAt(pos)
	if !ok {
		return 0, false
	}
	f := block.FluidOf(s)
	if fluidKind(f) != kind {
		return 0, false
	}
	if above, ok := w.BlockAt(world.BlockPos{X: pos.X, Y: pos.Y + 1, Z: pos.Z}); ok && fluidKind(block.FluidOf(above)) == kind {
		return float64(pos.Y) + 1, true
	}
	return float64(pos.Y) + float64(f.Height), true
}

// containsLiquid is Level.containsAnyLiquid.
func containsLiquid(w Blocks, box AABB) bool {
	for x := floor(box.MinX); x < int(math.Ceil(box.MaxX)); x++ {
		for y := floor(box.MinY); y < int(math.Ceil(box.MaxY)); y++ {
			for z := floor(box.MinZ); z < int(math.Ceil(box.MaxZ)); z++ {
				if s, ok := w.BlockAt(world.BlockPos{X: x, Y: y, Z: z}); ok && block.FluidOf(s) != nil {
					return true
				}
			}
		}
	}
	return false
}
