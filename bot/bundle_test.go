package bot_test

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// tracker is a client that tracks entities and keeps playing past a handler's error (as a program does,
// examples/minimal), the errors kept.
type tracker struct {
	c    *bot.Client
	seen *entities.Manager

	mu   sync.Mutex
	errs []error
}

func track(t *testing.T, s *fakeserver.Server) *tracker {
	t.Helper()
	tr := &tracker{c: bot.NewClient()}
	tr.c.Auth.Name = "Tracker"
	basic.NewPlayer(tr.c, basic.DefaultSettings, basic.EventsListener{})
	tr.seen = entities.New(tr.c, entities.Events{})
	if err := s.Join(tr.c); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			err := tr.c.HandleGame()
			var handlerErr bot.PacketHandlerError
			if !errors.As(err, &handlerErr) {
				return // the connection's end
			}
			tr.mu.Lock()
			tr.errs = append(tr.errs, err)
			tr.mu.Unlock()
		}
	}()
	t.Cleanup(func() { _ = tr.c.Close(); <-done })
	if err := s.Joined(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	synctest.Wait()
	return tr
}

func zombie(id int32) play.AddEntity {
	return play.AddEntity{ID: pk.VarInt(id), Type: pk.VarInt(slices.Index(registryid.EntityType, "minecraft:zombie")), X: 3, Y: 64, Z: 3}
}

// A bundle's packets are handled together when its closing delimiter comes, as the game's client handles them
// (in one go, a mob's spawn and its data never seen apart): not one of them before.
func TestBundleHandledAtItsEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		tr := track(t, s)
		for _, p := range []fakeserver.Clientbound{play.BundleDelimiter{}, zombie(100), zombie(101)} {
			if err := s.Send(p); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		if _, ok := tr.seen.Get(100); ok {
			t.Fatal("a bundle's entity handled before the bundle ended")
		}
		if err := s.Send(play.BundleDelimiter{}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		for _, id := range []int32{100, 101} {
			if e, ok := tr.seen.Get(id); !ok || e.Type != "minecraft:zombie" {
				t.Errorf("entity %d after the bundle: %v %+v, want a zombie", id, ok, e)
			}
		}
	})
}

// A packet of a bundle that its handler cannot read does not lose the bundle's others: each is handled, as it
// would be outside a bundle, and the error comes back after them.
func TestBundleAPacketFailsTheOthersHandled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		tr := track(t, s)
		broken := pk.Packet{ID: int32(play.AddEntity{}.PacketID()), Data: []byte{0x80}} // a VarInt cut short
		if err := s.Send(play.BundleDelimiter{}); err != nil {
			t.Fatal(err)
		}
		if err := s.SendRaw(broken); err != nil {
			t.Fatal(err)
		}
		for _, p := range []fakeserver.Clientbound{zombie(200), play.BundleDelimiter{}} {
			if err := s.Send(p); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		if _, ok := tr.seen.Get(200); !ok {
			t.Error("the zombie after a broken packet in its bundle was lost")
		}
		tr.mu.Lock()
		defer tr.mu.Unlock()
		if len(tr.errs) != 1 {
			t.Errorf("%d handler errors, want the broken packet's one: %v", len(tr.errs), tr.errs)
		}
	})
}
