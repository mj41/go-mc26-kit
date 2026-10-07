package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/path"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entity"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/types"
)

// command is one robot command: it gets the words after its name and returns
// the answer, printed after "robot: <name> ".
type command func(r *robot, args []string) (string, error)

var commands = map[string]command{
	"pos":       cmdPos,
	"state":     cmdState,
	"look":      cmdLook,
	"block":     cmdBlock,
	"keys":      cmdKeys,
	"wait":      cmdWait,
	"inv":       cmdInv,
	"hold":      cmdHold,
	"status":    cmdStatus,
	"nearby":    cmdNearby,
	"entities":  cmdEntities,
	"seen":      cmdSeen,
	"goto":      cmdGoto,
	"follow":    cmdFollow,
	"come":      cmdCome,
	"find":      cmdFind,
	"around":    cmdAround,
	"light":     cmdLight,
	"descend":   cmdDescend,
	"shelter":   cmdShelter,
	"mine":      cmdMine,
	"time":      cmdTime,
	"hunt":      cmdHunt,
	"cook":      cmdCook,
	"out":       cmdOut,
	"in":        cmdIn,
	"farm":      cmdFarm,
	"harvest":   cmdHarvest,
	"fish":      cmdFish,
	"water":     cmdWater,
	"scout":     cmdScout,
	"smeltall":  cmdSmeltAll,
	"hide":      cmdHide,
	"village":   cmdVillage,
	"tradeall":  cmdTradeAll,
	"recover":   cmdRecover,
	"down":      cmdDown,
	"useon":     cmdUseOn,
	"usetoward": cmdUseToward,
	"dismount":  cmdDismount,
	"stop":      cmdStop,
	"dig":       cmdDig,
	"collect":   cmdCollect,
	"place":     cmdPlace,
	"use":       cmdUse,
	"eat":       cmdEat,
	"open":      cmdOpen,
	"close":     cmdClose,
	"craft":     cmdCraft,
	"store":     cmdStore,
	"take":      cmdTake,
	"smelt":     cmdSmelt,
	"guard":     cmdGuard,
	"get":       cmdGet,
	"build":     cmdBuild,
	"recipe":    cmdRecipe,
	"click":     cmdClick,
	"updates":   cmdUpdates,
	"sign":      cmdSign,
	"interact":  cmdInteract,
	"trade":     cmdTrade,
}

// console reads commands from in until it ends or a quit, then calls stop.
func (r *robot) console(in io.Reader, stop func()) {
	defer stop()
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
			continue
		case line == "quit":
			r.client.Close()
			return
		case strings.HasPrefix(line, "/"):
			if err := r.chat.SendCommand(line[1:]); err != nil {
				log.Printf("robot: error /%s: %v", line[1:], err)
			}
			continue
		case strings.HasPrefix(line, "say "):
			if err := r.chat.SendMessage(strings.TrimPrefix(line, "say ")); err != nil {
				log.Printf("robot: error say: %v", err)
			}
			continue
		}
		words := strings.Fields(line)
		cmd, ok := commands[words[0]]
		if !ok {
			log.Printf("robot: error unknown command %q", words[0])
			continue
		}
		answer, err := runOrder(r, cmd, words)
		if err != nil {
			log.Printf("robot: error %s: %v", words[0], err)
			continue
		}
		log.Printf("robot: %s %s", words[0], answer)
	}
}

func cmdPos(r *robot, _ []string) (string, error) {
	p := r.player.Position()
	return fmt.Sprintf("%.4f %.4f %.4f %.2f %.2f", p.X, p.Y, p.Z, p.Yaw, p.Pitch), nil
}

func cmdState(r *robot, _ []string) (string, error) {
	s := r.ctl.State()
	riding := "-"
	if v, _, ok := r.entities.Vehicle(int32(r.player.Login.PlayerID)); ok {
		riding = fmt.Sprintf("%d:%s", v.ID, v.Type)
		if b, ok := r.rider.Boat(); ok {
			riding += fmt.Sprintf(":%.4f,%.4f,%.4f,%.2f", b.Pos.X, b.Pos.Y, b.Pos.Z, b.Yaw)
		}
	}
	riding += fmt.Sprintf(" corrections=%d", r.rider.CorrectionCount())
	return fmt.Sprintf("loaded=%v onGround=%v sprinting=%v ticks=%d teleports=%d dim=%s riding=%s", s.Loaded, s.OnGround, s.Sprinting, s.Ticks, s.Teleports, r.player.Spawn.Dimension, riding), nil
}

// cmdKeys holds keys for a number of ticks, then lets go of them, or holds
// the keys of the third argument instead — a person going from forward and
// shift to shift alone does not let go of shift in between.
func cmdKeys(r *robot, args []string) (string, error) {
	if len(args) != 2 && len(args) != 3 {
		return "", fmt.Errorf("want: keys <forward,back,left,right,jump,shift,sprint|none> <ticks> [<keys held after>]")
	}
	in, err := parseKeys(args[0])
	if err != nil {
		return "", err
	}
	then := control.Input{}
	if len(args) == 3 {
		if then, err = parseKeys(args[2]); err != nil {
			return "", err
		}
	}
	ticks, err := strconv.Atoi(args[1])
	if err != nil || ticks < 0 {
		return "", fmt.Errorf("want a number of ticks: %v", err)
	}
	r.ctl.SetInput(in)
	err = r.ctl.WaitTicks(r.ctx, ticks)
	r.ctl.SetInput(then)
	if err != nil {
		return "", err
	}
	p, s := r.player.Position(), r.ctl.State()
	return fmt.Sprintf("%.4f %.4f %.4f onGround=%v", p.X, p.Y, p.Z, s.OnGround), nil
}

func parseKeys(list string) (control.Input, error) {
	var in control.Input
	for _, k := range strings.Split(list, ",") {
		switch k {
		case "forward":
			in.Forward = true
		case "back":
			in.Backward = true
		case "left":
			in.Left = true
		case "right":
			in.Right = true
		case "jump":
			in.Jump = true
		case "shift":
			in.Shift = true
		case "sprint":
			in.Sprint = true
		case "none":
		default:
			return in, fmt.Errorf("unknown key %q", k)
		}
	}
	return in, nil
}

func cmdWait(r *robot, args []string) (string, error) {
	ticks := 20
	if len(args) == 1 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return "", err
		}
		ticks = n
	}
	if err := r.ctl.WaitTicks(r.ctx, ticks); err != nil {
		return "", err
	}
	return strconv.Itoa(ticks), nil
}

