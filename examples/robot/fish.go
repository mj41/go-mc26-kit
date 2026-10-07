package main

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// Fishing is the robot's first food, as it hunts no animal: a rod (three
// sticks, two string — from the spiders it fights, the cobwebs it meets
// underground), cast into still water under the sky. Its field's first channel
// is twelve water in a row with the pool: cast along it from the headland, a
// long throw still lands in water. Else the water it found. A bite is the
// bobber's splash — the server's sound at its own bobber; the rod is pulled
// in then and the catch flies to it. Raw fish are cooked in its furnaces.

var splashSound = soundID("minecraft:entity.fishing_bobber.splash")

func soundID(name string) int32 {
	for i, n := range registryid.SoundEvent {
		if n == name {
			return int32(i)
		}
	}
	return -1
}

// splash is a bobber's splash heard: where, when.
type splash struct {
	at      time.Time
	x, y, z float64
}

// soundListener hands the bobbers' splashes to the fishing (a full queue
// drops them: nobody fishing).
func (r *robot) soundListener() bot.PacketHandler {
	return bot.PacketHandler{ID: packetid.ClientboundPlaySound, F: func(p pk.Packet) error {
		var s play.Sound
		if err := p.Scan(&s); err != nil {
			return nil // a sound read wrong is no reason to drop the connection
		}
		if int32(s.Sound.ID)-1 != splashSound {
			return nil
		}
		select {
		case r.splashes <- splash{at: time.Now(), x: float64(s.X) / 8, y: float64(s.Y) / 8, z: float64(s.Z) / 8}:
		default:
		}
		return nil
	}}
}

// fishFood are the catches it eats (cooked): a pufferfish is not food, nor
// a tropical fish worth the furnace.
var fishFood = map[string]string{"minecraft:cod": "minecraft:cooked_cod", "minecraft:salmon": "minecraft:cooked_salmon"}

func (r *robot) fishCount() int {
	n := 0
	for raw, cooked := range fishFood {
		n += r.ui.Count(raw) + r.ui.Count(cooked)
	}
	return n
}

// fishingSpot answers where to stand and what to cast at: from its field's
// headland along the first channel, else beside still water it knows.
func (r *robot) fishingSpot() (stand, at world.BlockPos, err error) {
	if c := r.field; c != nil && r.isSource(world.BlockPos{X: c.X, Y: c.Y, Z: c.Z}) {
		stand = world.BlockPos{X: c.X + fieldFrom + fieldLen, Y: c.Y + 1, Z: c.Z}
		at = world.BlockPos{X: c.X + 1, Y: c.Y, Z: c.Z} // five out along the channel
		if r.standable(stand) {
			return stand, at, nil
		}
	}
	for _, w := range []*world.BlockPos{r.water, r.bucketWater} {
		if w == nil || !r.isSource(*w) {
			continue
		}
		if spots := r.beside(*w); len(spots) > 0 {
			return spots[0], *w, nil
		}
	}
	if w, ok := r.waterInSight(); ok {
		if spots := r.beside(w); len(spots) > 0 {
			return spots[0], w, nil
		}
	}
	return stand, at, fmt.Errorf("no still water to fish in")
}

// ownBobber is its own fishing bobber, by the owner its spawn names.
func (r *robot) ownBobber() (x, y, z float64, ok bool) {
	p := r.player.Position()
	me := int32(r.player.Login.PlayerID)
	for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, 40) {
		if e.Type == "minecraft:fishing_bobber" && e.SpawnData == me {
			return e.X, e.Y, e.Z, true
		}
	}
	return 0, 0, 0, false
}

// useRod casts the rod held at the point (or pulls it in).
func (r *robot) useRod(x, y, z float64) error {
	_, err := waitUse(r, func(done func(act.Use)) { r.hands.UseItemToward(x, y, z, done) })
	return err
}

