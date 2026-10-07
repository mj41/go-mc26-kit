package physics

import (
	"math"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Boat is a boat its passenger steers, as AbstractBoat ticks on the client
// that controls it: it floats, its paddles push and turn it, it moves itself,
// and the client tells the server where it went (ServerboundMoveVehicle) —
// the server only checks the move.
//
// Not ported: bubble columns, the fall damage, the block speed factor (soul
// sand under a boat), the push of other entities.
type Boat struct {
	Pos, Vel Vec3
	Yaw      float32
	// DeltaRotation is the turn of the last tick, which the passenger turns
	// with (AbstractBoat.positionRider).
	DeltaRotation float32
	Width, Height float64
	OnGround      bool
	// Paddles is which paddles move this tick (ServerboundPaddleBoat): left, right.
	Paddles [2]bool

	status, oldStatus boatStatus
	waterLevel        float64
	landFriction      float32
	lastYd            float64
}

type boatStatus int

const (
	boatNone boatStatus = iota
	boatInWater
	boatUnderWater
	boatUnderFlowingWater
	boatOnLand
	boatInAir
)

// boatGravity is AbstractBoat.getDefaultGravity.
const boatGravity = 0.04

// NewBoat is a boat of the given size at rest at pos.
func NewBoat(pos Vec3, yaw float32, width, height float64) *Boat {
	return &Boat{Pos: pos, Yaw: yaw, Width: width, Height: height}
}

// Box is the boat's bounding box.
func (b *Boat) Box() AABB {
	w := b.Width / 2
	return AABB{b.Pos.X - w, b.Pos.Y, b.Pos.Z - w, b.Pos.X + w, b.Pos.Y + b.Height, b.Pos.Z + w}
}

// Tick is one tick of AbstractBoat.tick on the controlling client, with the
// passenger's keys (LocalPlayer.rideTick → AbstractBoat.setInput).
func (b *Boat) Tick(w Blocks, left, right, up, down bool) {
	b.oldStatus = b.status
	b.status = b.getStatus(w)
	b.floatBoat(w)
	b.controlBoat(left, right, up, down)
	b.move(w, b.Vel)
}

func (b *Boat) getStatus(w Blocks) boatStatus {
	if s := b.isUnderwater(w); s != boatNone {
		b.waterLevel = b.Box().MaxY
		return s
	}
	if b.checkInWater(w) {
		return boatInWater
	}
	if f := b.groundFriction(w); f > 0 {
		b.landFriction = f
		return boatOnLand
	}
	return boatInAir
}

// waterHeight is FluidState.getHeight for water at pos: 0 and false where
// there is none.
func waterHeight(w Blocks, pos world.BlockPos) (float64, bool) {
	h, ok := fluidSurface(w, pos, "water")
	if !ok {
		return 0, false
	}
	return h - float64(pos.Y), true
}

// waterLevelAbove is AbstractBoat.getWaterLevelAbove.
func (b *Boat) waterLevelAbove(w Blocks) float64 {
	box := b.Box()
	minX, maxX := floor(box.MinX), int(math.Ceil(box.MaxX))
	minY, maxY := floor(box.MaxY), int(math.Ceil(box.MaxY-b.lastYd))
	minZ, maxZ := floor(box.MinZ), int(math.Ceil(box.MaxZ))
	y := minY
	for ; y < maxY; y++ {
		var height float64
		full := false
		for x := minX; x < maxX && !full; x++ {
			for z := minZ; z < maxZ; z++ {
				if h, ok := waterHeight(w, world.BlockPos{X: x, Y: y, Z: z}); ok {
					height = math.Max(height, float64(float32(h)))
				}
				if height >= 1 {
					full = true
					break
				}
			}
		}
		if !full {
			return float64(float32(y) + float32(height)) // int + float, a float
		}
	}
	return float64(float32(maxY + 1))
}

// groundFriction is AbstractBoat.getGroundFriction: the friction of the
// blocks just under the boat, averaged (a lily pad does not count).
func (b *Boat) groundFriction(w Blocks) float32 {
	bb := b.Box()
	box := AABB{bb.MinX, bb.MinY - 0.001, bb.MinZ, bb.MaxX, bb.MinY, bb.MaxZ}
	x0, x1 := floor(box.MinX)-1, int(math.Ceil(box.MaxX))+1
	y0, y1 := floor(box.MinY)-1, int(math.Ceil(box.MaxY))+1
	z0, z1 := floor(box.MinZ)-1, int(math.Ceil(box.MaxZ))+1
	var friction float32
	count := 0
	for x := x0; x < x1; x++ {
		for z := z0; z < z1; z++ {
			edges := 0
			if x == x0 || x == x1-1 {
				edges++
			}
			if z == z0 || z == z1-1 {
				edges++
			}
			if edges == 2 {
				continue
			}
			for y := y0; y < y1; y++ {
				if edges > 0 && (y == y0 || y == y1-1) {
					continue
				}
				s, ok := w.BlockAt(world.BlockPos{X: x, Y: y, Z: z})
				if !ok || world.StateString(s) == "minecraft:lily_pad" {
					continue
				}
				for _, c := range block.CollisionShape(s) {
					cb := AABB{c.MinX + float64(x), c.MinY + float64(y), c.MinZ + float64(z), c.MaxX + float64(x), c.MaxY + float64(y), c.MaxZ + float64(z)}
					if cb.Intersects(box) {
						friction += block.BehaviourOf(s).Friction
						count++
						break
					}
				}
			}
		}
	}
	if count == 0 {
		return 0 // the game divides 0 by 0: NaN, which is not above 0
	}
	return friction / float32(count)
}

// checkInWater is AbstractBoat.checkInWater: the water at the boat's bottom.
func (b *Boat) checkInWater(w Blocks) bool {
	box := b.Box()
	minX, maxX := floor(box.MinX), int(math.Ceil(box.MaxX))
	minY, maxY := floor(box.MinY), int(math.Ceil(box.MinY+0.001))
	minZ, maxZ := floor(box.MinZ), int(math.Ceil(box.MaxZ))
	in := false
	b.waterLevel = -math.MaxFloat64
	for x := minX; x < maxX; x++ {
		for y := minY; y < maxY; y++ {
			for z := minZ; z < maxZ; z++ {
				if h, ok := waterHeight(w, world.BlockPos{X: x, Y: y, Z: z}); ok {
					height := float64(float32(y) + float32(h)) // int + float, a float
					b.waterLevel = math.Max(height, b.waterLevel)
					in = in || box.MinY < height
				}
			}
		}
	}
	return in
}

// isUnderwater is AbstractBoat.isUnderwater: water over the boat's top.
func (b *Boat) isUnderwater(w Blocks) boatStatus {
	box := b.Box()
	maxY := box.MaxY + 0.001
	x0, x1 := floor(box.MinX), int(math.Ceil(box.MaxX))
	y0, y1 := floor(box.MaxY), int(math.Ceil(maxY))
	z0, z1 := floor(box.MinZ), int(math.Ceil(box.MaxZ))
	under := false
	for x := x0; x < x1; x++ {
		for y := y0; y < y1; y++ {
			for z := z0; z < z1; z++ {
				pos := world.BlockPos{X: x, Y: y, Z: z}
				h, ok := waterHeight(w, pos)
				if !ok || maxY >= float64(float32(y)+float32(h)) {
					continue
				}
				s, _ := w.BlockAt(pos)
				if f := block.FluidOf(s); f == nil || !f.Source {
					return boatUnderFlowingWater
				}
				under = true
			}
		}
	}
	if under {
		return boatUnderWater
	}
	return boatNone
}

// floatBoat is AbstractBoat.floatBoat: gravity, the water's lift, friction.
func (b *Boat) floatBoat(w Blocks) {
	vspeed := -boatGravity
	buoyancy := 0.0
	invFriction := float32(0.05)
	if b.oldStatus == boatInAir && b.status != boatInAir && b.status != boatOnLand {
		b.waterLevel = b.Pos.Y + b.Height
		targetY := b.waterLevelAbove(w) - b.Height + 0.101
		if noCollision(w, b.Box().Move(Vec3{0, targetY - b.Pos.Y, 0})) {
			b.Pos.Y = targetY
			b.Vel.Y = 0
			b.lastYd = 0
		}
		b.status = boatInWater
		return
	}
	switch b.status {
	case boatInWater:
		buoyancy = (b.waterLevel - b.Pos.Y) / b.Height
		invFriction = 0.9
	case boatUnderFlowingWater:
		vspeed = -7.0e-4
		invFriction = 0.9
	case boatUnderWater:
		buoyancy = float64(float32(0.01))
		invFriction = 0.45
	case boatInAir:
		invFriction = 0.9
	case boatOnLand:
		invFriction = b.landFriction
		b.landFriction /= 2 // a player steers it
	}
	b.Vel = Vec3{b.Vel.X * float64(invFriction), b.Vel.Y + vspeed, b.Vel.Z * float64(invFriction)}
	b.DeltaRotation *= invFriction
	if buoyancy > 0 {
		b.Vel.Y = (b.Vel.Y + buoyancy*(boatGravity/0.65)) * 0.75
	}
}

// controlBoat is AbstractBoat.controlBoat: the keys turn the boat and pull it
// forward or back; the paddles show which.
func (b *Boat) controlBoat(left, right, up, down bool) {
	var acceleration float32
	if left {
		b.DeltaRotation--
	}
	if right {
		b.DeltaRotation++
	}
	if right != left && !up && !down {
		acceleration += 0.005
	}
	b.Yaw += b.DeltaRotation
	if up {
		acceleration += 0.04
	}
	if down {
		acceleration -= 0.005
	}
	r := -b.Yaw * float32(math.Pi/180)
	b.Vel.X += float64(sin(float64(r)) * acceleration)
	b.Vel.Z += float64(cos(float64(b.Yaw*float32(math.Pi/180))) * acceleration)
	b.Paddles = [2]bool{right && !left || up, left && !right || up}
}

// move is Entity.move for the boat: no step up, collisions stop the velocity
// along their axis.
func (b *Boat) move(w Blocks, delta Vec3) {
	box := b.Box()
	movement := collide(w, box, delta, b.OnGround, 0)
	if l := movement.LengthSqr(); l > 1e-7 || delta.LengthSqr()-l < 1e-7 {
		b.Pos = b.Pos.Add(movement)
	}
	xCollision, zCollision := !equal(delta.X, movement.X), !equal(delta.Z, movement.Z)
	vertical := delta.Y != movement.Y
	b.OnGround = vertical && delta.Y < 0
	b.lastYd = b.Vel.Y // AbstractBoat.checkFallDamage
	if xCollision {
		b.Vel.X = 0
	}
	if zCollision {
		b.Vel.Z = 0
	}
	if vertical && delta.Y != 0 {
		b.Vel.Y = 0 // Block.updateEntityMovementAfterFallOn
	}
}