func cmdLook(r *robot, args []string) (string, error) {
	if len(args) != 2 {
		return "", fmt.Errorf("want: look <yaw> <pitch>")
	}
	yaw, err1 := strconv.ParseFloat(args[0], 32)
	pitch, err2 := strconv.ParseFloat(args[1], 32)
	if err1 != nil || err2 != nil {
		return "", fmt.Errorf("want numbers: look <yaw> <pitch>")
	}
	r.ctl.Look(float32(yaw), float32(pitch))
	return fmt.Sprintf("%.2f %.2f", yaw, pitch), nil
}

func cmdBlock(r *robot, args []string) (string, error) {
	pos, err := blockPos(args)
	if err != nil {
		return "", err
	}
	state, ok := r.world.BlockAt(pos)
	if !ok {
		return "", fmt.Errorf("%v is not loaded", pos)
	}
	return world.StateString(state), nil
}

// blockPos reads three integers: x y z.
func blockPos(args []string) (world.BlockPos, error) {
	if len(args) != 3 {
		return world.BlockPos{}, fmt.Errorf("want x y z")
	}
	var v [3]int
	for i, a := range args {
		n, err := strconv.Atoi(a)
		if err != nil {
			return world.BlockPos{}, fmt.Errorf("want integers x y z: %v", err)
		}
		v[i] = n
	}
	return world.BlockPos{X: v[0], Y: v[1], Z: v[2]}, nil
}

func itemName(id int32) string {
	if id >= 0 && int(id) < len(registryid.Item) {
		return registryid.Item[id]
	}
	return fmt.Sprintf("item#%d", id)
}

func cmdInv(r *robot, _ []string) (string, error) {
	r.screens.Lock()
	defer r.screens.Unlock()
	parts := []string{fmt.Sprintf("held=%d", r.screens.HeldSlot)}
	for i := 0; i <= 40; i++ {
		slot, ok := screen.MenuSlot(i)
		if !ok {
			continue
		}
		if s := r.screens.Inventory.Slots[slot]; s.Count > 0 {
			parts = append(parts, fmt.Sprintf("%d=%s*%d", i, itemName(int32(s.Item)), s.Count))
		}
	}
	return strings.Join(parts, " "), nil
}

func cmdHold(r *robot, args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("want: hold <0-8>")
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		return "", err
	}
	if err := r.screens.SelectHotbar(n); err != nil {
		return "", err
	}
	return strconv.Itoa(n), nil
}

func cmdStatus(r *robot, _ []string) (string, error) {
	s := r.player.Status()
	var effects []string
	for name, e := range s.Effects {
		effects = append(effects, fmt.Sprintf("%s:%d:%d", name, e.Amplifier, e.Duration))
	}
	sort.Strings(effects)
	return fmt.Sprintf("health=%g food=%d saturation=%g xp=%d effects=%s", s.Health, s.Food, s.Saturation, s.XPLevel, strings.Join(effects, ",")), nil
}

func cmdNearby(r *robot, args []string) (string, error) {
	radius := 16.0
	if len(args) == 1 {
		v, err := strconv.ParseFloat(args[0], 64)
		if err != nil {
			return "", err
		}
		radius = v
	}
	p := r.player.Position()
	var parts []string
	for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, radius) {
		parts = append(parts, fmt.Sprintf("%d %s %.4f %.4f %.4f", e.ID, e.Type, e.X, e.Y, e.Z))
	}
	return strings.Join(parts, "; "), nil
}

// cmdEntities lists the tracked entities within a radius (16 by default):
// id, type, position, the custom name in their data, how many data values and
// equipment slots the server sent, and the data values the entity type's
// layout (data/entitydata) does not have at that index with that serializer.
func cmdEntities(r *robot, args []string) (string, error) {
	radius := 16.0
	if len(args) == 1 {
		v, err := strconv.ParseFloat(args[0], 64)
		if err != nil {
			return "", err
		}
		radius = v
	}
	p := r.player.Position()
	var parts []string
	for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, radius) {
		layout := map[int]int{}
		for _, f := range entitydata.Fields[e.Type] {
			layout[f.Index] = f.Serializer
		}
		name := ""
		var bad []string
		for _, v := range e.Data {
			if s, ok := layout[int(v.Index)]; !ok || s != int(v.Serializer) {
				bad = append(bad, fmt.Sprintf("%d:%d", v.Index, v.Serializer))
			}
			if v.Index == entitydata.EntityCustomName {
				if n, ok := v.Value.(*pk.Option[types.Text, *types.Text]); ok && bool(n.Has) {
					name = n.Val.ClearString()
				}
			}
		}
		sort.Strings(bad)
		parts = append(parts, fmt.Sprintf("%d %s %.2f %.2f %.2f name=%q data=%d equip=%d bad=%s",
			e.ID, e.Type, e.X, e.Y, e.Z, name, len(e.Data), len(e.Equipment), strings.Join(bad, ",")))
	}
	return strings.Join(parts, "; "), nil
}

// cmdSeen lists the types of the entities the server added since the last
// "seen reset", with how many of each.
func cmdSeen(r *robot, args []string) (string, error) {
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	if len(args) == 1 && args[0] == "reset" {
		r.seen = map[string]int{}
		return "reset", nil
	}
	var parts []string
	for t, n := range r.seen {
		parts = append(parts, fmt.Sprintf("%s=%d", t, n))
	}
	sort.Strings(parts)
	return strings.Join(parts, " "), nil
}

