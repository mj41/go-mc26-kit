package physics

import (
	"math"
	"slices"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Blocks is the world as the physics reads it.
type Blocks interface {
	BlockAt(world.BlockPos) (block.StateID, bool)
}

// EntitySolids is a Blocks that also knows the entities a player collides
// with (Entity.canBeCollidedWith: a boat, a living shulker, an adult happy
// ghast from above): their boxes join the blocks' in the player's move.
type EntitySolids interface {
	EntityBoxes(area AABB) []AABB
}

// colliders returns the block boxes that intersect area (BlockCollisions,
// which the game walks with a block of margin for the shapes taller than a
// block: fences, walls). A block of an unloaded chunk collides with nothing.
func colliders(w Blocks, area AABB) []AABB {
	var out []AABB
	if es, ok := w.(EntitySolids); ok {
		for _, b := range es.EntityBoxes(area) {
			if b.Intersects(area) {
				out = append(out, b)
			}
		}
	}
	x0, x1 := floor(area.MinX-1e-7)-1, floor(area.MaxX+1e-7)+1
	y0, y1 := floor(area.MinY-1e-7)-1, floor(area.MaxY+1e-7)+1
	z0, z1 := floor(area.MinZ-1e-7)-1, floor(area.MaxZ+1e-7)+1
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				state, ok := w.BlockAt(world.BlockPos{X: x, Y: y, Z: z})
				if !ok {
					continue
				}
				for _, b := range block.CollisionShape(state) {
					box := AABB{b.MinX + float64(x), b.MinY + float64(y), b.MinZ + float64(z), b.MaxX + float64(x), b.MaxY + float64(y), b.MaxZ + float64(z)}
					if box.Intersects(area) {
						out = append(out, box)
					}
				}
			}
		}
	}
	return out
}

// collideAxis is Shapes.collide over boxes: how far moving can travel along
// axis a, at most distance, before it touches one of them.
func collideAxis(a int, moving AABB, boxes []AABB, distance float64) float64 {
	b, c := (a+1)%3, (a+2)%3
	for _, box := range boxes {
		if math.Abs(distance) < 1e-7 {
			return 0
		}
		// only the boxes that overlap the moving one on the two other axes
		if box.max(b) <= moving.min(b)+1e-7 || box.min(b) >= moving.max(b)-1e-7 ||
			box.max(c) <= moving.min(c)+1e-7 || box.min(c) >= moving.max(c)-1e-7 {
			continue
		}
		if distance > 0 && box.min(a) >= moving.max(a)-1e-7 {
			if d := box.min(a) - moving.max(a); d >= -1e-7 {
				distance = math.Min(distance, d)
			}
		} else if distance < 0 && box.max(a) <= moving.min(a)+1e-7 {
			if d := box.max(a) - moving.min(a); d <= 1e-7 {
				distance = math.Max(distance, d)
			}
		}
	}
	return distance
}

// collideWithShapes is Entity.collideWithShapes: Y first, then the larger of
// the horizontal axes last (Direction.axisStepOrder).
func collideWithShapes(movement Vec3, box AABB, boxes []AABB) Vec3 {
	if len(boxes) == 0 {
		return movement
	}
	order := [3]int{1, 0, 2}
	if math.Abs(movement.X) < math.Abs(movement.Z) {
		order = [3]int{1, 2, 0}
	}
	var resolved Vec3
	for _, a := range order {
		if d := movement.axis(a); d != 0 {
			resolved = resolved.with(a, collideAxis(a, box.Move(resolved), boxes, d))
		}
	}
	return resolved
}

// collide is Entity.collide: the movement the blocks allow, stepping up onto
// what is at most stepHeight high when walking into it on the ground.
func collide(w Blocks, box AABB, movement Vec3, onGround bool, stepHeight float32) Vec3 {
	step := movement
	if movement.LengthSqr() != 0 {
		step = collideWithShapes(movement, box, colliders(w, box.ExpandTowards(movement)))
	}
	xCollision, yCollision, zCollision := movement.X != step.X, movement.Y != step.Y, movement.Z != step.Z
	onGroundAfter := yCollision && movement.Y < 0
	if stepHeight > 0 && (onGroundAfter || onGround) && (xCollision || zCollision) {
		grounded := box
		if onGroundAfter {
			grounded = box.Move(Vec3{0, step.Y, 0})
		}
		stepUp := grounded.ExpandTowards(Vec3{movement.X, float64(stepHeight), movement.Z})
		if !onGroundAfter {
			stepUp = stepUp.ExpandTowards(Vec3{0, float64(float32(-1.0e-5)), 0})
		}
		boxes := colliders(w, stepUp)
		for _, h := range stepHeights(grounded, boxes, stepHeight, float32(step.Y)) {
			fromGround := collideWithShapes(Vec3{movement.X, float64(h), movement.Z}, grounded, boxes)
			if fromGround.HorizontalSqr() > step.HorizontalSqr() {
				return fromGround.Sub(Vec3{0, box.MinY - grounded.MinY, 0})
			}
		}
	}
	return step
}

// stepHeights is Entity.collectCandidateStepUpHeights: the heights of the
// colliders' faces above the box's bottom, up to maxStep, lowest first.
func stepHeights(box AABB, boxes []AABB, maxStep, skip float32) []float32 {
	var out []float32
	for _, b := range boxes {
		for _, y := range [2]float64{b.MinY, b.MaxY} {
			h := float32(y - box.MinY)
			if h < 0 || h == skip || h > maxStep || slices.Contains(out, h) {
				continue
			}
			out = append(out, h)
		}
	}
	slices.Sort(out)
	return out
}

// noCollision reports whether box touches no block (Level.noCollision for
// blocks only).
func noCollision(w Blocks, box AABB) bool {
	return len(colliders(w, box)) == 0
}
