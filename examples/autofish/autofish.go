package main

import (
	"flag"
	"log"
	"time"

	//"github.com/mattn/go-colorable"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/msg"
	"github.com/mj41/go-mc26-kit/bot/playerlist"
	"github.com/mj41/go-mc26/chat"
	_ "github.com/mj41/go-mc26/data/lang/en-us"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

const timeout = 45

var (
	address = flag.String("address", "127.0.0.1:25565", "The server address")
	name    = flag.String("name", "Fisher", "The player's name")
)

var (
	c *bot.Client
	p *basic.Player

	playerList  *playerlist.PlayerList
	chatHandler *msg.Manager

	watch chan time.Time
)

func main() {
	flag.Parse()
	// log.SetOutput(colorable.NewColorableStdout()) // optional for colorable output
	c = bot.NewClient()
	c.Auth.Name = *name
	p = basic.NewPlayer(c, basic.DefaultSettings, basic.EventsListener{
		GameStart:  onGameStart,
		Disconnect: onDisconnect,
		Death:      onDeath,
	})
	playerList = playerlist.New(c)
	chatHandler = msg.New(c, p, playerList, msg.EventsHandler{
		SystemChat:        onSystemChat,
		PlayerChatMessage: onPlayerChat,
		DisguisedChat:     onDisguisedChat,
	})

	// Register event handlers

	c.Events.AddListener(soundListener)

	// Login
	err := c.JoinServer(*address)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Login success")

	// JoinGame
	err = c.HandleGame()
	if err != nil {
		log.Fatal(err)
	}
}

func onDeath() error {
	log.Println("Died and Respawned")
	// If we exclude Respawn(...) then the player won't press the "Respawn" button upon death
	return p.Respawn()
}

func onGameStart() error {
	log.Println("Game start")

	watch = make(chan time.Time)
	go watchDog()

	return UseItem(types.InteractionHandMainHand)
}

var soundListener = bot.PacketHandler{
	ID:       play.Sound{}.PacketID(),
	Priority: 0,
	F: func(p pk.Packet) error {
		var s play.Sound
		if err := p.Scan(&s); err != nil {
			return err
		}
		// The sound holder carries registry id + 1 (0 means an inline sound event);
		// positions are fixed-point eighths of a block.
		actualID := int(s.Sound.ID) - 1
		return onSound(actualID, int(s.Source), float64(s.X)/8, float64(s.Y)/8, float64(s.Z)/8, float32(s.Volume), float32(s.Pitch))
	},
}

func UseItem(hand types.InteractionHand) error {
	use := play.UseItem{Hand: hand} // sequence 0, no rotation
	return c.Conn.WritePacket(pk.Marshal(use.PacketID(), use))
}

//goland:noinspection SpellCheckingInspection
func onSound(id int, category int, x, y, z float64, volume, pitch float32) error {
	if id == 609 { // entity.fishing_bobber.splash
		if err := UseItem(types.InteractionHandMainHand); err != nil { // retrieve
			return err
		}
		log.Println("gra~")
		time.Sleep(time.Millisecond * 300)
		if err := UseItem(types.InteractionHandMainHand); err != nil { // throw
			return err
		}
		watch <- time.Now()
	}
	return nil
}

func onSystemChat(c chat.Message, overlay bool) error {
	log.Printf("System Chat: %v, Overlay: %v", c, overlay)
	return nil
}

func onPlayerChat(c chat.Message, _ bool) error {
	log.Println("Player Chat:", c)
	return nil
}

func onDisguisedChat(c chat.Message) error {
	log.Println("Disguised Chat:", c)
	return nil
}

func onDisconnect(c chat.Message) error {
	log.Println("Disconnect:", c)
	return nil
}

func watchDog() {
	to := time.NewTimer(time.Second * timeout)
	for {
		select {
		case <-watch:
		case <-to.C:
			log.Println("rethrow")
			if err := UseItem(types.InteractionHandMainHand); err != nil {
				panic(err)
			}
		}
		to.Reset(time.Second * timeout)
	}
}