func cmdGoto(r *robot, args []string) (string, error) {
	goal, err := blockPos(args)
	if err != nil {
		return "", err
	}
	// attacked: fought first — a walk that fails at once (shut in a hole)
	// never gets to the checks on the way
	r.defend()
	// far: in legs, as a person heads for a hill on the horizon — one search
	// over a hundred blocks of rough land runs out of places to look at
	if !r.legging {
		if err := r.legsToward(goal); err != nil {
			plan("goto: %v", err)
		}
	}
	// the cells on a tree it stood in on the way: a way over the trees is
	// for where the ground has none, and said
	overTree := map[world.BlockPos]bool{}
	// the ground it last stood on: a drop of more than three under it is a
	// fall (off an edge, pushed), the path never drops more
	var top world.BlockPos
	grounded, falls, fights := false, 0, 0
	defer func() {
		if len(overTree) > 0 {
			plan("goto %v: over a tree, %d cells", goal, len(overTree))
		}
	}()
	var st string
	for try := 0; ; try++ {
		r.walker.Goto(goal)
		deadline := time.Now().Add(2 * time.Minute)
		for t := 0; r.walker.Status() == "walking" && time.Now().Before(deadline); t++ {
			if err := r.ctl.WaitTicks(r.ctx, 5); err != nil {
				return "", err
			}
			if p := r.player.Position(); r.ctl.State().OnGround {
				if n, ok := r.walker.Graph.Start(p.X, p.Y, p.Z); ok && r.walker.Graph.OnTree(n) {
					overTree[n.Pos] = true
				}
				cell := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
				// a fall goes down, not across (a death's respawn, a teleport,
				// is no fall: far off at once)
				near := abs(top.X-cell.X) <= 4 && abs(top.Z-cell.Z) <= 4
				if grounded && near && top.Y-cell.Y > 3 && !r.climbing && falls < 2 {
					falls++
					r.walker.Stop()
					r.backUp(top, goal)
					r.walker.Goto(goal)
					deadline = deadline.Add(time.Minute)
					p = r.player.Position()
					cell = world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
				}
				top, grounded = cell, true
			} else if p := r.player.Position(); r.inWaterOrClimbing(p.X, p.Y, p.Z) {
				grounded = false // down a ladder, a vine, through water: the path's way down, no fall
			}
			if t%2 == 1 && r.defend() { // attacked on the way: fought, then on
				r.walker.Goto(goal)
				if fights++; fights <= 4 { // more time for the walk, not without end
					deadline = deadline.Add(30 * time.Second)
				}
			}
		}
		st = r.walker.Status()
		// shut in — a hole it dug itself into, a fall into a pit: a person
		// digs out, a step toward where they are going
		var n int
		if i := strings.Index(st, "every place reachable searched ("); try < 3 && i >= 0 {
			fmt.Sscanf(st[i+len("every place reachable searched ("):], "%d", &n)
		}
		if n == 0 || n >= 20 {
			break
		}
		log.Printf("goto: shut in (%d places): digging out toward %v", n, goal)
		if err := r.digOut(goal); err != nil {
			log.Printf("goto: dig out: %v", err)
			break
		}
	}
	if st == "walking" {
		r.walker.Stop()
		st = "failed: still walking after two minutes"
	}
	// a wider place with no way out toward the goal: dug out of, then the
	// walk again
	if st != "arrived" && strings.Contains(st, "every place reachable searched (") && !r.climbing {
		// once: the walk after it does not climb out again
		r.climbing = true
		defer func() { r.climbing = false }()
		err := r.climbOut(goal)
		if err == nil {
			return cmdGoto(r, args)
		}
		plan("goto: %v", err)
	}
	p := r.player.Position()
	answer := fmt.Sprintf("%.4f %.4f %.4f %s", p.X, p.Y, p.Z, st)
	if st != "arrived" {
		return "", fmt.Errorf("%s", answer)
	}
	return answer, nil
}

func cmdFollow(r *robot, args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("want: follow <player>")
	}
	id, ok := r.players.ByName(args[0])
	if !ok {
		return "", fmt.Errorf("no player %q in the list", args[0])
	}
	r.walker.Follow(func() (float64, float64, float64, bool) {
		e, ok := r.entities.ByUUID(id)
		return e.X, e.Y, e.Z, ok
	}, 2)
	return args[0], nil
}

func cmdStop(r *robot, _ []string) (string, error) {
	r.walker.Stop()
	return "stopped", nil
}

func cmdDig(r *robot, args []string) (string, error) {
	pos, err := blockPos(args)
	if err != nil {
		return "", err
	}
	// water and lava themselves are not dug (a minute's swinging at a pond
	// came of it); a waterlogged block is
	if s, ok := r.world.BlockAt(pos); ok && int(s) < len(block.StateList) {
		if id := block.StateList[s].ID(); id == "minecraft:water" || id == "minecraft:lava" {
			return "", fmt.Errorf("%s at %v: nothing to dig", world.StateString(s), pos)
		}
	}
	// a person lands before mining: off the ground a tick of digging is worth a fifth
	for t := 0; t < 40 && !r.ctl.State().OnGround; t++ {
		if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
			return "", err
		}
	}
	result := make(chan act.Dig, 1)
	started := time.Now()
	r.hands.Dig(pos, func(d act.Dig) { result <- d })
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case d := <-result:
			if d.Err != nil {
				return "", d.Err
			}
			return fmt.Sprintf("ticks=%d was=%s airborne=%d", d.Ticks, world.StateString(d.Was), d.Airborne), nil
		case <-tick.C:
			// hit while digging (a long dig: stone by hand): the dig given
			// up, the caller's defend turns to the attacker
			// given up: the dig told to stop; its answer waited for a moment
			// only (another action may have taken the hands, its dig then
			// never answers)
			giveUp := func() error {
				r.hands.CancelDig()
				select {
				case d := <-result:
					return d.Err
				case <-time.After(2 * time.Second):
					return nil
				}
			}
			if time.UnixMilli(r.lastHurt.Load()).After(started) && r.hostileNear(6) { // not the wall it is digging out of
				return "", fmt.Errorf("hurt while digging %v: %v", pos, giveUp())
			}
			if time.Since(started) > time.Minute {
				_ = giveUp()
				return "", fmt.Errorf("still digging after a minute")
			}
		case <-r.ctx.Done():
			return "", r.ctx.Err()
		}
	}
}

// hostileNear reports whether a hostile mob is within d blocks.
func (r *robot) hostileNear(d float64) bool {
	p := r.player.Position()
	for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, d) {
		if hostileType(e.Type) {
			return true
		}
	}
	return false
}

