// Daze is a bot with a console: what is typed on its stdin is sent as chat
// (a line starting with / as a command). It
//   - says "Hello, world" when the game starts,
//   - prints the chat it receives: system messages, players' messages (with who
//     sent them, msg.PlayerChat) and disguised ones,
//   - keeps the player list, loads and unloads the chunks the server sends,
//     prints the inventory slots that change,
//   - respawns 5 seconds after it dies.
//
// Offline mode needs only -name; for an online-mode server, pass -uuid and -token
// (examples/microsoft prints them). mc26's end-to-end run drives this bot through
// its console and reads its output.
package main

import (
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"sync"
	"time"

	//"github.com/mattn/go-colorable"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/msg"
	"github.com/mj41/go-mc26-kit/bot/playerlist"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level"
)

var (
	address     = flag.String("address", "127.0.0.1:25565", "The server address")
	name        = flag.String("name", "Daze", "The player's name")
	playerID    = flag.String("uuid", "", "The player's UUID")
	accessToken = flag.String("token", "", "AccessToken")
)

var (
	client        *bot.Client
	player        *basic.Player
	playerList    *playerlist.PlayerList
	chatHandler   *msg.Manager
	worldManager  *world.World
	screenManager *screen.Manager
)

func main() {
	flag.Parse()
	// log.SetOutput(colorable.NewColorableStdout())
	auth := bot.Auth{Name: *name, UUID: *playerID, AsTk: *accessToken}
	if err := run(*address, auth, bot.JoinOptions{}, os.Stdin); err != nil {
		log.Fatal(err)
	}
}

// run is the bot: it joins address as auth, reads its console (lines sent as chat, or as commands with a
// leading /), and returns when the server kicks it or the connection ends. opts is how it dials: the zero
// value for a real server (daze_test.go gives a fake server's in-memory dialer).
func run(address string, auth bot.Auth, opts bot.JoinOptions, stdin io.Reader) error {
	consoleIn, consoleOnce = stdin, sync.Once{}
	client = bot.NewClient()
	client.Auth = auth
	player = basic.NewPlayer(client, basic.DefaultSettings, basic.EventsListener{
		GameStart:    onGameStart,
		Disconnect:   onDisconnect,
		HealthChange: onHealthChange,
		Death:        onDeath,
	})
	playerList = playerlist.New(client)
	chatHandler = msg.New(client, player, playerList, msg.EventsHandler{
		SystemChat:    onSystemMsg,
		PlayerChat:    onPlayerMsg,
		DisguisedChat: onDisguisedMsg,
	})
	worldManager = world.NewWorld(client, player, world.EventsListener{
		LoadChunk:   onChunkLoad,
		UnloadChunk: onChunkUnload,
	})
	screenManager = screen.NewManager(client, screen.EventsListener{
		Open:    nil,
		SetSlot: onScreenSlotChange,
		Close:   nil,
	})

	// Login
	if err := client.JoinServerWithOptions(address, opts); err != nil {
		return err
	}
	defer client.Close()
	log.Println("Login success")

	// JoinGame
	for {
		err := client.HandleGame() // never nil
		if err2 := new(bot.PacketHandlerError); errors.As(err, err2) {
			if kicked := new(bot.DisconnectErr); errors.As(err2, kicked) {
				log.Print("Disconnect, reason: ", chat.Message(*kicked))
				return nil
			}
			// print and ignore the error
			log.Print(err2)
		} else {
			return err
		}
	}
}

func onDeath() error {
	log.Println("Died and Respawned")
	// If we exclude Respawn(...) then the player won't press the "Respawn" button upon death
	go func() {
		time.Sleep(time.Second * 5)
		err := player.Respawn()
		if err != nil {
			log.Print(err)
		}
	}()
	return nil
}

func onGameStart() error {
	log.Println("Game start")
	console()
	if err := chatHandler.SendMessage("Hello, world"); err != nil {
		return err
	}
	return nil // if err isn't nil, HandleGame() will return it.
}

func onSystemMsg(c chat.Message, overlay bool) error {
	log.Printf("System: %v, Overlay: %v", c, overlay)
	return nil
}

// onPlayerMsg prints a player's message as the client shows it. m.Sender, m.Name
// and m.Content are what a bot answering it uses (examples/minimal does).
func onPlayerMsg(m msg.PlayerChat) error {
	if m.Disguised {
		return nil // onDisguisedMsg prints those
	}
	var prefix string
	if !m.Validated {
		prefix = "[Not Secure] "
	}
	log.Printf("%sPlayer: %v", prefix, m.Message)
	return nil
}

func onDisguisedMsg(msg chat.Message) error {
	log.Printf("Disguised: %v", msg)
	return nil
}

func onChunkLoad(pos level.ChunkPos) error {
	log.Println("Load chunk:", pos)
	return nil
}

func onChunkUnload(pos level.ChunkPos) error {
	log.Println("Unload chunk:", pos)
	return nil
}

func onScreenSlotChange(id, index int) error {
	if id == -2 {
		log.Printf("Slot: inventory: %v", screenManager.Inventory.Slots[index])
	} else if id == -1 && index == -1 {
		log.Printf("Slot: cursor: %v", screenManager.Cursor)
	} else {
		container, ok := screenManager.Screens[id]
		if ok {
			// Currently, only inventory container is supported
			switch container := container.(type) {
			case *screen.Inventory:
				slot := container.Slots[index]
				itemName := "nil"
				if slot.Item >= 0 && int(slot.Item) < len(registryid.Item) {
					itemName = registryid.Item[slot.Item]
				}
				log.Printf("Slot: Screen[%d].Slot[%d]: [%v] * %d | Components: +%d/-%d", id, index, itemName, slot.Count, len(slot.Components.Positive), len(slot.Components.Negative))
			}
		}
	}
	return nil
}

func onHealthChange(health float32, foodLevel int32, foodSaturation float32) error {
	log.Printf("Health: %.2f, FoodLevel: %d, FoodSaturation: %.2f", health, foodLevel, foodSaturation)
	return nil
}

func onDisconnect(reason chat.Message) error {
	// return an error value so that we can stop main loop
	return bot.DisconnectErr(reason)
}