// cmdFish catches n fish (four unless said) and cooks them. Outdoor work:
// by day, and home at dusk; a fight between two casts.
func cmdFish(r *robot, args []string) (string, error) {
	n := 4
	if len(args) == 1 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v < 1 {
			return "", fmt.Errorf("fish N, not %q", args[0])
		}
		n = v
	}
	if err := r.dayWork(); err != nil {
		return "", err
	}
	if r.ui.Count("minecraft:fishing_rod") == 0 {
		if err := r.get("minecraft:fishing_rod", 1, 0); err != nil {
			return "", fmt.Errorf("a fishing rod: %w", err)
		}
	}
	stand, at, err := r.fishingSpot()
	if err != nil {
		return "", err
	}
	if err := r.goTo(stand); err != nil {
		return "", fmt.Errorf("to the water at %v: %w", at, err)
	}
	r.centre()
	start := r.fishCount()
	caught, misses := 0, 0
	for casts := 0; caught < n && casts < 6*n && misses < 6; casts++ {
		if err := r.dayWork(); err != nil {
			break // dusk: home, the fish it has cooked tomorrow
		}
		if r.defend() {
			if err := r.goTo(stand); err != nil {
				return "", fmt.Errorf("back to the water: %w", err)
			}
		}
		if _, err := r.toHotbar("minecraft:fishing_rod", true); err != nil {
			if gerr := r.get("minecraft:fishing_rod", 1, 0); gerr != nil {
				break // worn out, and no string for another
			}
			continue
		}
		bit, err := r.castAndWait(at)
		if err != nil {
			plan("fish: %v", err)
			misses++
			continue
		}
		if bit {
			_ = r.ctl.WaitTicks(r.ctx, 25) // the catch flies to it
			caught = r.fishCount() - start
		}
	}
	cooked := r.cookFish()
	events.emit("fished", map[string]any{"caught": caught, "cooked": cooked})
	return fmt.Sprintf("caught=%d cooked=%d", caught, cooked), nil
}

// castAndWait casts at the water at, waits for its bobber in the water and
// for a bite (forty-five seconds), and pulls the rod in. It reports whether a
// fish bit; an error is a cast that did not land in water.
func (r *robot) castAndWait(at world.BlockPos) (bool, error) {
	for {
		select {
		case <-r.splashes: // old splashes, another's
			continue
		default:
		}
		break
	}
	tx, ty, tz := float64(at.X)+0.5, float64(at.Y)+1.0, float64(at.Z)+0.5
	if err := r.useRod(tx, ty, tz); err != nil {
		return false, err
	}
	// the bobber flies, lands: in water within three seconds, or pulled in
	var bx, by, bz float64
	landed := false
	for t := 0; t < 60 && !landed; t++ {
		if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
			return false, err
		}
		var ok bool
		if bx, by, bz, ok = r.ownBobber(); ok {
			if s, ok := r.world.BlockAt(world.BlockPos{X: int(math.Floor(bx)), Y: int(math.Floor(by)), Z: int(math.Floor(bz))}); ok {
				if f := block.FluidOf(s); f != nil && (f.Name == "minecraft:water" || f.Name == "minecraft:flowing_water") {
					landed = true
				}
			}
		}
	}
	if !landed {
		_ = r.useRod(tx, ty, tz)
		return false, fmt.Errorf("the bobber did not land in the water at %v", at)
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case s := <-r.splashes:
			if x, y, z, ok := r.ownBobber(); ok {
				bx, by, bz = x, y, z
			}
			if math.Abs(s.x-bx) < 2 && math.Abs(s.y-by) < 2 && math.Abs(s.z-bz) < 2 {
				return true, r.useRod(tx, ty, tz)
			}
		case <-time.After(250 * time.Millisecond):
			if _, _, _, ok := r.ownBobber(); !ok {
				return false, fmt.Errorf("the bobber is gone")
			}
		}
	}
	return false, r.useRod(tx, ty, tz) // nothing bit: in, and again
}

// cookFish cooks its raw cod and salmon in its furnaces, as it smelts ore:
// it answers how many it cooked.
func (r *robot) cookFish() int {
	n := 0
	for raw, cooked := range fishFood {
		have := r.ui.Count(raw)
		if have == 0 {
			continue
		}
		before := r.ui.Count(cooked)
		if err := r.get(cooked, before+have, 0); err != nil {
			plan("fish: cook %s: %v", raw, err)
		}
		n += r.ui.Count(cooked) - before
	}
	return n
}

// canFish: a rod, or the string for one.
func (r *robot) canFish() bool {
	return r.ui.Count("minecraft:fishing_rod") > 0 || r.ui.Count("minecraft:string") >= 2
}

// wantsString: no rod and not yet the string for one — a spider's drops
// picked up after a fight, a cobweb met underground cut with the sword.
func (r *robot) wantsString() bool {
	return r.ui.Count("minecraft:fishing_rod") == 0 && r.ui.Count("minecraft:string") < 2
}
