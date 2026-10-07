// Package clock follows the game's time as the server sends it
// (ClientboundSetTime): the game time, and the world clocks of 26.x, the
// overworld's of which is the day — its ticks over 24000 the time of day,
// running at the clock's rate between the server's updates.
package clock

import (
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// DayLength is a day's ticks.
const DayLength = 24000

// Clock is the time the server last said, and when it said it.
type Clock struct {
	c        *bot.Client
	mu       sync.Mutex
	gameTime int64
	day      state // the overworld's clock
	hasDay   bool
}

type state struct {
	ticks float64 // total ticks of the clock
	rate  float64 // ticks a tick
	at    time.Time
}

// New follows the time the client's server sends.
func New(c *bot.Client) *Clock {
	k := &Clock{c: c}
	c.Events.AddListener(bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlaySetTime, F: k.onSetTime})
	return k
}

func (k *Clock) onSetTime(p pk.Packet) error {
	var st play.SetTime
	if err := p.Scan(&st); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.gameTime = int64(st.GameTime)
	for _, e := range st.ClockUpdates {
		name, ok := k.c.Registries.WorldClock.KeyOf(int32(e.Key))
		if !ok || name != "minecraft:overworld" {
			continue
		}
		k.day = state{ticks: float64(e.Val.TotalTicks) + float64(e.Val.PartialTick), rate: float64(e.Val.Rate), at: time.Now()}
		k.hasDay = true
	}
	return nil
}

// DayTicks is the overworld clock's total ticks now — what it was when the
// server said, run on at its rate (20 ticks a second) since — and whether the
// server has said.
func (k *Clock) DayTicks() (int64, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.hasDay {
		return 0, false
	}
	return int64(k.day.ticks + k.day.rate*time.Since(k.day.at).Seconds()*20), true
}

// TimeOfDay is the day's tick, 0 to 23999 (0 the sun up past dawn, 6000
// noon, 13000 the night's start, 18000 midnight).
func (k *Clock) TimeOfDay() (int64, bool) {
	t, ok := k.DayTicks()
	return ((t % DayLength) + DayLength) % DayLength, ok
}

// Phase names the time of day: "day" (0–11999), "dusk" (12000–12999, the
// monsters about to come out), "night" (13000–22999), "dawn" (23000–23999).
func Phase(t int64) string {
	switch {
	case t < 12000:
		return "day"
	case t < 13000:
		return "dusk"
	case t < 23000:
		return "night"
	}
	return "dawn"
}
