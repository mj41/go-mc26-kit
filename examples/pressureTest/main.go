// This example is used to apply pressure test on the server: it logs a lot of clients in, each logging in
// again 3 s after it is out, till Ctrl-C. It tests the login handling of a server (the kit's server package,
// or any other implementation). The number of clients is the -number flag.
//
//	go run ./examples/pressureTest -address localhost:25565 -number 100
//
// main_test.go runs it with three clients against a fake server (examples/README.md).
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26/chat"
)

var (
	address = flag.String("address", "127.0.0.1", "The server address")
	number  = flag.Int("number", 1023, "The number of clients")
)

func main() {
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	run(ctx, *address, *number, bot.JoinOptions{})
}

// run logs n clients in to address, each again 3 s after it is out, and returns when ctx ends and every
// client is out. opts is how they dial: the zero value for a real server (a test gives a fake server's).
func run(ctx context.Context, address string, n int, opts bot.JoinOptions) {
	var wg sync.WaitGroup
	for i := 0; i < n && ctx.Err() == nil; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for ctx.Err() == nil {
				newIndividual(i, "Player"+strconv.Itoa(i)).run(ctx, address, opts)
				select {
				case <-ctx.Done():
				case <-time.After(3 * time.Second):
				}
			}
		}(i)
		time.Sleep(time.Millisecond)
	}
	wg.Wait()
}

type individual struct {
	id     int
	client *bot.Client
	player *basic.Player
}

func newIndividual(id int, name string) (i *individual) {
	i = new(individual)
	i.id = id
	i.client = bot.NewClient()
	i.client.Auth.Name = name
	i.player = basic.NewPlayer(i.client, basic.DefaultSettings, basic.EventsListener{
		GameStart:  i.onGameStart,
		Disconnect: onDisconnect,
	})
	return
}

func (i *individual) run(ctx context.Context, address string, opts bot.JoinOptions) {
	// Login
	err := i.client.JoinServerWithOptions(address, opts)
	if err != nil {
		log.Printf("[%d]Login fail: %v", i.id, err)
		return
	}
	defer i.client.Close()
	log.Printf("[%d]Login success", i.id)

	done := make(chan struct{}) // the end of ctx closes the connection, which ends HandleGame
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			i.client.Close()
		case <-done:
		}
	}()

	// JoinGame
	err = i.client.HandleGame() // never nil
	log.Printf("[%d] Handle game error: %v", i.id, err)
}

func (i *individual) onGameStart() error {
	log.Printf("[%d]Game start", i.id)
	return nil
}

func onDisconnect(reason chat.Message) error {
	return bot.DisconnectErr(reason)
}
