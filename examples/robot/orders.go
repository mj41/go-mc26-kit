package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/msg"
	"github.com/mj41/go-mc26-kit/bot/world"
)

// order is a command a player gave in chat.
type order struct {
	from    string
	line    string
	whisper bool
}

// onChat takes orders from players the way people command a bot: "robot
// <command>" in public chat, or the command alone whispered to it (/msg). The
// orders run one at a time off the packet goroutine (obey); "follow" and
// "come" without a name mean the sender.
func (r *robot) onChat(c msg.PlayerChat) error {
	if c.Name == r.client.Auth.Name {
		return nil
	}
	o := order{from: c.Name, whisper: c.Type == "minecraft:msg_command_incoming"}
	switch {
	case o.whisper:
		o.line = strings.TrimSpace(c.Content)
	case strings.HasPrefix(c.Content, "robot "):
		o.line = strings.TrimSpace(strings.TrimPrefix(c.Content, "robot "))
	default:
		return nil
	}
	select {
	case r.orders <- o:
	default:
		log.Printf("robot: busy, dropped the order %q from %s", o.line, o.from)
	}
	return nil
}

// obey runs the orders and answers each the way it was given.
func (r *robot) obey() {
	for o := range r.orders {
		words := strings.Fields(o.line)
		if len(words) == 0 {
			continue
		}
		if (words[0] == "follow" || words[0] == "come") && len(words) == 1 {
			words = append(words, o.from)
		}
		var reply string
		if cmd, ok := commands[words[0]]; !ok {
			reply = fmt.Sprintf("I do not know %q", words[0])
		} else if answer, err := runOrder(r, cmd, words); err != nil {
			reply = "error: " + err.Error()
		} else {
			reply = answer
		}
		if reply == "" {
			reply = "done"
		}
		if len(reply) > 200 {
			reply = reply[:200] + "…"
		}
		log.Printf("robot: order from %s %q: %s", o.from, o.line, reply)
		var err error
		if o.whisper {
			err = r.chat.SendCommand("msg " + o.from + " " + reply)
		} else {
			err = r.chat.SendMessage(reply)
		}
		if err != nil {
			log.Printf("robot: answering %s: %v", o.from, err)
		}
	}
}

// cmdFind is where the nearest of the named blocks is, as the goals look for it.
func cmdFind(r *robot, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("want: find <block>...")
	}
	var names []string
	for _, a := range args {
		if !strings.Contains(a, ":") {
			a = "minecraft:" + a
		}
		names = append(names, a)
	}
	pos, ok := r.nearest(names, nil)
	if !ok {
		return "none", nil
	}
	return fmt.Sprintf("%d %d %d", pos.X, pos.Y, pos.Z), nil
}

// cmdCome walks to where a player stands.
func cmdCome(r *robot, args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("want: come <player>")
	}
	id, ok := r.players.ByName(args[0])
	if !ok {
		return "", fmt.Errorf("no player %q in the list", args[0])
	}
	e, ok := r.entities.ByUUID(id)
	if !ok {
		return "", fmt.Errorf("I cannot see %s", args[0])
	}
	goal := world.BlockPos{X: int(math.Floor(e.X)), Y: int(math.Floor(e.Y + 1e-6)), Z: int(math.Floor(e.Z))}
	return cmdGoto(r, []string{fmt.Sprint(goal.X), fmt.Sprint(goal.Y), fmt.Sprint(goal.Z)})
}

// concurrentOK are the commands that run beside another: they only look,
// or stop the one running.
var concurrentOK = map[string]bool{
	"pos": true, "state": true, "status": true, "inv": true, "time": true, "seen": true,
	"nearby": true, "entities": true, "around": true, "light": true, "block": true, "stop": true,
}

// errDied ends an order the robot died in: it respawned with nothing, far
// from its work — what it was doing does not go on from there (a person
// starts again: recover, home).
var errDied = errors.New("died")

// diedInOrder is errDied once the robot died since the running order began.
func (r *robot) diedInOrder() error {
	if r.deaths.Load() != r.orderDeaths.Load() {
		p := r.player.Position()
		return fmt.Errorf("%w during the order (respawned at %.0f %.0f %.0f)", errDied, p.X, p.Y, p.Z)
	}
	return nil
}

// runOrder runs a command a player gave, noted in the events.
func runOrder(r *robot, cmd func(*robot, []string) (string, error), words []string) (string, error) {
	// one command at a time, from the console or a player's chat: two would
	// drive the walker and the hands at once. A look at it (pos, state…) and
	// stop go on beside the one running.
	if !concurrentOK[words[0]] {
		r.cmdMu.Lock()
		defer r.cmdMu.Unlock()
		r.orderDeaths.Store(r.deaths.Load())
	}
	done := r.running(strings.Join(words, " "))
	answer, err := cmd(r, words[1:])
	if errors.Is(err, errLate) { // stopped for the dark on purpose: home (or walled in), not a failure
		answer, err = "stopped: "+err.Error(), nil
	}
	done(answer, err)
	return answer, err
}
