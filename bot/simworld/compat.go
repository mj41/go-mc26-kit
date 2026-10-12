package simworld

import (
	"reflect"

	"github.com/mj41/go-mc26/protocol/play"
)

// The packets the simulated game sends are built for the library of any version the kit supports (README.md): a few
// changed their shape between 26.2 and 26.3, and a struct literal of one shape does not compile against the other.
// These builders set the fields by name, those the version's packet has.

// positionSync is an EntityPositionSync of the entity at x y z facing yaw and pitch: 26.3 tells a position path
// (a linear one, the zero case) and the two rotations beside it; 26.1 and 26.2 a position, a movement (none) and
// the rotations in one value.
func positionSync(id int32, x, y, z float64, yaw, pitch float64, onGround bool) play.EntityPositionSync {
	var p play.EntityPositionSync
	v := reflect.ValueOf(&p).Elem()
	set := func(root reflect.Value, name string, f float64) {
		if fv := root.FieldByName(name); fv.IsValid() {
			fv.SetFloat(f)
		}
	}
	v.FieldByName("ID").SetInt(int64(id))
	if f := v.FieldByName("Position"); f.IsValid() { // 26.3
		set(f, "X", x)
		set(f, "Y", y)
		set(f, "Z", z)
		set(v, "YRot", yaw)
		set(v, "XRot", pitch)
	} else if f := v.FieldByName("Values"); f.IsValid() { // 26.1, 26.2
		pos := f.FieldByName("Position")
		set(pos, "X", x)
		set(pos, "Y", y)
		set(pos, "Z", z)
		set(f, "YRot", yaw)
		set(f, "XRot", pitch)
	}
	v.FieldByName("OnGround").SetBool(onGround)
	return p
}

// withSound sets an explosion's PlaySound where the version has it (26.3): the game before it always played it.
func withSound(e play.Explode) play.Explode {
	if f := reflect.ValueOf(&e).Elem().FieldByName("PlaySound"); f.IsValid() {
		f.SetBool(true)
	}
	return e
}
