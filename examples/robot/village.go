package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/types"
)

// A village: where villagers live — a bell at its middle, beds, the
// villagers about. The robot marks one on its map when it sees it (a bell, a
// villager), goes looking for one by day (village), and trades there (trade
// all): what it has much of — coal, wheat, sticks — for emeralds, emeralds
// for bread.

// villagerJob is a villager's profession (minecraft:farmer…), from its
// entity data, and whether it trades: a grown one with a job (not none, not a
// nitwit — they shake their heads).
func villagerJob(e entities.Entity) (string, bool) {
	if v, ok := e.Data[entitydata.AgeableMobBaby]; ok {
		if b, ok := v.Value.(*pk.Boolean); ok && bool(*b) {
			return "baby", false
		}
	}
	v, ok := e.Data[entitydata.VillagerVillagerData]
	if !ok {
		// not sent: the default (plains, no profession, level one) — the
		// server sends only what differs; a villager without a job yet
		return "minecraft:none", false
	}
	var prof int
	switch d := v.Value.(type) {
	case *types.VillagerData:
		prof = int(d.Profession)
	default:
		return fmt.Sprintf("data %T", v.Value), true
	}
	if prof < 0 || prof >= len(registryid.VillagerProfession) {
		return fmt.Sprintf("profession #%d", prof), true
	}
	name := registryid.VillagerProfession[prof]
	return name, name != "minecraft:none" && name != "minecraft:nitwit"
}

// isVillager: a villager, by its type.
func isVillager(t string) bool { return t == "minecraft:villager" }

// villagersNear are the villagers within radius of it, the nearest first.
func (r *robot) villagersNear(radius float64) []entities.Entity {
	p := r.player.Position()
	var out []entities.Entity
	for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, radius) {
		if isVillager(e.Type) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return math.Hypot(out[i].X-p.X, out[i].Z-p.Z) < math.Hypot(out[j].X-p.X, out[j].Z-p.Z)
	})
	return out
}

// markVillagers marks the areas the villagers in sight stand in as a
// village's.
func (r *robot) markVillagers() {
	for _, e := range r.villagersNear(learnRadius) {
		k := keyOf(int(math.Floor(e.X)), int(math.Floor(e.Z)))
		a, _ := r.atlas.get(k)
		a.Village++
		if a.SeenAt == 0 {
			a.Y, a.SeenAt = int(math.Floor(e.Y))-1, time.Now().Unix()
		}
		r.atlas.put(k, a)
	}
}

// cmdVillage goes to a village: the one it knows (its map), or one it finds
// looking — leg after leg of its search (explore), legs at most (40) — and
// answers where it is and how many villagers are about.
func cmdVillage(r *robot, args []string) (string, error) {
	if h := r.player.Status().Health; h < 10 && r.countAll(foods) == 0 {
		return fmt.Sprintf("staying in: health %.1f and nothing to eat", h), nil // no walk far, hurt and not healing
	}
	legs := 40
	if len(args) == 1 {
		v, err := strconv.Atoi(args[0])
		if err != nil {
			return "", err
		}
		legs = v
	}
	village := func(a area) bool { return a.Village > 0 }
	for try := 0; try <= legs; try++ {
		if err := r.dayWork(); err != nil { // after dark: not out (in, it stays in)
			return "", err
		}
		r.markVillagers()
		if vs := r.villagersNear(48); len(vs) > 0 {
			e := vs[0]
			if math.Hypot(e.X-r.player.Position().X, e.Z-r.player.Position().Z) > 8 {
				r.goToKnown("village", village)
			}
			p := r.player.Position()
			return fmt.Sprintf("village at %.0f %.0f %.0f, %d villagers about", p.X, p.Y, p.Z, len(r.villagersNear(48))), nil
		}
		if _, ok := r.nearestKnown(village); ok && try == 0 {
			if r.goToKnown("village", village) {
				continue
			}
		}
		if try == legs {
			break
		}
		if err := r.explore(try); err != nil {
			plan("village: %v", err)
		}
		r.learn()
	}
	return "", fmt.Errorf("%w: no village found in %d legs", errNoWay, legs)
}

