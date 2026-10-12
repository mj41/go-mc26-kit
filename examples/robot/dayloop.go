package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/clock"
)

// The day loop: with -days the robot lives by itself — the day's work and
// the night's are orders of its own (source "day") in the queue, by the
// clock, as a person plays a world day after day: the first day the tools,
// the shelter and wood; each night down its stairs and a mine; the next days
// the field, the land round home, wood, a village. A test then only judges
// what the server sees. Without -days it does what it is told, as before (the
// mc26 days and week drive it themselves).

var daysOn = flag.Bool("days", false, "live by itself: the day's and the night's work as orders of its own, by the clock (the day loop)")

// dayOne is the first day: a wooden pickaxe, the shelter (its room the first
// stone), the stone tools at once, then wood, torches and sticks, in.
var dayOne = []string{"get wooden_pickaxe", "shelter", "get stone_pickaxe", "get stone_axe", "get stone_shovel", "get stone_sword", "get logs 16", "get torch 16", "get stick 8", "in"}

// phaseNow is the phase of the day the clock says ("" not known yet).
func (r *robot) phaseNow() string {
	t, ok := r.clock.TimeOfDay()
	if !ok {
		return ""
	}
	return clock.Phase(t)
}

// housekeep is the day loop's housekeeping between its chores, by what the
// inventory watch saw: a pack nearly full tidied, iron enough made a better
// tool.
func (r *robot) housekeep() {
	if r.housekeeping {
		return
	}
	r.housekeeping = true
	defer func() { r.housekeeping = false }()
	sig := r.sigs()
	if sig.packFull {
		r.self("tidy")
	}
	if sig.betterTool {
		r.self("upgrade")
	}
	for _, kind := range sig.worn { // a spare before the only one breaks
		r.self(fmt.Sprintf("get minecraft:stone_%s %d", kind, r.countAll(tagItems("minecraft:"+kind+"s"))+1))
	}
}

// self runs an order of its own and waits for its answer, the housekeeping
// first.
func (r *robot) self(line string) (string, error) {
	r.housekeep()
	words := strings.Fields(line)
	must := strings.HasPrefix(words[0], "!")
	words[0] = strings.TrimPrefix(words[0], "!")
	cmd, ok := commands[words[0]]
	if !ok {
		return "", fmt.Errorf("no command %q", words[0])
	}
	type result struct {
		answer string
		err    error
	}
	got := make(chan result, 1)
	r.order(words, source{kind: "day"}, cmd, func(a string, err error) { got <- result{a, err} }, true)
	select {
	case res := <-got:
		if res.err != nil {
			log.Printf("robot: day: %s: %v", strings.Join(words, " "), res.err)
			// died: back for its things while they last, then home — the
			// day goes on from there
			if errors.Is(res.err, errDied) {
				r.self("recover")
				r.self("in")
			}
			if must {
				return res.answer, fmt.Errorf("%s: %w", strings.Join(words, " "), res.err)
			}
			return res.answer, nil // a chore not done is not the end of the day
		}
		log.Printf("robot: day: %s: %s", strings.Join(words, " "), res.answer)
		return res.answer, nil
	case <-r.ctx.Done():
		return "", r.ctx.Err()
	}
}