// cmdCollect walks to every dropped item within the radius, nearest first;
// the server hands it over once the player is close.
func cmdCollect(r *robot, args []string) (string, error) {
	radius := 8.0
	if len(args) == 1 {
		v, err := strconv.ParseFloat(args[0], 64)
		if err != nil {
			return "", err
		}
		radius = v
	}
	before := r.pickedUp.Load()
	deadline := time.Now().Add(time.Minute)
	// the drops of a block just broken appear a tick or two later
	for waited := 0; time.Now().Before(deadline); {
		p := r.player.Position()
		var target *entities.Entity
		r.seenMu.Lock()
		for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, radius) {
			// one fallen more than two below its feet (down a hole, into a
			// cave) is left: going after it is a fall
			if e.Type == "minecraft:item" && !r.stuck[e.ID] && e.Y >= p.Y-2 {
				target = &e
				break
			}
		}
		r.seenMu.Unlock()
		r.defend()
		if target == nil {
			if r.pickedUp.Load() != before || waited >= 10 {
				break
			}
			waited++
			if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
				return "", err
			}
			continue
		}
		// the client lets a dropped item fall by itself and the server may say
		// nothing more of it: walk to the ground under where it was seen
		left := false
		for attempt := 0; attempt < 2; attempt++ {
			ground := r.groundUnder(target.X, target.Y, target.Z)
			if float64(ground.Y) < p.Y-2 {
				r.seenMu.Lock()
				r.stuck[target.ID] = true // its ground is down a hole: left
				r.seenMu.Unlock()
				left = true
				break
			}
			r.clearHead(ground)
			r.walker.Goto(ground)
			for r.walker.Status() == "walking" && time.Now().Before(deadline) {
				if err := r.ctl.WaitTicks(r.ctx, 2); err != nil {
					return "", err
				}
				if _, still := r.entities.Get(target.ID); !still {
					r.walker.Stop()
					break
				}
			}
			// the server hands it over within a few ticks of being close
			for t := 0; t < 20; t++ {
				if _, still := r.entities.Get(target.ID); !still {
					break
				}
				if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
					return "", err
				}
			}
			if _, still := r.entities.Get(target.ID); !still {
				break
			}
			p := r.player.Position()
			log.Printf("collect: the item at %.2f %.2f %.2f, walked to %v (%s), at %.2f %.2f %.2f", target.X, target.Y, target.Z, ground, r.walker.Status(), p.X, p.Y, p.Z)
			// no way the path search knows of, but close: dig one, then a step
			// straight at it
			if math.Hypot(target.X-p.X, target.Z-p.Z) < 4 {
				r.digWay(target.X, target.Z)
				act.LookAt(r.player, false, target.X, target.Y, target.Z)
				r.ctl.SetInput(control.Input{Forward: true})
				for t := 0; t < 10; t++ {
					if _, still := r.entities.Get(target.ID); !still {
						break
					}
					if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
						r.ctl.SetInput(control.Input{})
						return "", err
					}
				}
				r.ctl.SetInput(control.Input{})
				if err := r.ctl.WaitTicks(r.ctx, 4); err != nil {
					return "", err
				}
			}
			if e, ok := r.entities.Get(target.ID); ok {
				target = &e
			}
		}
		if left {
			continue // the next item, if any
		}
		if _, still := r.entities.Get(target.ID); still {
			// not again: a person leaves an item in a hole they cannot reach
			r.seenMu.Lock()
			r.stuck[target.ID] = true
			r.seenMu.Unlock()
			return "", fmt.Errorf("could not pick up the item at %.1f %.1f %.1f", target.X, target.Y, target.Z)
		}
	}
	return strconv.Itoa(int(r.pickedUp.Load() - before)), nil
}

// waitUse waits for a hands action that reports an act.Use.
func waitUse(r *robot, start func(func(act.Use))) (act.Use, error) {
	result := make(chan act.Use, 1)
	start(func(u act.Use) { result <- u })
	select {
	case u := <-result:
		return u, u.Err
	case <-time.After(time.Minute):
		return act.Use{}, fmt.Errorf("no result after a minute")
	case <-r.ctx.Done():
		return act.Use{}, r.ctx.Err()
	}
}

func cmdPlace(r *robot, args []string) (string, error) {
	pos, err := blockPos(args)
	if err != nil {
		return "", err
	}
	u, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(pos, done) })
	if err != nil {
		return "", err
	}
	return world.StateString(u.State), nil
}

func cmdUse(r *robot, args []string) (string, error) {
	pos, err := blockPos(args)
	if err != nil {
		return "", err
	}
	u, err := waitUse(r, func(done func(act.Use)) { r.hands.UseBlock(pos, done) })
	if err != nil {
		return "", err
	}
	return world.StateString(u.State), nil
}

// cmdUseOn uses the held item on a block that does not change for it — a
// minecart put on a rail, a spawn egg — and answers once the click went out.
func cmdUseOn(r *robot, args []string) (string, error) {
	pos, err := blockPos(args)
	if err != nil {
		return "", err
	}
	u, err := waitUse(r, func(done func(act.Use)) { r.hands.Interact(pos, done) })
	if err != nil {
		return "", err
	}
	return world.StateString(u.State), nil
}

// cmdUseToward uses the held item aimed at the top of a block — a boat put
// on water — and answers once the click went out.
func cmdUseToward(r *robot, args []string) (string, error) {
	pos, err := blockPos(args)
	if err != nil {
		return "", err
	}
	x, y, z := float64(pos.X)+0.5, float64(pos.Y)+0.9, float64(pos.Z)+0.5
	if _, err := waitUse(r, func(done func(act.Use)) { r.hands.UseItemToward(x, y, z, done) }); err != nil {
		return "", err
	}
	return "used", nil
}

// cmdDismount gets off the vehicle as a person does: shift, for a moment.
func cmdDismount(r *robot, _ []string) (string, error) {
	if _, _, ok := r.entities.Vehicle(int32(r.player.Login.PlayerID)); !ok {
		return "", fmt.Errorf("riding nothing")
	}
	r.ctl.SetInput(control.Input{Shift: true})
	err := r.ctl.WaitTicks(r.ctx, 2)
	r.ctl.SetInput(control.Input{})
	if err != nil {
		return "", err
	}
	for t := 0; t < 20; t++ {
		if _, _, ok := r.entities.Vehicle(int32(r.player.Login.PlayerID)); !ok {
			p := r.player.Position()
			return fmt.Sprintf("%.4f %.4f %.4f", p.X, p.Y, p.Z), nil
		}
		if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("still riding")
}

func cmdEat(r *robot, _ []string) (string, error) {
	u, err := waitUse(r, r.hands.Eat)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("ticks=%d", u.Ticks), nil
}

func cmdOpen(r *robot, args []string) (string, error) {
	pos, err := blockPos(args)
	if err != nil {
		return "", err
	}
	// shift held (across a bridge) makes the click a use of the held item, not
	// the block's: let go for the click only, as a person does to open a
	// table — the server reads the keys of the tick before — and held again
	// as soon as it is sent (a mob's hit must not push it off)
	sneak := r.walker.Sneak
	click := func() error {
		if sneak {
			r.walker.Sneak = false
			defer func() { r.walker.Sneak = true }()
			if err := r.ctl.WaitTicks(r.ctx, 2); err != nil {
				return err
			}
		}
		_, err := waitUse(r, func(done func(act.Use)) { r.hands.Interact(pos, done) })
		return err
	}
	// a click the server did not take (the block just placed, the robot
	// still turning to it) is clicked again, as a person clicks twice
	for try := 0; try < 3; try++ {
		if err := click(); err != nil {
			if try == 2 {
				return "", err
			}
			continue
		}
		for t := 0; t < 40; t++ {
			if m, ok := r.screens.Open(); ok && len(m.Slots) > 0 {
				return fmt.Sprintf("%d %s %d", m.ID, m.Type, len(m.Slots)), nil
			}
			if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
				return "", err
			}
		}
		plan("open %v: no screen yet, again", pos)
	}
	return "", fmt.Errorf("no screen opened")
}

