package fakeserver_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/msg"
	"github.com/mj41/go-mc26-kit/bot/playerlist"
	"github.com/mj41/go-mc26-kit/offline"
)

// client is a client of the kit with what a multi-player test looks at: its player list, the entities it
// sees, the player messages it got.
type client struct {
	c       *bot.Client
	chat    *msg.Manager
	players *playerlist.PlayerList
	seen    *entities.Manager

	mu    sync.Mutex
	heard []msg.PlayerChat
}

// join joins a client named name to s and runs it till the test ends (or Close).
func join(t *testing.T, s *fakeserver.Server, name string) (*client, error) {
	t.Helper()
	cl := &client{c: bot.NewClient()}
	cl.c.Auth.Name = name
	p := basic.NewPlayer(cl.c, basic.DefaultSettings, basic.EventsListener{})
	cl.players = playerlist.New(cl.c)
	cl.seen = entities.New(cl.c, entities.Events{})
	cl.chat = msg.New(cl.c, p, cl.players, msg.EventsHandler{PlayerChat: func(m msg.PlayerChat) error {
		cl.mu.Lock()
		defer cl.mu.Unlock()
		cl.heard = append(cl.heard, m)
		return nil
	}})
	if err := s.Join(cl.c); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() { _ = cl.c.HandleGame(); close(done) }()
	t.Cleanup(func() { _ = cl.c.Close(); <-done })
	return cl, nil
}

func (cl *client) messages() []string {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	var out []string
	for _, m := range cl.heard {
		out = append(out, m.Name+": "+m.Content)
	}
	return out
}

// Two players on one server: each told of the other (in its player list, its entity seen), and what one says
// in chat heard by both, from it.
func TestTwoPlayers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.MaxPlayers = 2
		alice, err := join(t, s, "Alice")
		if err != nil {
			t.Fatal(err)
		}
		bob, err := join(t, s, "Bob")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.WaitPlayers(2, 5*time.Second); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()

		for _, c := range []struct {
			who    *client
			name   string
			other  string
			entity uint32
		}{{alice, "Alice", "Bob", 2}, {bob, "Bob", "Alice", 1}} {
			if _, ok := c.who.players.ByName(c.other); !ok {
				t.Errorf("%s's player list has no %s", c.name, c.other)
			}
			e, ok := c.who.seen.ByUUID(offline.NameToUUID(c.other))
			if !ok || uint32(e.ID) != c.entity {
				t.Errorf("%s sees %s's entity: %v %+v, want entity id %d", c.name, c.other, ok, e, c.entity)
			}
		}
		if got := []string{s.Players()[0].Name, s.Players()[1].Name}; got[0] != "Alice" || got[1] != "Bob" {
			t.Errorf("the players in join order: %v", got)
		}

		if err := alice.chat.SendMessage("hi Bob"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		for _, c := range []*client{alice, bob} {
			if got := c.messages(); len(got) != 1 || got[0] != "Alice: hi Bob" {
				t.Errorf("heard %v, want [Alice: hi Bob]", got)
			}
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if said, err := s.Player("Alice").Said(ctx, 1); err != nil || said != "hi Bob" {
			t.Errorf("the server's record of Alice's chat: %q, %v", said, err)
		}
	})
}

// A player who leaves is taken out of the others' player list and world.
func TestPlayerLeaves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.MaxPlayers = 2
		alice, err := join(t, s, "Alice")
		if err != nil {
			t.Fatal(err)
		}
		bob, err := join(t, s, "Bob")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.WaitPlayers(2, 5*time.Second); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		_ = bob.c.Close()
		synctest.Wait()
		if _, ok := alice.players.ByName("Bob"); ok {
			t.Error("Bob left and is still in Alice's player list")
		}
		if _, ok := alice.seen.ByUUID(offline.NameToUUID("Bob")); ok {
			t.Error("Bob left and Alice still sees his entity")
		}
		if n := len(s.Players()); n != 1 {
			t.Errorf("%d players in play after Bob left, want 1", n)
		}
	})
}

// A client past MaxPlayers is refused at login with the vanilla reason; the players in play keep playing.
func TestServerFull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t) // MaxPlayers unset: one
		if _, err := join(t, s, "Alice"); err != nil {
			t.Fatal(err)
		}
		if err := s.Joined(5 * time.Second); err != nil {
			t.Fatal(err)
		}
		_, err := join(t, s, "Bob")
		if err == nil || !strings.Contains(err.Error(), "multiplayer.disconnect.server_full") && !strings.Contains(err.Error(), "The server is full!") {
			t.Fatalf("Bob's login: %v, want the server full", err)
		}
		if ps := s.Players(); len(ps) != 1 || ps[0].Name != "Alice" {
			t.Errorf("the players in play: %d, want Alice alone", len(ps))
		}
	})
}
