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
// reels in a catch (a cod, told as a slot's new stack). A UseItem with no fishing rod in the player's hand
// does nothing, as the game's does (a cast at the game start, before a harness gives the rod, is ignored): the
// player holds one from the start when Rod is set, else from GiveRod. One bobber is out at a time. Set
// Server.Fishing before the player joins.
type Fishing struct {
	Rod bool // the player holds a fishing rod from the start

	BiteAfter time.Duration // the wait for a bite (0: 5 s, the low end of the game's 5 to 30 s)
	Hooked    time.Duration // how long a fish stays on (0: 1 s, the game's 20 to 40 ticks)

	mu     sync.Mutex
	bobber int32       // its entity id; 0: none out
	shown  bool        // the player was told of it (its chunk was sent)
	bitAt  time.Time   // when the fish on the hook bit; zero: none on
	timer  *time.Timer // the next bite, or the fish getting away
	caught int
}

// GiveRod puts a fishing rod in the player's main hand (the hotbar's first slot), as /give does, and tells the player.
func (f *Fishing) GiveRod(p *Player) {
	f.mu.Lock()
	f.Rod = true
	f.mu.Unlock()
	rod := types.ItemStack{SlotData: component.SlotData{Count: 1, Item: pk.VarInt(slices.Index(registryid.Item, "minecraft:fishing_rod"))}}
	_ = p.Send(play.ContainerSetSlot{ContainerID: 0, Slot: 36, ItemStack: rod})
}

// Out is whether a bobber is in the water.
func (f *Fishing) Out() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bobber != 0
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

// bobberAt is where a cast lands: a few blocks in front of the player.
func bobberAt(p *Player) [3]float64 {
	at := p.Pos()
	return [3]float64{at[0], at[1], at[2] + 4}
}

// told is whether the bobber is in a chunk the player has been sent: the game tells an entity only to the
// players that track it (ChunkMap.isChunkTracked), and later, when the chunk comes.
func (f *Fishing) told(p *Player) bool {
	at := bobberAt(p)
	return p.chunks.tracked(floorDiv16(at[0]), floorDiv16(at[2]))
}

// chunksSent is a batch of chunks gone to the player: a bobber out that it was not told of, in one of
// them, is told now.
func (f *Fishing) chunksSent(p *Player) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.bobber != 0 && !f.shown && f.told(p) {
		f.show(p)
	}
}

// show tells the player of the bobber; the caller holds f.mu.
func (f *Fishing) show(p *Player) {
	at := bobberAt(p)
	f.shown = true
	_ = p.Send(play.AddEntity{ID: pk.VarInt(f.bobber), Type: pk.VarInt(slices.Index(registryid.EntityType, "minecraft:fishing_bobber")),
		X: pk.Double(at[0]), Y: pk.Double(at[1]), Z: pk.Double(at[2]), Data: pk.VarInt(p.EntityID)})
}

// use is a UseItem of the player: a cast with no bobber out, else a reel.
func (f *Fishing) use(s *Server, p *Player) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.Rod { // no rod in the hand: the use does nothing
		return
	}
	if f.bobber == 0 { // cast
		s.mu.Lock()
		s.lastID++
		f.bobber = s.lastID
		s.mu.Unlock()
		if f.told(p) {
			f.show(p)
		}
		f.wait(p)
		return
	}
	// reel
	if f.timer != nil {
		f.timer.Stop()
	}
	if f.shown {
		_ = p.Send(play.RemoveEntities{EntityIds: types.List[pk.VarInt, *pk.VarInt]{pk.VarInt(f.bobber)}})
	}
	if !f.bitAt.IsZero() && time.Since(f.bitAt) <= f.hooked() {
		f.caught++
		cod := types.ItemStack{SlotData: component.SlotData{Count: 1, Item: pk.VarInt(slices.Index(registryid.Item, "minecraft:cod"))}}
		_ = p.Send(play.ContainerSetSlot{ContainerID: 0, Slot: 36 + pk.Short(f.caught%9), ItemStack: cod}) // into the hotbar
	}
	f.bobber, f.shown, f.bitAt = 0, false, time.Time{}
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
	at := bobberAt(p)
	_ = p.Send(play.Sound{Sound: types.Holder[types.SoundEvent, *types.SoundEvent]{ID: pk.VarInt(splash + 1)}, // registry id + 1
		Source: types.SoundSourceNeutral, X: pk.Int(at[0] * 8), Y: pk.Int(at[1] * 8), Z: pk.Int(at[2] * 8), // eighths of a block
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
