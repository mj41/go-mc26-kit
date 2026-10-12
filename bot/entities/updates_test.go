package entities_test

import (
	"slices"
	"testing"
	"testing/synctest"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

func stack(item string) types.ItemStack {
	return types.ItemStack{SlotData: component.SlotData{Count: 1, Item: pk.VarInt(slices.Index(registryid.Item, item))}}
}

func spawn(id int32, kind string, x, y, z float64) play.AddEntity {
	return play.AddEntity{ID: pk.VarInt(id), Type: pk.VarInt(slices.Index(registryid.EntityType, kind)), X: pk.Double(x), Y: pk.Double(y), Z: pk.Double(z)}
}

// What the server tells of an entity after its spawn, as the game's client reads it: a rotation and a head
// turn in protocol angles (a byte, 1/256 of a turn), a teleport absolute or relative per axis (the Relative
// bits: x 1, y 2, z 4, yaw 8, pitch 16), its equipment by slot, and its passengers.
func TestEntityUpdates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		b := fakeserver.Join(t, s, "Watcher")
		send := func(ps ...fakeserver.Clientbound) {
			t.Helper()
			for _, p := range ps {
				if err := s.Send(p); err != nil {
					t.Fatal(err)
				}
			}
			synctest.Wait()
		}
		send(spawn(100, "minecraft:zombie", 3, 64, 3), spawn(101, "minecraft:horse", 0, 64, 0))

		// rotation, and the head: 64 is a quarter turn, -32 an eighth back
		send(play.MoveEntityRot{EntityID: 100, YRot: 64, XRot: -32, OnGround: true}, play.RotateHead{EntityID: 100, YHeadRot: -64})
		if e, _ := b.Entities.Get(100); e.Yaw != 90 || e.Pitch != -45 || !e.OnGround || e.HeadYaw != -90 {
			t.Errorf("after the rotation: yaw %v pitch %v on ground %v head %v, want 90 -45 true -90", e.Yaw, e.Pitch, e.OnGround, e.HeadYaw)
		}

		// a teleport, absolute
		send(play.TeleportEntity{ID: 100, Change: types.PositionMoveRotation{Position: types.Vec3{X: 10, Y: 70, Z: -5}, YRot: 45, XRot: 10}})
		if e, _ := b.Entities.Get(100); e.X != 10 || e.Y != 70 || e.Z != -5 || e.Yaw != 45 || e.Pitch != 10 || e.OnGround {
			t.Errorf("after the absolute teleport: %v %v %v yaw %v pitch %v on ground %v, want 10 70 -5 45 10 false", e.X, e.Y, e.Z, e.Yaw, e.Pitch, e.OnGround)
		}
		// x and the yaw relative, the rest absolute
		send(play.TeleportEntity{ID: 100, Relatives: 1 | 8, OnGround: true,
			Change: types.PositionMoveRotation{Position: types.Vec3{X: 2, Y: 65, Z: 1}, YRot: 5, XRot: 20}})
		if e, _ := b.Entities.Get(100); e.X != 12 || e.Y != 65 || e.Z != 1 || e.Yaw != 50 || e.Pitch != 20 || !e.OnGround {
			t.Errorf("after the relative teleport: %v %v %v yaw %v pitch %v, want 12 65 1 50 20", e.X, e.Y, e.Z, e.Yaw, e.Pitch)
		}

		// equipment, by slot (mainhand 0, head 5); an empty stack empties a slot, the others kept
		sword, helmet := stack("minecraft:iron_sword"), stack("minecraft:diamond_helmet")
		send(play.SetEquipment{Entity: 100, Slots: types.SetEquipmentEntryList{{SlotID: 0, ItemStack: sword}, {SlotID: 5, ItemStack: helmet}}})
		if e, _ := b.Entities.Get(100); e.Equipment[0].Item != sword.Item || e.Equipment[5].Item != helmet.Item {
			t.Errorf("the equipment: mainhand %v head %v, want the sword and the helmet", e.Equipment[0].Item, e.Equipment[5].Item)
		}
		send(play.SetEquipment{Entity: 100, Slots: types.SetEquipmentEntryList{{SlotID: 0, ItemStack: types.ItemStack{}}}})
		if e, _ := b.Entities.Get(100); e.Equipment[0].Count != 0 || e.Equipment[5].Item != helmet.Item {
			t.Errorf("after the sword was taken: mainhand %d of %v, head %v", e.Equipment[0].Count, e.Equipment[0].Item, e.Equipment[5].Item)
		}

		// passengers: the zombie on the horse, then off
		send(play.SetPassengers{Vehicle: 101, Passengers: types.List[pk.VarInt, *pk.VarInt]{100}})
		if v, at, ok := b.Entities.Vehicle(100); !ok || v.ID != 101 || at != 0 {
			t.Errorf("the zombie's vehicle: %v %d %v, want the horse, first", ok, v.ID, at)
		}
		if h, _ := b.Entities.Get(101); !slices.Equal(h.Passengers, []int32{100}) {
			t.Errorf("the horse's passengers %v, want the zombie", h.Passengers)
		}
		send(play.SetPassengers{Vehicle: 101})
		if _, _, ok := b.Entities.Vehicle(100); ok {
			t.Error("the zombie still rides after the horse's passengers were emptied")
		}
		if h, _ := b.Entities.Get(101); len(h.Passengers) != 0 {
			t.Errorf("the horse's passengers %v after it was emptied", h.Passengers)
		}
	})
}
