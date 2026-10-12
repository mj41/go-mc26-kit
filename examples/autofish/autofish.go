// Autofish fishes: it casts its rod, reels in at the splash of a bite and casts again, and casts again
// when no fish has bitten for 45 s. Give it a rod in its main hand.
//
//	go run ./examples/autofish -address localhost:25565 -name Fisher
//
// autofish_test.go runs it against a fake server that plays the fishing (examples/README.md).
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	//"github.com/mattn/go-colorable"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/msg"
	"github.com/mj41/go-mc26-kit/bot/playerlist"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/soundid"
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

	watch   chan time.Time  // a reel, told to the watchdog
	running context.Context // run's: its end stops the watchdog
)

func main() {
	flag.Parse()
	// log.SetOutput(colorable.NewColorableStdout()) // optional for colorable output
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *address, *name, bot.JoinOptions{}); err != nil {
		log.Fatal(err)
	}
}

// run fishes on address as name till ctx ends or the connection does. opts is how it dials: the zero value
// for a real server (a test gives a fake server's in-memory dialer).
func run(ctx context.Context, address, name string, opts bot.JoinOptions) error {
	running = ctx
	c = bot.NewClient()
	c.Auth.Name = name
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
	if err := c.JoinServerWithOptions(address, opts); err != nil {
		return err
	}
	defer c.Close()
	log.Println("Login success")

	done := make(chan struct{}) // the end of ctx closes the connection, which ends HandleGame
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-done:
		}
	}()

	// JoinGame
	err := c.HandleGame() // never nil
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func onDeath() error {
	log.Println("Died and Respawned")
	// If we exclude Respawn(...) then the player won't press the "Respawn" button upon death
	return p.Respawn()
}

func onGameStart() error {
	log.Println("Game start")

	watch = make(chan time.Time, 1)
	go watchDog(running)

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
	// A bite is the bobber's splash, matched by name: a sound's registry id moves between versions (609 in
	// an older one, 644 in 26.3), so a number here stops recognising the bite.
	if name, _ := soundid.GetSoundNameByID(soundid.SoundID(id)); name == "entity.fishing_bobber.splash" {
		if err := UseItem(types.InteractionHandMainHand); err != nil { // retrieve
			return err
		}
		log.Println("gra~")
		time.Sleep(time.Millisecond * 300)
		if err := UseItem(types.InteractionHandMainHand); err != nil { // throw
			return err
		}
		select {
		case watch <- time.Now():
		default: // (one already told)
		}
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

// watchDog casts again when no fish has bitten for timeout seconds, till ctx ends.
func watchDog(ctx context.Context) {
	to := time.NewTimer(time.Second * timeout)
	defer to.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-watch:
		case <-to.C:
			// with the bobber out a use reels it in, so casting again is two: one use alone would leave
			// no line in the water till the next timeout
			log.Println("rethrow")
			err := UseItem(types.InteractionHandMainHand) // reel in
			if err == nil {
				time.Sleep(time.Millisecond * 300)
				err = UseItem(types.InteractionHandMainHand) // cast
			}
			if err != nil {
				log.Print("rethrow: ", err) // the connection is gone
				return
			}
		}
		to.Reset(time.Second * timeout)
	}
}
