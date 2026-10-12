package bot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/microsoft"
	"github.com/mj41/go-mc26/chat"
)

// The server-list ping: what the multiplayer screen shows of a server. The fake server here stands in for
// one; any server's address does ("localhost:25565").
func ExamplePingAndList() {
	s, err := fakeserver.Listen()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer s.Close()

	resp, _, err := bot.PingAndList(s.Addr) // the status JSON and the round trip
	if err != nil {
		fmt.Println(err)
		return
	}
	var status struct {
		Description chat.Message `json:"description"`
	}
	if err := json.Unmarshal(resp, &status); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(status.Description.ClearString())
	// Output: a test's server
}

// An offline-mode server (online-mode=false) needs only a name. The fake server here stands in for one
// ("localhost:25565"); examples/minimal is this with chat, teleports and a clean exit on a kick or Ctrl-C.
func ExampleClient_JoinServer_offline() {
	s, err := fakeserver.Listen()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer s.Close()

	c := bot.NewClient()
	c.Auth.Name = "Steve"

	// Required: basic.Player answers the server's keep-alives (without it the
	// server drops the client) and reports the game's start, a kick, health and
	// death.
	basic.NewPlayer(c, basic.DefaultSettings, basic.EventsListener{
		GameStart: func() error {
			fmt.Println("joined")
			return c.Close() // this example leaves at once; a bot plays here
		},
	})

	if err := c.JoinServer(s.Addr); err != nil {
		fmt.Println(err)
		return
	}
	// HandleGame reads and handles packets until the connection ends: here, the bot's own Close.
	_ = c.HandleGame()
	fmt.Println("left")
	// Output:
	// joined
	// left
}

// An online-mode server needs a Microsoft account: package microsoft logs it in
// (a code to enter in a browser the first time, the cached login after that) and
// its result fills Auth. Compiled, not run by the tests: it needs an account.
func ExampleClient_JoinServer_online() {
	access, err := microsoft.AuthenticateDeviceCode(context.Background(), microsoft.Options{
		OnDeviceCode: func(code microsoft.DeviceCode) { fmt.Println(code.Message) },
	})
	if err != nil {
		log.Fatal(err)
	}

	c := bot.NewClient()
	c.Auth = bot.Auth{
		Name: access.Profile.Name,
		UUID: access.Profile.ID,
		AsTk: access.AccessToken(),
	}
	basic.NewPlayer(c, basic.DefaultSettings, basic.EventsListener{})

	if err := c.JoinServer("localhost:25565"); err != nil {
		log.Fatal(err)
	}
	log.Fatal(c.HandleGame())
}
