package main

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// A tree is felled whole, as a person does: no crown left floating over
// the land. The logs from the bottom up; one out of reach is cut from a
// pillar in the felled trunk's place — a jump, a block of dirt put under the
// feet at its top, again, the leaves over the head cut first — and the
// pillar dug away after. The leaves left without a log fall of themselves.

// treeLogs are the logs of the tree the log at p belongs to: those joined to
// it (sides, edges, corners: a branch), up from it, within six blocks of it.
func (r *robot) treeLogs(p world.BlockPos) []world.BlockPos {
	seen := map[world.BlockPos]bool{p: true}
	queue := []world.BlockPos{p}
	var out []world.BlockPos
	for len(queue) > 0 && len(out) < 200 {
		q := queue[0]
		queue = queue[1:]
		if r.logAt(q) {
			out = append(out, q)
		}
		for dx := -1; dx <= 1; dx++ {
			for dy := 0; dy <= 1; dy++ {
				for dz := -1; dz <= 1; dz++ {
					n := world.BlockPos{X: q.X + dx, Y: q.Y + dy, Z: q.Z + dz}
					if seen[n] || abs(n.X-p.X) > 6 || abs(n.Z-p.Z) > 6 || n.Y-p.Y > 30 {
						continue
					}
					seen[n] = true
					if r.logAt(n) {
						queue = append(queue, n)
					}
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Y < out[j].Y })
	return out
}

// eyeReaches: the block at p within its reach from where it stands.
func (r *robot) eyeReaches(p world.BlockPos) bool {
	e := r.player.Position()
	dx, dy, dz := float64(p.X)+0.5-e.X, float64(p.Y)+0.5-(e.Y+1.62), float64(p.Z)+0.5-e.Z
	return math.Sqrt(dx*dx+dy*dy+dz*dz) <= 4.2
}

// fellTree fells the tree the log at p belongs to; it answers how many logs it cut.
func (r *robot) fellTree(p world.BlockPos) int {
	logs := r.treeLogs(p)
	cut := 0
	dig := func(l world.BlockPos) bool {
		if !r.logAt(l) {
			return false
		}
		s, _ := r.world.BlockAt(l)
		_ = r.holdBestTool(s)
		if _, err := cmdDig(r, []string{strconv.Itoa(l.X), strconv.Itoa(l.Y), strconv.Itoa(l.Z)}); err != nil {
			plan("tree at %v: %v", l, err)
			return false
		}
		cut++
		return true
	}
	for _, l := range logs {
		if !r.logAt(l) {
			continue
		}
		r.defend()
		if r.reachGround(l) == nil {
			dig(l)
			continue
		}
		// out of reach from the ground: up a pillar in the trunk's place
		n, err := r.climbTo(l)
		if err != nil {
			plan("tree at %v: %v", l, err)
		}
		for _, o := range logs { // all it reaches from up there
			if r.logAt(o) && r.eyeReaches(o) {
				dig(o)
			}
		}
		r.pillarDown(l, n)
	}
	if cut > 0 {
		_, _ = cmdCollect(r, []string{"8"})
	}
	return cut
}

// climbTo pillars up from the column under the log l (the trunk's place,
// felled below it) till l is in reach; it answers how high it built.
func (r *robot) climbTo(l world.BlockPos) (int, error) {
	// the ground in the log's column: the first place to stand under it on
	// the ground — not on the tree's own leaves or a log (a branch over its
	// crown): the pillar goes up through the leaves, cutting them
	stand, found := world.BlockPos{}, false
	for y := l.Y - 1; y > l.Y-24; y-- {
		q := world.BlockPos{X: l.X, Y: y, Z: l.Z}
		if !r.standable(q) {
			continue
		}
		if s, ok := r.world.BlockAt(world.BlockPos{X: q.X, Y: q.Y - 1, Z: q.Z}); ok && vegetation(blockID(s)) {
			continue
		}
		stand, found = q, true
		break
	}
	if !found {
		return 0, fmt.Errorf("no ground under %v to build up from", l)
	}
	// the blocks for the whole pillar first: fetched on the way up, the walk
	// for them would take it off its pillar
	need := l.Y - stand.Y - 2
	if have := r.ui.Count("minecraft:dirt") + r.ui.Count("minecraft:cobblestone"); have < need {
		if err := r.get("minecraft:dirt", r.ui.Count("minecraft:dirt")+need-have, 1); err != nil {
			return 0, fmt.Errorf("blocks for a pillar of %d: %w", need, err)
		}
	}
	if err := r.goTo(stand); err != nil {
		return 0, fmt.Errorf("to %v under it: %w", stand, err)
	}
	r.centre()
	n := 0
	for !r.eyeReaches(l) && n < 10 {
		if err := r.pillarUp(); err != nil {
			return n, err
		}
		n++
	}
	if !r.eyeReaches(l) {
		return n, fmt.Errorf("%v still out of reach %d up", l, n)
	}
	return n, nil
}

// pillarUp raises it a block where it stands: the block over its head cut
// (leaves), a jump, and at its top a block put where the feet were.
func (r *robot) pillarUp() error {
	p := r.player.Position()
	feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	over := world.BlockPos{X: feet.X, Y: feet.Y + 2, Z: feet.Z}
	if !r.openAt(over) {
		s, _ := r.world.BlockAt(over)
		if !vegetation(blockID(s)) {
			return fmt.Errorf("no room over its head at %v", over)
		}
		_ = r.holdBestTool(s)
		if _, err := cmdDig(r, []string{strconv.Itoa(over.X), strconv.Itoa(over.Y), strconv.Itoa(over.Z)}); err != nil {
			return fmt.Errorf("the leaves over its head: %w", err)
		}
	}
	block := ""
	for _, b := range []string{"minecraft:dirt", "minecraft:cobblestone"} {
		if r.ui.Count(b) > 0 {
			block = b
			break
		}
	}
	if block == "" {
		return errors.New("no block left to stand on") // fetched before the climb (climbTo), never on the way up
	}
	if _, err := r.toHotbar(block, true); err != nil {
		return err
	}
	r.walker.Stop()
	r.ctl.SetInput(control.Input{Jump: true})
	for t := 0; t < 12 && r.player.Position().Y < float64(feet.Y)+1.05; t++ {
		if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
			r.ctl.SetInput(control.Input{})
			return err
		}
	}
	r.ctl.SetInput(control.Input{})
	_, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(feet, done) })
	for t := 0; t < 20 && !r.ctl.State().OnGround; t++ {
		_ = r.ctl.WaitTicks(r.ctx, 1)
	}
	if err != nil {
		return fmt.Errorf("a block under its feet: %w", err)
	}
	if r.player.Position().Y < float64(feet.Y)+0.9 {
		return fmt.Errorf("the block under its feet at %v did not hold", feet)
	}
	return nil
}

