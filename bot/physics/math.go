package physics

import "math"

// Vec3 is a position or a movement, in blocks (Vec3).
type Vec3 struct{ X, Y, Z float64 }

func (v Vec3) Add(o Vec3) Vec3        { return Vec3{v.X + o.X, v.Y + o.Y, v.Z + o.Z} }
func (v Vec3) Sub(o Vec3) Vec3        { return Vec3{v.X - o.X, v.Y - o.Y, v.Z - o.Z} }
func (v Vec3) Scale(f float64) Vec3   { return Vec3{v.X * f, v.Y * f, v.Z * f} }
func (v Vec3) LengthSqr() float64     { return v.X*v.X + v.Y*v.Y + v.Z*v.Z }
func (v Vec3) HorizontalSqr() float64 { return v.X*v.X + v.Z*v.Z }

func (v Vec3) axis(a int) float64 {
	switch a {
	case 0:
		return v.X
	case 1:
		return v.Y
	}
	return v.Z
}

func (v Vec3) with(a int, f float64) Vec3 {
	switch a {
	case 0:
		v.X = f
	case 1:
		v.Y = f
	default:
		v.Z = f
	}
	return v
}

// AABB is a box in world coordinates.
type AABB struct{ MinX, MinY, MinZ, MaxX, MaxY, MaxZ float64 }

func (b AABB) Move(d Vec3) AABB {
	return AABB{b.MinX + d.X, b.MinY + d.Y, b.MinZ + d.Z, b.MaxX + d.X, b.MaxY + d.Y, b.MaxZ + d.Z}
}

// ExpandTowards grows the box in the direction of d (AABB.expandTowards).
func (b AABB) ExpandTowards(d Vec3) AABB {
	if d.X < 0 {
		b.MinX += d.X
	} else {
		b.MaxX += d.X
	}
	if d.Y < 0 {
		b.MinY += d.Y
	} else {
		b.MaxY += d.Y
	}
	if d.Z < 0 {
		b.MinZ += d.Z
	} else {
		b.MaxZ += d.Z
	}
	return b
}

func (b AABB) Deflate(f float64) AABB {
	return AABB{b.MinX + f, b.MinY + f, b.MinZ + f, b.MaxX - f, b.MaxY - f, b.MaxZ - f}
}

func (b AABB) Intersects(o AABB) bool {
	return b.MinX < o.MaxX && b.MaxX > o.MinX && b.MinY < o.MaxY && b.MaxY > o.MinY && b.MinZ < o.MaxZ && b.MaxZ > o.MinZ
}

func (b AABB) min(a int) float64 {
	switch a {
	case 0:
		return b.MinX
	case 1:
		return b.MinY
	}
	return b.MinZ
}

func (b AABB) max(a int) float64 {
	switch a {
	case 0:
		return b.MaxX
	case 1:
		return b.MaxY
	}
	return b.MaxZ
}

// sinTable is Mth.SIN: the game's sine and cosine are this table, and moving
// exactly as the client does means using it too.
var sinTable = func() [65536]float32 {
	var t [65536]float32
	for i := range t {
		t[i] = float32(math.Sin(float64(i) / 10430.378350470453))
	}
	return t
}()

// sin and cos are Mth.sin and Mth.cos.
func sin(f float64) float32 { return sinTable[int(int64(f*10430.378350470453)&65535)] }
func cos(f float64) float32 { return sinTable[int(int64(f*10430.378350470453+16384.0)&65535)] }

// degToRad is the float the game multiplies an angle by: (float)(Math.PI / 180.0).
const degToRad = float32(math.Pi / 180)

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func floor(v float64) int { return int(math.Floor(v)) }

// equal is Mth.equal: closer than 1e-5 (a float).
func equal(a, b float64) bool { return math.Abs(b-a) < float64(float32(1.0e-5)) }
