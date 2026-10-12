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
	"slices"
	"sync"
	"time"

	//"github.com/mattn/go-colorable"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/msg"
	"github.com/mj41/go-mc26-kit/bot/playerlist"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
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

	watch   chan struct{}   // a bite reeled in, told to the watchdog: it casts again once the bobber is gone
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
	resetBobber()
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

	c.Events.AddListener(soundListener, bobberAdded, bobberRemoved)

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

	watch = make(chan struct{}, 1)
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

// float is whether the bot's bobber is in the water, as the server says it: an entity of type fishing_bobber whose data
// is the bot's own entity id (its owner) was added and not yet removed. The bot does not assume it — a cast with no rod in
// the hand (the game start, before the rod is given) puts none out — it asks this before it reels in or casts.
var float struct {
	mu  sync.Mutex
	id  int32         // the bobber's entity id; 0: none out
	ch  chan struct{} // closed when the state changes (and made again)
	own int32         // the bot's entity id
}

// resetBobber is the state of a run that begins: none out, a channel made inside the run (a test's bubble owns it).
func resetBobber() {
	float.mu.Lock()
	defer float.mu.Unlock()
	float.id, float.ch = 0, make(chan struct{})
}

func floatChanged() {
	close(float.ch)
	float.ch = make(chan struct{})
}

var bobberType = slices.Index(registryid.EntityType, "minecraft:fishing_bobber")

var bobberAdded = bot.PacketHandler{ID: packetid.ClientboundPlayAddEntity, F: func(pk_ pk.Packet) error {
	var a play.AddEntity
	if err := pk_.Scan(&a); err != nil {
		return nil // (one read wrong is no reason to drop the connection)
	}
	if int(a.Type) != bobberType || int32(a.Data) != int32(p.Login.PlayerID) {
		return nil
	}
	float.mu.Lock()
	float.id = int32(a.ID)
	floatChanged()
	float.mu.Unlock()
	return nil
}}

var bobberRemoved = bot.PacketHandler{ID: packetid.ClientboundPlayRemoveEntities, F: func(pk_ pk.Packet) error {
	var r play.RemoveEntities
	if err := pk_.Scan(&r); err != nil {
		return nil
	}
	float.mu.Lock()
	defer float.mu.Unlock()
	for _, id := range r.EntityIds {
		if float.id != 0 && int32(id) == float.id {
			float.id = 0
			floatChanged()
		}
	}
	return nil
}}

// bobberOut is whether the bot's bobber is out now.
func bobberOut() bool {
	float.mu.Lock()
	defer float.mu.Unlock()
	return float.id != 0
}

// waitBobber waits (at most limit, or till ctx ends) for the bobber to be out, or gone: whether it is.
func waitBobber(ctx context.Context, out bool, limit time.Duration) bool {
	deadline := time.After(limit)
	for {
		float.mu.Lock()
		ok, ch := (float.id != 0) == out, float.ch
		float.mu.Unlock()
		if ok {
			return true
		}
		select {
		case <-ch:
		case <-deadline:
			return false
		case <-ctx.Done():
			return false
		}
	}
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
		if !bobberOut() {
			return nil // a splash that is no bite of ours: our bobber is not out
		}
		if err := UseItem(types.InteractionHandMainHand); err != nil { // retrieve
			return err
		}
		log.Println("gra~")
		// the cast again waits, in the watchdog's goroutine, for the server to say the bobber is gone: this handler
		// runs the packets, and the word is the next of them
		select {
		case watch <- struct{}{}:
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

// watchDog casts again — once the bobber is gone — after each bite reeled in, and when no fish has bitten for timeout
// seconds, till ctx ends. It asks the server's word (bobberOut), it does not assume it: a cast with no rod in the hand puts
// no bobber out, and the use that was meant to reel it in would cast.
func watchDog(ctx context.Context) {
	to := time.NewTimer(time.Second * timeout)
	defer to.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-watch:
		case <-to.C:
			log.Println("rethrow")
			if bobberOut() {
				if err := UseItem(types.InteractionHandMainHand); err != nil { // reel in
					log.Print("rethrow: ", err) // the connection is gone
					return
				}
			}
		}
		// the line out is cast only when none is: a use with the bobber out would reel it in
		if !waitBobber(ctx, false, 10*time.Second) {
			if ctx.Err() != nil {
				return
			}
			log.Println("the bobber is still out after the reel: not cast again")
		} else if err := UseItem(types.InteractionHandMainHand); err != nil { // cast
			log.Print("cast: ", err)
			return
		}
		if !to.Stop() {
			select {
			case <-to.C:
			default:
			}
		}
		to.Reset(time.Second * timeout)
	}
}