func cmdClose(r *robot, _ []string) (string, error) {
	m, ok := r.screens.Open()
	if !ok {
		return "", fmt.Errorf("nothing is open")
	}
	return m.Type, r.screens.Close(m.ID)
}

func cmdCraft(r *robot, args []string) (string, error) {
	if len(args) < 1 || len(args) > 2 {
		return "", fmt.Errorf("want: craft <item> [n]")
	}
	n := 1
	if len(args) == 2 {
		v, err := strconv.Atoi(args[1])
		if err != nil {
			return "", err
		}
		n = v
	}
	made, err := r.ui.Craft(r.ctx, fullName(args[0]), n)
	if err != nil {
		return "", fmt.Errorf("made %d: %w", made, err)
	}
	return strconv.Itoa(made), nil
}

func cmdStore(r *robot, args []string) (string, error) { return move(r, args, true) }
func cmdTake(r *robot, args []string) (string, error)  { return move(r, args, false) }

func move(r *robot, args []string, store bool) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("want an item")
	}
	n, err := r.ui.Move(r.ctx, fullName(args[0]), store)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(n), nil
}

func cmdSmelt(r *robot, args []string) (string, error) {
	if len(args) < 2 || len(args) > 3 {
		return "", fmt.Errorf("want: smelt <input> <fuel> [n]")
	}
	n := 1
	if len(args) == 3 {
		v, err := strconv.Atoi(args[2])
		if err != nil {
			return "", err
		}
		n = v
	}
	got, err := r.ui.Smelt(r.ctx, fullName(args[0]), fullName(args[1]), n)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(got), nil
}

// fullName adds the minecraft namespace to a bare item name.
func fullName(s string) string {
	if strings.Contains(s, ":") {
		return s
	}
	return "minecraft:" + s
}

// cmdRecipe shows the book's recipes for an item and what their ingredients take.
func cmdRecipe(r *robot, args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("want an item")
	}
	var parts []string
	for _, rec := range r.book.For(fullName(args[0])) {
		var ings []string
		for _, ing := range rec.Ingredients {
			opts := r.book.Options(ing)
			if len(opts) > 3 {
				opts = append(opts[:3], "…")
			}
			ings = append(ings, fmt.Sprintf("%v%v=%v", ing.Items, ing.Tags, opts))
		}
		parts = append(parts, fmt.Sprintf("%d %s %dx%d makes %d: %s", rec.ID, rec.Kind, rec.Width, rec.Height, rec.Count, strings.Join(ings, " ")))
	}
	return fmt.Sprintf("book=%d %s", r.book.Len(), strings.Join(parts, "; ")), nil
}

// cmdClick clicks a slot of the open menu (the inventory's when none is
// open): left, right, or shift.
func cmdClick(r *robot, args []string) (string, error) {
	if len(args) < 1 || len(args) > 2 {
		return "", fmt.Errorf("want: click <slot> [right|shift]")
	}
	slot, err := strconv.Atoi(args[0])
	if err != nil {
		return "", err
	}
	id := 0
	if m, ok := r.screens.Open(); ok {
		id = m.ID
	}
	button, mode := 0, types.ContainerInputPickup
	if len(args) == 2 {
		switch args[1] {
		case "right":
			button = 1
		case "shift":
			mode = types.ContainerInputQuickMove
		default:
			return "", fmt.Errorf("right or shift, not %q", args[1])
		}
	}
	if err := r.screens.Click(id, slot, button, mode); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d %d", id, slot), r.ctl.WaitTicks(r.ctx, 2)
}

// cmdUpdates is how many single slots the server has sent: a click the robot
// predicted right brings none.
func cmdUpdates(r *robot, _ []string) (string, error) {
	r.screens.Lock()
	defer r.screens.Unlock()
	return strconv.Itoa(r.screens.SlotUpdates), nil
}

func cmdSign(r *robot, args []string) (string, error) {
	if len(args) < 4 {
		return "", fmt.Errorf("want: sign <x> <y> <z> <line|line|line|line>")
	}
	pos, err := blockPos(args[:3])
	if err != nil {
		return "", err
	}
	var lines [4]string
	copy(lines[:], strings.SplitN(strings.Join(args[3:], " "), "|", 4))
	if _, err := waitUse(r, func(done func(act.Use)) { r.hands.Place(pos, done) }); err != nil {
		return "", err
	}
	select {
	case at := <-r.signs.Opened():
		if err := r.signs.Write(at, lines); err != nil {
			return "", err
		}
		return fmt.Sprintf("%v", at), nil
	case <-time.After(5 * time.Second):
		return "", fmt.Errorf("no sign editor opened")
	}
}

func entityArg(r *robot, s string) (func() (entities.Entity, bool), error) {
	id, err := strconv.Atoi(s)
	if err != nil {
		return nil, err
	}
	return func() (entities.Entity, bool) { return r.entities.Get(int32(id)) }, nil
}

func cmdInteract(r *robot, args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("want an entity id")
	}
	where, err := entityArg(r, args[0])
	if err != nil {
		return "", err
	}
	if e, ok := where(); ok {
		// within reach first (the game measures to its box): a spot a block
		// and a half from it, on this side
		p := r.player.Position()
		dx, dz := p.X-e.X, p.Z-e.Z
		half := 0.0
		if t := entity.ByName[e.Type]; t != nil {
			half = t.Width / 2
		}
		if d := math.Hypot(dx, dz); d-half > 2.5 {
			x, z := e.X+dx/d*1.5, e.Z+dz/d*1.5
			if _, err := cmdGoto(r, []string{strconv.Itoa(int(math.Floor(x))), strconv.Itoa(int(math.Floor(e.Y + 0.01))), strconv.Itoa(int(math.Floor(z)))}); err != nil {
				return "", fmt.Errorf("walking to it: %w", err)
			}
		}
	}
	done := make(chan error, 1)
	r.hands.InteractEntity(where, func(err error) { done <- err })
	if err := <-done; err != nil {
		return "", err
	}
	return args[0], r.ctl.WaitTicks(r.ctx, 5)
}

func cmdTrade(r *robot, args []string) (string, error) {
	if len(args) < 2 || len(args) > 3 {
		return "", fmt.Errorf("want: trade <entity id> <offer> [times]")
	}
	if _, err := cmdInteract(r, args[:1]); err != nil {
		return "", err
	}
	offer, err := strconv.Atoi(args[1])
	if err != nil {
		return "", err
	}
	times := 1
	if len(args) == 3 {
		if times, err = strconv.Atoi(args[2]); err != nil {
			return "", err
		}
	}
	for t := 0; t < 40; t++ {
		if m, ok := r.screens.Open(); ok && len(m.Offers) > 0 {
			break
		}
		if err := r.ctl.WaitTicks(r.ctx, 1); err != nil {
			return "", err
		}
	}
	made, err := r.ui.Trade(r.ctx, offer, times)
	if m, ok := r.screens.Open(); ok {
		_ = r.screens.Close(m.ID)
	}
	if err != nil {
		return "", fmt.Errorf("traded %d: %w", made, err)
	}
	return strconv.Itoa(made), nil
}

