package fakeserver_test

import (
	"errors"
	"fmt"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/chat"
)

// A bot tried against a fake server on loopback: it joins, the server kicks it, and the kick ends the game.
// A test of a bot's own code does the same with Start (the server closed when the test ends), or StartMem
// in a testing/synctest bubble.
func ExampleListen() {
	s, err := fakeserver.Listen()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer s.Close()

	c := bot.NewClient()
	c.Auth.Name = "Steve"
	basic.NewPlayer(c, basic.DefaultSettings, basic.EventsListener{
		GameStart: func() error {
			fmt.Println("joined")
			return s.Kick("that's all")
		},
		Disconnect: func(reason chat.Message) error {
			return bot.DisconnectErr(reason) // ends HandleGame
		},
	})
	if err := s.Join(c); err != nil {
		fmt.Println(err)
		return
	}
	var kicked bot.DisconnectErr
	if err := c.HandleGame(); errors.As(err, &kicked) {
		fmt.Println("kicked:", chat.Message(kicked).ClearString())
	}
	// Output:
	// joined
	// kicked: that's all
}