// cmdTradeAll trades with the villagers about (within 24): each one's offers
// read, and those it can pay for made — first what gives emeralds (its coal,
// wheat, sticks: a few times each), then emeralds for food. Every offer seen
// is written down (the merchant's packets are what this exercises). It
// answers how many trades with how many villagers, and the emeralds it has.
func cmdTradeAll(r *robot, _ []string) (string, error) {
	var vs []entities.Entity
	for _, v := range r.villagersNear(48) {
		job, ok := villagerJob(v)
		plan("trade: villager %d at %.0f %.0f %.0f: %s", v.ID, v.X, v.Y, v.Z, job)
		if ok {
			vs = append(vs, v)
		}
	}
	if len(vs) == 0 {
		return "traded 0 times: no villager with a trade about", nil // none near: an answer, not a failure
	}
	trades, with := 0, 0
	for i, v := range vs {
		if i == 8 {
			break
		}
		id := strconv.Itoa(int(v.ID))
		if _, err := cmdInteract(r, []string{id}); err != nil {
			plan("trade: villager %s: %v", id, err)
			continue
		}
		var offers int
		for t := 0; t < 40; t++ {
			if m, ok := r.screens.Open(); ok && len(m.Offers) > 0 {
				offers = len(m.Offers)
				break
			}
			if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
				return "", err
			}
		}
		m, ok := r.screens.Open()
		if !ok || offers == 0 {
			plan("trade: villager %s: no offers (a villager with no trade yet?)", id)
			r.closeScreen()
			continue
		}
		with++
		// the emerald-giving first, then the emerald-taking
		order := make([]int, len(m.Offers))
		for k := range order {
			order[k] = k
		}
		sell := func(k int) bool { return itemName(int32(m.Offers[k].Sell.Item)) == "minecraft:emerald" }
		sort.SliceStable(order, func(a, b int) bool { return sell(order[a]) && !sell(order[b]) })
		for _, k := range order {
			o := m.Offers[k]
			pay, n := itemName(int32(o.Buy.Item)), int(o.Buy.Count)
			got := itemName(int32(o.Sell.Item))
			desc := fmt.Sprintf("%d %s", n, strings.TrimPrefix(pay, "minecraft:"))
			payB, nB := "", 0
			if o.BuyB.Has {
				payB, nB = itemName(int32(o.BuyB.Val.Item)), int(o.BuyB.Val.Count)
				desc += fmt.Sprintf(" + %d %s", nB, strings.TrimPrefix(payB, "minecraft:"))
			}
			plan("trade: villager %s offer %d: %s for %d %s (used %d of %d)", id, k, desc, o.Sell.Count, strings.TrimPrefix(got, "minecraft:"), o.Uses, o.MaxUses)
			if bool(o.IsExhausted) || n <= 0 {
				continue
			}
			food := false
			for _, f := range foods {
				if f == got {
					food = true
				}
			}
			if got != "minecraft:emerald" && !food {
				continue // nothing it wants
			}
			// how many it can pay for, counted before the first: the server
			// moves the payment into the menu's slots once a trade is chosen
			want := min(4, r.ui.Count(pay)/n, int(o.MaxUses-o.Uses))
			if payB != "" && nB > 0 {
				want = min(want, r.ui.Count(payB)/nB)
			}
			if want <= 0 {
				continue
			}
			made, err := r.ui.Trade(r.ctx, k, want)
			if err != nil {
				plan("trade: villager %s offer %d: made %d of %d: %v", id, k, made, want, err)
			}
			trades += made
		}
		r.closeScreen()
	}
	return fmt.Sprintf("traded %d times with %d villagers, emeralds %d", trades, with, r.ui.Count("minecraft:emerald")), nil
}