// groundUnder is the cell above the first block with a collision shape at or
// below (x, y, z): where a dropped item comes to rest.
func (r *robot) groundUnder(x, y, z float64) world.BlockPos {
	p := world.BlockPos{X: int(math.Floor(x)), Y: int(math.Floor(y + 0.01)), Z: int(math.Floor(z))}
	for i := 0; i < 8; i++ {
		below := world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}
		s, ok := r.world.BlockAt(below)
		if !ok || len(block.CollisionShape(s)) > 0 {
			break
		}
		p = below
	}
	if r.standable(p) {
		return p
	}
	// an item floating on water, lying in a hole: the nearest place beside it
	// to stand, which a player picks up from (its box grown by a block)
	best, bestD := p, math.MaxFloat64
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				q := world.BlockPos{X: int(math.Floor(x)) + dx, Y: int(math.Floor(y+0.01)) + dy, Z: int(math.Floor(z)) + dz}
				if !r.standable(q) {
					continue
				}
				cx, cz := float64(q.X)+0.5-x, float64(q.Z)+0.5-z
				if d := cx*cx + cz*cz + float64(dy*dy); d < bestD {
					best, bestD = q, d
				}
			}
		}
	}
	return best
}

// digOut digs a step out of a hole toward goal: up when it is higher (the two
// blocks over the step ahead and the one over the head, to jump), else ahead,
// two high.
func (r *robot) digOut(goal world.BlockPos) error {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	// in a block itself (gravel or sand fallen into its place): that dug
	// first, as a person digs out of the gravel that fell on them
	for _, c := range []world.BlockPos{{X: x, Y: y, Z: z}, {X: x, Y: y + 1, Z: z}} {
		if s, ok := r.world.BlockAt(c); ok && len(block.CollisionShape(s)) > 0 {
			plan("dig out: %s in its own place at %v", world.StateString(s), c)
			if err := r.clear(c); err != nil {
				return err
			}
		}
	}
	dx, dz := goal.X-x, goal.Z-z
	step := [2]int{sign(dx), 0}
	if abs(dz) > abs(dx) {
		step = [2]int{0, sign(dz)}
	}
	if step == [2]int{} {
		step = [2]int{1, 0}
	}
	nx, nz := x+step[0], z+step[1]
	// ahead only, never over its head: up where the block over its head is
	// open already, else a level step with the headroom of the next
	cells := []world.BlockPos{{X: nx, Y: y, Z: nz}, {X: nx, Y: y + 1, Z: nz}}
	if goal.Y > y {
		if r.openAt(world.BlockPos{X: x, Y: y + 2, Z: z}) {
			cells = []world.BlockPos{{X: nx, Y: y + 1, Z: nz}, {X: nx, Y: y + 2, Z: nz}, {X: nx, Y: y + 3, Z: nz}}
		} else {
			cells = append(cells, world.BlockPos{X: nx, Y: y + 2, Z: nz})
		}
	}
	for _, c := range cells {
		if err := r.clear(c); err != nil {
			return err
		}
	}
	return nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// climbOut digs its way toward goal from where no path leads — a basin, a
// ravine, a pit too wide to dig out of in a step — as a person in Minecraft
// is never shut in: to the place it can walk to nearest the goal, then a
// staircase dug toward it (up while the goal is higher, level else; a block
// put under a step with nothing under it), the path tried again every few
// steps. Water or lava ahead turns it the other way round the goal.
func (r *robot) climbOut(goal world.BlockPos) error { return r.climbOutWithin(goal, 0) }

// climbOutWithin is climbOut where a way longer than maxLen places counts as
// none (0: any way): fallen down a cliff, the way round through the caves is
// no way back up.
func (r *robot) climbOutWithin(goal world.BlockPos, maxLen int) error {
	g := r.walker.Graph
	short := func(nodes []path.Node) bool { return maxLen <= 0 || len(nodes) <= maxLen }
	for round := 0; round < 4; round++ {
		p := r.player.Position()
		start, _ := g.Start(p.X, p.Y, p.Z)
		nodes, err := g.Find(start, goal, 100000)
		if err == nil && short(nodes) {
			return nil // a way now
		}
		var np *path.NoPathError
		if err != nil && !errors.As(err, &np) {
			return err
		}
		// (a search stopped at its limit, in a wide cave, still knows the
		// place nearest the goal it got to: dug on from there)
		// to the place nearest the goal it can walk to — only if that is
		// nearer than here: a search through a cave may find "nearest" far
		// off, and the stairs from here are the shorter way
		dist := func(a world.BlockPos) float64 {
			return math.Sqrt(float64((a.X-goal.X)*(a.X-goal.X) + (a.Y-goal.Y)*(a.Y-goal.Y) + (a.Z-goal.Z)*(a.Z-goal.Z)))
		}
		if np == nil {
			// a way, too long: the stairs from here
		} else if c := np.Closest.Pos; c != start.Pos && dist(c) < dist(start.Pos)-1 {
			plan("climb out: to %v, the nearest it gets to %v", c, goal)
			if err := r.walkTo(c); err != nil {
				return err
			}
		}
		var spiral [2]int // the way of the next step up a spiral, when under the goal
		for step := 0; step < 12; step++ {
			r.defend() // a cave on the way: what is in it is fought, not dug past
			p := r.player.Position()
			x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
			dx, dz := goal.X-x, goal.Z-z
			if abs(dx) <= 1 && abs(dz) <= 1 && abs(goal.Y-y) <= 1 {
				// next to it and still no way in: the goal's own cells dug
				// (a trunk, a bank in the way) and a step into it
				for _, c := range []world.BlockPos{goal, {X: goal.X, Y: goal.Y + 1, Z: goal.Z}} {
					if err := r.clear(c); err != nil {
						return fmt.Errorf("climb out: into %v: %w", goal, err)
					}
				}
				if r.openAt(world.BlockPos{X: goal.X, Y: goal.Y - 1, Z: goal.Z}) {
					_ = r.fill(world.BlockPos{X: goal.X, Y: goal.Y - 1, Z: goal.Z})
				}
				return r.walkTo(goal)
			}
			ways := [][2]int{{sign(dx), 0}, {0, sign(dz)}}
			if abs(dz) > abs(dx) {
				ways[0], ways[1] = ways[1], ways[0]
			}
			if abs(dx) <= 1 && abs(dz) <= 1 && abs(goal.Y-y) > 1 {
				// right under it (or over it): a spiral round a small column,
				// a quarter turn each step (toward it would step away and back)
				if spiral == ([2]int{}) {
					spiral = [2]int{1, 0}
				}
				ways = [][2]int{spiral, rightOf(spiral), {-spiral[0], -spiral[1]}, {spiral[1], -spiral[0]}}
			}
			moved := false
			for _, d := range ways {
				if d == [2]int{} {
					continue
				}
				fx, fz := x+d[0], z+d[1]
				// everything dug is ahead, never over its head: a step up
				// wants the block over its head open (the jump), so it is
				// dug a step before, as the third block of the step ahead;
				// where it is not open yet, a level step first
				up := goal.Y > y && r.openAt(world.BlockPos{X: x, Y: y + 2, Z: z})
				down := goal.Y < y-1
				ny := y
				cells := []world.BlockPos{{X: fx, Y: y, Z: fz}, {X: fx, Y: y + 1, Z: fz}}
				if goal.Y > y {
					cells = append(cells, world.BlockPos{X: fx, Y: y + 2, Z: fz})
				}
				if up {
					ny = y + 1
					cells = []world.BlockPos{{X: fx, Y: y + 1, Z: fz}, {X: fx, Y: y + 2, Z: fz}, {X: fx, Y: y + 3, Z: fz}}
				}
				if down { // a stair step down, as descend digs: ahead, three high
					ny = y - 1
					cells = []world.BlockPos{{X: fx, Y: y + 1, Z: fz}, {X: fx, Y: y, Z: fz}, {X: fx, Y: y - 1, Z: fz}}
				}
				if why := r.stepDanger(fx, ny+1, fz); why != "" {
					plan("climb out: %s toward %v", why, d)
					continue
				}
				ok := true
				for _, c := range cells {
					if r.floorAhead(c) {
						continue // the floor it walks on (farmland), not in the way
					}
					if err := r.clear(c); err != nil {
						ok = false
						break
					}
				}
				floor := world.BlockPos{X: fx, Y: ny - 1, Z: fz}
				// a step up on blocks is a stair; a step down over the air
				// on the land would be a bridge: not that way
				if ok && r.openAt(floor) && ny < y && r.openSky(world.BlockPos{X: fx, Y: ny, Z: fz}) {
					plan("climb out: the open air below %v: not that way", floor)
					continue
				}
				if ok && r.openAt(floor) {
					ok = r.fill(floor) == nil
				}
				if ok && r.walkTo(world.BlockPos{X: fx, Y: ny, Z: fz}) == nil {
					moved = true
					if spiral != ([2]int{}) {
						spiral = rightOf(d)
					}
					break
				}
			}
			if !moved {
				return fmt.Errorf("climb out: no step from %d %d %d toward %v", x, y, z, goal)
			}
			if step%4 == 3 {
				p := r.player.Position()
				start, _ := g.Start(p.X, p.Y, p.Z)
				if nodes, err := g.Find(start, goal, 100000); err == nil && short(nodes) {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("climb out: still no way to %v", goal)
}

// inWaterOrClimbing reports whether the feet at x, y, z are in water or on
// a ladder, a vine — a way down the path takes that is no fall.
func (r *robot) inWaterOrClimbing(x, y, z float64) bool {
	feet := world.BlockPos{X: int(math.Floor(x)), Y: int(math.Floor(y + 1e-6)), Z: int(math.Floor(z))}
	s, ok := r.world.BlockAt(feet)
	if !ok {
		return false
	}
	if f := block.FluidOf(s); f != nil && strings.HasSuffix(f.Name, "water") {
		return true
	}
	return r.walker.Graph.Tags.BlockIn(s, "minecraft:climbable")
}

// backUp, fallen from top (the ground it stood on) on the way to goal: the
// way on from down here if it is short; else back up where it fell from, as a
// person does — a staircase dug into the rock, a block of dirt or stone put
// under a step with nothing under it — and on from there.
func (r *robot) backUp(top, goal world.BlockPos) {
	p := r.player.Position()
	if math.Hypot(p.X-float64(top.X)-0.5, p.Z-float64(top.Z)-0.5) > 8 {
		return // not under where it fell from (respawned, moved by the server): no way back up from here
	}
	plan("goto: fell %d blocks from %v", top.Y-int(math.Floor(p.Y+1e-6)), top)
	g := r.walker.Graph
	start, _ := g.Start(p.X, p.Y, p.Z)
	d := math.Hypot(float64(goal.X)+0.5-p.X, float64(goal.Z)+0.5-p.Z) + math.Abs(float64(goal.Y)-p.Y)
	if nodes, err := g.Find(start, goal, 100000); err == nil && float64(len(nodes)) <= 2*d+24 {
		return // the way on from down here is short
	}
	r.climbing = true
	defer func() { r.climbing = false }()
	// up to where it fell from: a way there only as long as the fall is deep
	if err := r.climbOutWithin(top, 3*(top.Y-start.Pos.Y)+16); err != nil {
		plan("goto: back up to %v: %v", top, err)
		return
	}
	if err := r.walkTo(top); err != nil {
		plan("goto: back up to %v: %v", top, err)
	}
}

// legsToward walks toward a goal more than 64 blocks off in legs of forty,
// along the line to it (a little to either side where a leg is blocked),
// till it is within 64; the walk there is the caller's.
func (r *robot) legsToward(goal world.BlockPos) error {
	// legs go from ground to ground under the sky; underground (a cave's
	// floor is no ground on the way) the walk is the path's alone
	if !r.underSky() && !r.skyWithin(8) { // a tree's leaves, an overhang are no roof; a mine is
		return nil
	}
	if r.underground(goal) { // down its stairs, a mine: not over the land
		return nil
	}
	r.legging = true
	defer func() { r.legging = false }()
	for leg := 0; leg < 40; leg++ {
		p := r.player.Position()
		dx, dz := float64(goal.X)+0.5-p.X, float64(goal.Z)+0.5-p.Z
		dist := math.Hypot(dx, dz)
		if dist <= 64 {
			return nil
		}
		a := math.Atan2(dz, dx)
		moved := false
		for _, dev := range []float64{0, 0.4, -0.4, 0.8, -0.8} {
			x, z := int(math.Floor(p.X+math.Cos(a+dev)*40)), int(math.Floor(p.Z+math.Sin(a+dev)*40))
			g, ok := r.groundAt(x, z, int(math.Floor(p.Y)))
			if !ok {
				continue
			}
			if _, err := cmdGoto(r, []string{strconv.Itoa(g.X), strconv.Itoa(g.Y), strconv.Itoa(g.Z)}); err == nil {
				moved = true
				break
			}
		}
		if !moved {
			return fmt.Errorf("no leg on toward %v from %.0f %.0f %.0f", goal, p.X, p.Y, p.Z)
		}
	}
	return nil
}

// walkTo walks to p by the path alone, no digging out — climbOut's steps.
func (r *robot) walkTo(p world.BlockPos) error {
	r.walker.Goto(p)
	for t := 0; r.walker.Status() == "walking" && t < 200; t++ {
		if err := r.ctl.WaitTicks(r.ctx, 5); err != nil {
			return err
		}
		if t%2 == 1 && r.defend() { // attacked on the way: fought, then on
			r.walker.Goto(p)
		}
	}
	if st := r.walker.Status(); st != "arrived" {
		r.walker.Stop()
		return fmt.Errorf("to %v: %s", p, st)
	}
	return nil
}

// digWay digs a way two blocks high from where the robot stands to the
// column (x, z) at its height, along x then z, as a person tunnels to a drop.
func (r *robot) digWay(x, z float64) {
	p := r.player.Position()
	cur := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	goal := world.BlockPos{X: int(math.Floor(x)), Y: cur.Y, Z: int(math.Floor(z))}
	for steps := 0; cur != goal && steps < 6; steps++ {
		switch {
		case cur.X != goal.X:
			cur.X += sign(goal.X - cur.X)
		default:
			cur.Z += sign(goal.Z - cur.Z)
		}
		for _, y := range []int{cur.Y, cur.Y + 1} {
			at := world.BlockPos{X: cur.X, Y: y, Z: cur.Z}
			s, ok := r.world.BlockAt(at)
			if !ok || len(block.CollisionShape(s)) == 0 || block.FluidOf(s) != nil || r.floorAhead(at) {
				continue
			}
			if _, err := cmdDig(r, []string{strconv.Itoa(at.X), strconv.Itoa(at.Y), strconv.Itoa(at.Z)}); err != nil {
				log.Printf("collect: digging a way at %v: %v", at, err)
				return
			}
		}
	}
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// clearHead digs the block over p when it is all that keeps the robot from
// standing at p — an item in a pit a block deep, as mining a hillside leaves.
func (r *robot) clearHead(p world.BlockPos) {
	if r.standable(p) {
		return
	}
	head := world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}
	feet, ok1 := r.world.BlockAt(p)
	below, ok2 := r.world.BlockAt(world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z})
	s, ok3 := r.world.BlockAt(head)
	if !ok1 || !ok2 || !ok3 || len(block.CollisionShape(feet)) > 0 || len(block.CollisionShape(below)) == 0 ||
		len(block.CollisionShape(s)) == 0 || block.FluidOf(s) != nil {
		return
	}
	if _, err := cmdDig(r, []string{strconv.Itoa(head.X), strconv.Itoa(head.Y), strconv.Itoa(head.Z)}); err != nil {
		log.Printf("collect: clearing %v: %v", head, err)
	}
}

// standable: the robot's feet can be at p — air there and above, no fluid,
// something solid below.
func (r *robot) standable(p world.BlockPos) bool {
	for i, want := range []bool{true, false, false} { // below solid; feet, head free
		s, ok := r.world.BlockAt(world.BlockPos{X: p.X, Y: p.Y - 1 + i, Z: p.Z})
		if !ok || (len(block.CollisionShape(s)) > 0) != want || i > 0 && block.FluidOf(s) != nil {
			return false
		}
	}
	return true
}

// cmdAround draws the blocks around the robot, a layer a line, from its feet
// down one to two up: # solid, ~ fluid, . open, @ the robot's column.
func cmdAround(r *robot, _ []string) (string, error) {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	var layers []string
	for dy := -1; dy <= 2; dy++ {
		var rows []string
		for dz := -3; dz <= 3; dz++ {
			var b strings.Builder
			for dx := -3; dx <= 3; dx++ {
				s, ok := r.world.BlockAt(world.BlockPos{X: x + dx, Y: y + dy, Z: z + dz})
				switch {
				case !ok:
					b.WriteByte('?')
				case block.FluidOf(s) != nil:
					b.WriteByte('~')
				case len(block.CollisionShape(s)) > 0:
					b.WriteByte('#')
				case dx == 0 && dz == 0:
					b.WriteByte('@')
				default:
					b.WriteByte('.')
				}
			}
			rows = append(rows, b.String())
		}
		layers = append(layers, fmt.Sprintf("y%+d %s", dy, strings.Join(rows, "/")))
	}
	return fmt.Sprintf("at %d %d %d (x across, z down): %s", x, y, z, strings.Join(layers, " | ")), nil
}

// underRoof: something solid over the robot's head, up to 48 blocks: in a
// cave, a mine, a shelter — not under the sky.
// underSky reports whether it is out under the open sky: the sky's light at
// its head, as the server sends it, is near full (leaves over it take one
// a layer; a room, a mine, a hideout take it all). Without the light known,
// nothing solid over its head.
func (r *robot) underSky() bool {
	p := r.player.Position()
	head := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y+1e-6)) + 1, Z: int(math.Floor(p.Z))}
	if sky, _, ok := r.world.LightAt(head); ok {
		return sky >= 13
	}
	return !r.underRoof()
}

// underground reports whether pos has solid blocks over it (a mine, a cave
// under a hill); not known (not loaded), no.
func (r *robot) underground(pos world.BlockPos) bool {
	for dy := 2; dy <= 64; dy++ {
		if s, ok := r.world.BlockAt(world.BlockPos{X: pos.X, Y: pos.Y + dy, Z: pos.Z}); ok && len(block.CollisionShape(s)) > 0 && block.FluidOf(s) == nil {
			if !strings.Contains(world.StateString(s), "leaves") {
				return true
			}
		}
	}
	return false
}

// skyWithin reports whether the open sky is within d blocks over its head:
// nothing solid from there up (an overhang, a cave's mouth — not a mine).
func (r *robot) skyWithin(d int) bool {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	top := y + 1
	for dy := 2; dy <= 64; dy++ {
		if s, ok := r.world.BlockAt(world.BlockPos{X: x, Y: y + dy, Z: z}); ok && len(block.CollisionShape(s)) > 0 {
			top = y + dy
		}
	}
	return top-y-1 <= d
}

func (r *robot) underRoof() bool {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	for dy := 2; dy <= 48; dy++ {
		if s, ok := r.world.BlockAt(world.BlockPos{X: x, Y: y + dy, Z: z}); ok && len(block.CollisionShape(s)) > 0 {
			return true
		}
	}
	return false
}
