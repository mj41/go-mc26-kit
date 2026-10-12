package main

import (
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
		src := source{kind: "chat", who: o.from}
		if o.whisper {
			src.kind = "whisper"
		}
		// the answer the way the order was given, once it is done
		answer := func(reply string) {
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
		cmd, ok := commands[words[0]]
		if !ok {
			answer(fmt.Sprintf("I do not know %q", words[0]))
			continue
		}
		r.order(words, src, cmd, func(a string, err error) {
			if err != nil {
				a = "error: " + err.Error()
			}
			answer(a)
		}, false)
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
