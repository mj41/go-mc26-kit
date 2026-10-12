// Package ticks collects, tick by tick, what the client was told that a
// watcher would draw as the game's client draws it: where each entity was
// sent (the path a move takes, each step with its ticks), which came and
// went, whose arm swung, who was hurt or began to die, what was picked up,
// what exploded, how fast thrown things fly — the packets a renderer acts on,
// gathered between two ticks and taken at the end of each (Take). A bot that
// writes them down lets a replay move everything as the client does: its
// own position each tick, the others interpolated to theirs over the ticks
// the client takes (26.3: SteppedInterpolationHandler for living entities,
// LinearInterpolationHandler's three for boats and minecarts).
package ticks

import (
	"math"
	"sync"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// Tick is what one tick brought. Positions are absolute, as the client holds
// them after the packet.
type Tick struct {
	// Ents: per step of a move, [id, x, y, z, yaw, pitch, head yaw, ticks]
	// (ticks 0: the entity type's own update interval); a turn only (no
	// move) is one entry at its position
	Ents    [][8]float64
	New     []entities.Entity // came into tracking (add_entity)
	Gone    []int32           // removed (remove_entities)
	Swing   []int32           // its arm swung (animate: main or off hand)
	Hurt    []int32           // took damage (hurt_animation, damage_event)
	Died    []int32           // began to die (entity_event 3)
	Pickup  [][3]int32        // take_item_entity: item, by, count
	Explode [][4]float64      // explode: x, y, z, radius
	Motion  [][4]float64      // set_entity_motion: id, velocity (blocks a tick)
}

// Empty: nothing came this tick.
func (t *Tick) Empty() bool {
	return len(t.Ents)+len(t.New)+len(t.Gone)+len(t.Swing)+len(t.Hurt)+len(t.Died)+len(t.Pickup)+len(t.Explode)+len(t.Motion) == 0
}

// Collector gathers a tick's packets; Take hands them over and starts anew.
type Collector struct {
	mu  sync.Mutex
	cur Tick
	// Near, when set, says whether an entity at x, y, z is worth telling
	// (within the watcher's range); unset: all
	Near func(x, y, z float64) bool
}

// New starts collecting from c (its own packet handlers) and m (its moves,
// arrivals, departures, pick-ups, velocities: m.Watch).
func New(c *bot.Client, m *entities.Manager) *Collector {
	k := &Collector{}
	m.Watch(entities.Events{
		Added: func(e entities.Entity) {
			if k.near(e.X, e.Y, e.Z) {
				k.with(func(t *Tick) { t.New = append(t.New, e) })
			}
		},
		Removed: func(e entities.Entity) { k.with(func(t *Tick) { t.Gone = append(t.Gone, e.ID) }) },
		PickedUp: func(item, by int32, n int) {
			k.with(func(t *Tick) { t.Pickup = append(t.Pickup, [3]int32{item, by, int32(n)}) })
		},
		Moved: func(e entities.Entity, path []entities.Step) {
			if !k.near(e.X, e.Y, e.Z) {
				return
			}
			k.with(func(t *Tick) {
				if len(path) == 0 { // a turn: where it is
					path = []entities.Step{{X: e.X, Y: e.Y, Z: e.Z}}
				}
				for _, s := range path {
					t.Ents = append(t.Ents, [8]float64{float64(e.ID), r2(s.X), r2(s.Y), r2(s.Z), r1(float64(e.Yaw)), r1(float64(e.Pitch)), r1(float64(e.HeadYaw)), float64(s.Ticks)})
				}
			})
		},
		Motion: func(e entities.Entity) {
			if k.near(e.X, e.Y, e.Z) {
				k.with(func(t *Tick) { t.Motion = append(t.Motion, [4]float64{float64(e.ID), r3(e.VX), r3(e.VY), r3(e.VZ)}) })
			}
		},
	})
	// after the manager's own handlers (priority 0): the entity as it is now
	c.Events.AddListener(
		bot.PacketHandler{Priority: -1, ID: packetid.ClientboundPlayAnimate, F: func(p pk.Packet) error {
			var a play.Animate
			if p.Scan(&a) == nil && (a.Action == 0 || a.Action == 3) { // SWING_MAIN_HAND, SWING_OFF_HAND
				k.with(func(t *Tick) { t.Swing = append(t.Swing, int32(a.ID)) })
			}
			return nil
		}},
		bot.PacketHandler{Priority: -1, ID: packetid.ClientboundPlayHurtAnimation, F: func(p pk.Packet) error {
			var h play.HurtAnimation
			if p.Scan(&h) == nil {
				k.hurt(int32(h.ID))
			}
			return nil
		}},
		bot.PacketHandler{Priority: -1, ID: packetid.ClientboundPlayDamageEvent, F: func(p pk.Packet) error {
			var d play.DamageEvent
			if p.Scan(&d) == nil {
				k.hurt(int32(d.EntityID))
			}
			return nil
		}},
		bot.PacketHandler{Priority: -1, ID: packetid.ClientboundPlayEntityEvent, F: func(p pk.Packet) error {
			var e play.EntityEvent
			if p.Scan(&e) == nil && e.EventID == 3 { // LivingEntity: its death
				k.with(func(t *Tick) { t.Died = append(t.Died, int32(e.EntityID)) })
			}
			return nil
		}},
		bot.PacketHandler{Priority: -1, ID: packetid.ClientboundPlayExplode, F: func(p pk.Packet) error {
			var x play.Explode
			if p.Scan(&x) == nil {
				k.with(func(t *Tick) {
					t.Explode = append(t.Explode, [4]float64{r2(float64(x.Center.X)), r2(float64(x.Center.Y)), r2(float64(x.Center.Z)), r2(float64(x.Radius))})
				})
			}
			return nil
		}},
	)
	return k
}

// Swung notes an arm's swing the bot knows of but the server does not send
// it (its own: a client is not sent its own swings)
func (k *Collector) Swung(id int32) { k.with(func(t *Tick) { t.Swing = append(t.Swing, id) }) }

// Take hands over what came since the last Take and starts a new tick.
func (k *Collector) Take() Tick {
	k.mu.Lock()
	defer k.mu.Unlock()
	t := k.cur
	k.cur = Tick{}
	return t
}

func (k *Collector) with(f func(t *Tick)) {
	k.mu.Lock()
	f(&k.cur)
	k.mu.Unlock()
}

// hurt notes id hurt once a tick (both packets come for one hit)
func (k *Collector) hurt(id int32) {
	k.with(func(t *Tick) {
		for _, h := range t.Hurt {
			if h == id {
				return
			}
		}
		t.Hurt = append(t.Hurt, id)
	})
}

func (k *Collector) near(x, y, z float64) bool { return k.Near == nil || k.Near(x, y, z) }

func r1(v float64) float64 { return math.Round(v*10) / 10 }
func r2(v float64) float64 { return math.Round(v*100) / 100 }
func r3(v float64) float64 { return math.Round(v*1000) / 1000 }