// waitPhase waits till the phase is one of want, telling what it waits for
// (the mc26 harness skips an idle night on "waiting for the morning").
func (r *robot) waitPhase(want ...string) bool {
	told := ""
	for {
		p := r.phaseNow()
		for _, w := range want {
			if p == w {
				return true
			}
		}
		if p == "night" && told != p {
			log.Printf("robot: waiting for the morning")
			told = p
		} else if p == "dusk" && told != p {
			log.Printf("robot: waiting for the night")
			told = p
		}
		select {
		case <-r.ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
}

// busy runs the rounds of chores while the phase is while — a person playing
// is on the move day and night — and waits the rest of it out when every
// chore of a round says it stays in (or stopped for the dark).
func (r *robot) busy(while string, round func(i int) []string) {
	// the dawn before the day's work (a first day that ran through the
	// night): waited out — out there it is still the night's, nothing goes out
	if while == "day" && r.phaseNow() == "dawn" {
		r.waitPhase("day")
	}
	for i := 0; i < 20; i++ {
		idle := true
		for _, c := range round(i) {
			if !inPhase(r.phaseNow(), while) {
				return
			}
			ans, err := r.self(c)
			if err != nil { // a chore the rest stands on failed: the rest of it waits
				log.Printf("robot: day: the %s's work ends: %v", while, err)
				r.waitOut(while)
				return
			}
			// staying in, stopped, or the night's work done at first light
			// ("(morning)"): nothing more to do in this phase — nor is going
			// in work done (twenty rounds of "in" and chores that stayed in
			// once ran in sixteen seconds, the day then stood out at home)
			if !strings.HasPrefix(ans, "staying in") && !strings.HasPrefix(ans, "stopped:") && !strings.Contains(ans, "(morning)") && !strings.HasPrefix(ans, "in at") {
				idle = false
			}
		}
		if idle {
			r.waitOut(while)
			return
		}
	}
}

// inPhase: the phase p is the phase while.
func inPhase(p, while string) bool { return p == while }

// waitOut waits till the phase while is over.
func (r *robot) waitOut(while string) {
	next := map[string][]string{"day": {"dusk", "night"}, "night": {"dawn", "day"}}[while]
	r.waitPhase(next...)
}

// mining is a night's round after the first: on down the stairs to the
// deepest level (the diamonds'), a mine with longer tunnels each round.
func (r *robot) mining(i int) []string {
	y := int(r.player.Position().Y)
	return []string{fmt.Sprintf("descend %d", max(y-12, -54)), fmt.Sprintf("mine %d", 24+16*i)}
}

// dayLoop is the loop: the day it is on kept in its memory (a relog, a held
// run go on from there).
func (r *robot) dayLoop() {
	if !*daysOn {
		return
	}
	// the word to begin: the one who started it (a test setting its world
	// up, a person at the console) says "live" first — orders of its own
	// from the first moment queued before theirs
	select {
	case <-r.ctx.Done():
		return
	case <-r.liveGo:
	}
	for r.phaseNow() == "" || !r.ctl.State().Loaded {
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
	day := int(r.liveDay.Load())
	if day == 0 {
		day = 1
		r.liveDay.Store(1)
		log.Printf("robot: day 1")
		for _, c := range dayOne {
			if _, err := r.self(c); err != nil {
				log.Printf("robot: day 1: %v", err)
			}
		}
	}
	for r.ctx.Err() == nil {
		// what is left of the day worked through (a day behind the clock: a
		// relog, a held run), then the evening: smelted at home, the night
		r.busy("day", func(int) []string { return []string{"out", "harvest", "get logs +16", "in"} })
		r.self("smeltall")
		if !r.waitPhase("night") {
			return
		}
		night := day
		log.Printf("robot: night %d", night)
		r.busy("night", func(i int) []string {
			switch {
			case i == 0 && night == 1:
				return []string{"!get stone_pickaxe 4", "!descend 16", "mine"}
			case i == 0 && night == 2:
				return []string{"!down", "!get iron_pickaxe", "!descend -54", "mine"}
			case i == 0:
				return []string{"down", "mine"}
			}
			return r.mining(i)
		})
		r.self("smeltall") // the night's ore, before it goes up
		if !r.waitPhase("day") {
			return
		}
		day++
		r.liveDay.Store(int32(day))
		log.Printf("robot: day %d", day)
		r.self("in") // up its stairs home
		r.self("out")
		if day == 2 {
			r.self("farm")
			r.self("scout 48")
		}
		r.busy("day", func(i int) []string {
			switch {
			case day == 2:
				return []string{fmt.Sprintf("get logs %d", 16+16*i)}
			case i == 0 && r.field == nil && r.wild == nil:
				// no field yet (the farm stopped by the dusk): the field first,
				// each day till there is one
				return []string{"farm", "harvest", fmt.Sprintf("scout %d", 48+16*(day-2)), "get logs +16"}
			case i == 0 && day >= 5:
				return []string{"harvest", "village 50", "tradeall", "in"}
			case i == 0:
				return []string{"harvest", fmt.Sprintf("scout %d", 48+16*(day-2)), "get logs +16"}
			}
			return []string{"harvest", "get logs +16"}
		})
		r.self("in")
	}
}

// cmdLive begins the day loop (-days): from now on it lives by itself.
func cmdLive(r *robot, _ []string) (string, error) {
	if !*daysOn {
		return "", fmt.Errorf("not started with -days")
	}
	r.liveOnce.Do(func() { close(r.liveGo) })
	return fmt.Sprintf("living by itself from day %d", max(1, int(r.liveDay.Load()))), nil
}
