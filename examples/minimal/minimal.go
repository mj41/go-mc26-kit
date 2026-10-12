// Minimal is the smallest bot that behaves: it joins a server in offline mode
// (online-mode=false: a name is all it needs), prints the chat it sees, answers
// "hi" with "hello <name>", and leaves cleanly on Ctrl-C or when the server
// kicks it. For an online-mode server, log in with package microsoft first
// (examples/microsoft); for a bot with a console, see examples/daze.
//
//	go run ./examples/minimal -address localhost:25565 -name Minimal
//
// minimal_test.go runs it against a fake server (examples/README.md).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/msg"
	"github.com/mj41/go-mc26-kit/bot/playerlist"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/version"
	pk "github.com/mj41/go-mc26/net/packet"
)

var (
	address = flag.String("address", "localhost:25565", "the server's address")
	name    = flag.String("name", "Minimal", "the player's name")
)

func main() {
	flag.Parse()
	// Ctrl-C ends run, which leaves the server.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *address, *name, bot.JoinOptions{}); err != nil {
		log.Fatal(err)
	}
}

// run is the bot: it joins address as name and returns when the server kicks it or ctx ends. opts is how
// it dials: the zero value for a real server (a test gives a fake server's in-memory dialer).
func run(ctx context.Context, address, name string, opts bot.JoinOptions) error {
	// The library's version is the Minecraft version this bot speaks: the server must be the same.
	log.Printf("Minecraft %s (protocol %d)", version.Name, version.ProtocolVersion)

	client := bot.NewClient()
	client.Auth.Name = name // offline mode: no UUID, no access token

	// basic.Player is required: it answers the server's keep-alives and reports
	// the game's start, a kick, health, death and teleports.
	var player *basic.Player
	player = basic.NewPlayer(client, basic.DefaultSettings, basic.EventsListener{
		GameStart: func() error { log.Print("joined"); return nil },
		Disconnect: func(reason chat.Message) error {
			return bot.DisconnectErr(reason) // ends HandleGame below
		},
		// Without a control.Controller (which moves the player and confirms
		// teleports itself), a teleport is confirmed here.
		Teleported: func(_, _, _ float64, _, _ float32, _, teleportID int32) error {
			return player.AcceptTeleportation(pk.VarInt(teleportID))
		},
	})

	var chatMgr *msg.Manager
	chatMgr = msg.New(client, player, playerlist.New(client), msg.EventsHandler{
		SystemChat: func(m chat.Message, overlay bool) error {
			if !overlay { // overlay messages are the ones above the hotbar
				log.Print("[system] ", m.ClearString())
			}
			return nil
		},
		PlayerChat: func(m msg.PlayerChat) error {
			log.Printf("<%s> %s", m.Name, m.Content)
			if m.Sender != client.UUID && strings.EqualFold(strings.TrimSpace(m.Content), "hi") {
				return chatMgr.SendMessage("hello " + m.Name)
			}
			return nil
		},
	})

	if err := client.JoinServerWithOptions(address, opts); err != nil {
		return err
	}
	defer client.Close()

	// The end of ctx closes the connection, which ends HandleGame.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			client.Close()
		case <-done: // (run returned: nothing to close)
		}
	}()

	for {
		err := client.HandleGame() // reads and handles packets; returns only with an error
		var kicked bot.DisconnectErr
		var handlerErr bot.PacketHandlerError
		switch {
		case errors.As(err, &kicked):
			log.Print("kicked: ", chat.Message(kicked).ClearString())
			return nil
		case ctx.Err() != nil:
			log.Print("left the server")
			return nil
		case errors.As(err, &handlerErr):
			log.Print(err) // one handler failed; the connection is fine
		default:
			return fmt.Errorf("connection lost: %w", err)
		}
	}
}
