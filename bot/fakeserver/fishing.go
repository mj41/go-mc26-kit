package fakeserver

import (
	"slices"
	"sync"
	"time"

	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/data/soundid"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Fishing is the fake server's fishing, as much as a fishing bot needs (FISHING.md): a UseItem with no
// bobber out casts it, the splash of a bite comes BiteAfter later, and a UseItem within Hooked of the bite
// reels in a catch (a cod, told as a slot's new stack). The player's rod is not checked, and one bobber is
// out at a time. Set Server.Fishing before the player joins.
type Fishing struct {
	BiteAfter time.Duration // the wait for a bite (0: 5 s, the low end of the game's 5 to 30 s)
	Hooked    time.Duration // how long a fish stays on (0: 1 s, the game's 20 to 40 ticks)

	mu     sync.Mutex
	bobber int32       // its entity id; 0: none out
	bitAt  time.Time   // when the fish on the hook bit; zero: none on
	timer  *time.Timer // the next bite, or the fish getting away
	caught int
}

// Caught is how many fish were reeled in.
func (f *Fishing) Caught() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.caught
}

func (f *Fishing) biteAfter() time.Duration { return cmpOr(f.BiteAfter, 5*time.Second) }
func (f *Fishing) hooked() time.Duration    { return cmpOr(f.Hooked, time.Second) }

func cmpOr(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// bobberAt is where a cast lands: a few blocks in front of the spawn.
var bobberAt = [3]float64{Spawn[0], Spawn[1], Spawn[2] + 4}

// use is a UseItem of the player: a cast with no bobber out, else a reel.
func (f *Fishing) use(s *Server, p *Player) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.bobber == 0 { // cast
		s.mu.Lock()
		s.lastID++
		f.bobber = s.lastID
		s.mu.Unlock()
		_ = p.Send(play.AddEntity{ID: pk.VarInt(f.bobber), Type: pk.VarInt(slices.Index(registryid.EntityType, "minecraft:fishing_bobber")),
			X: pk.Double(bobberAt[0]), Y: pk.Double(bobberAt[1]), Z: pk.Double(bobberAt[2]), Data: pk.VarInt(p.EntityID)})
		f.wait(p)
		return
	}
	// reel
	if f.timer != nil {
		f.timer.Stop()
	}
	_ = p.Send(play.RemoveEntities{EntityIds: types.List[pk.VarInt, *pk.VarInt]{pk.VarInt(f.bobber)}})
	if !f.bitAt.IsZero() && time.Since(f.bitAt) <= f.hooked() {
		f.caught++
		cod := types.ItemStack{SlotData: component.SlotData{Count: 1, Item: pk.VarInt(slices.Index(registryid.Item, "minecraft:cod"))}}
		_ = p.Send(play.ContainerSetSlot{ContainerID: 0, Slot: 36 + pk.Short(f.caught%9), ItemStack: cod}) // into the hotbar
	}
	f.bobber, f.bitAt = 0, time.Time{}
}

// wait starts the wait for the next bite; the caller holds f.mu.
func (f *Fishing) wait(p *Player) {
	f.timer = time.AfterFunc(f.biteAfter(), func() { f.bite(p) })
}

// bite is a fish on the hook: the bobber's splash, and a while to reel it in before it gets away and the
// wait starts again.
func (f *Fishing) bite(p *Player) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.bobber == 0 {
		return
	}
	f.bitAt = time.Now()
	_ = p.Send(play.Sound{Sound: types.Holder[types.SoundEvent, *types.SoundEvent]{ID: pk.VarInt(splash + 1)}, // registry id + 1
		Source: types.SoundSourceNeutral, X: pk.Int(bobberAt[0] * 8), Y: pk.Int(bobberAt[1] * 8), Z: pk.Int(bobberAt[2] * 8), // eighths of a block
		Volume: 0.25, Pitch: 1})
	bitAt := f.bitAt
	f.timer = time.AfterFunc(f.hooked(), func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.bobber != 0 && f.bitAt.Equal(bitAt) { // not reeled in: the fish got away
			f.bitAt = time.Time{}
			f.wait(p)
		}
	})
}

// splash is the registry id of the bobber's splash, the sound of a bite, looked up by name.
var splash = func() int {
	for id, name := range soundid.SoundNames {
		if name == "entity.fishing_bobber.splash" {
			return int(id)
		}
	}
	panic("fakeserver: no entity.fishing_bobber.splash sound")
}()