// pillarDown digs away the n blocks of the pillar it stands on in the log
// l's column, landing on each next one down — only standing on it: off it
// (pushed, a fight), nothing is dug where it stands.
func (r *robot) pillarDown(l world.BlockPos, n int) {
	for i := 0; i < n; i++ {
		p := r.player.Position()
		under := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y+1e-6)) - 1, Z: int(math.Floor(p.Z))}
		if under.X != l.X || under.Z != l.Z {
			plan("down the pillar: not on it (at %v): left", under)
			return
		}
		s, _ := r.world.BlockAt(under)
		_ = r.holdBestTool(s)
		if _, err := cmdDig(r, []string{strconv.Itoa(under.X), strconv.Itoa(under.Y), strconv.Itoa(under.Z)}); err != nil {
			plan("down the pillar at %v: %v", under, err)
			return
		}
		for t := 0; t < 20; t++ {
			_ = r.ctl.WaitTicks(r.ctx, 1)
			if r.ctl.State().OnGround && r.player.Position().Y < float64(under.Y)+0.5 {
				break
			}
		}
	}
	_, _ = cmdCollect(r, []string{"4"})
}

// blockID is the block state's block's name ("" unknown).
func blockID(s block.StateID) string {
	if int(s) < 0 || int(s) >= len(block.StateList) {
		return ""
	}
	return block.StateList[s].ID()
}
