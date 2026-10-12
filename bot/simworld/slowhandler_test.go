package simworld

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// A handler that waits half a minute (a slow consumer: a lock held through a long round, a full channel) does not
// stop the client from answering the keep-alives, nor let its read deadline fall: the answer is the connection
// reader's, not HandleGame's (bot.Events.AddReadHook). On the bubble's clock, the server sends a keep-alive every 15
// s; with the handler stuck for 40 s three come and are answered, and the connection is still there at the end.
func TestSlowHandlerDoesNotStopTheKeepAlive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		c := bot.NewClient()
		c.Auth.Name = "Tester"
		basic.NewPlayer(c, basic.DefaultSettings, basic.EventsListener{})
		release := make(chan struct{})
		c.Events.AddListener(bot.PacketHandler{ID: packetid.ClientboundPlaySetTime, F: func(pk.Packet) error {
			<-release // the consumer that does not come
			return nil
		}})
		if err := s.Join(c); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		ended := make(chan error, 1)
		go func() {
			for {
				if err := c.HandleGame(); err != nil {
					ended <- err
					return
				}
			}
		}()
		t.Cleanup(func() { cancel(); close(release); _ = c.Close() })
		if err := s.Joined(5 * time.Second); err != nil {
			t.Fatal(err)
		}
		if err := s.Send(play.SetTime{GameTime: 1}); err != nil { // the handler is stuck on this one
			t.Fatal(err)
		}
		before := len(s.Got(packetid.ServerboundPlayKeepAlive))
		start := time.Now()
		for i := 1; i <= 3; i++ {
			time.Sleep(15 * time.Second)
			if err := s.Send(play.ClientboundKeepAlive{ID: pk.Long(i)}); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
			if got := len(s.Got(packetid.ServerboundPlayKeepAlive)) - before; got != i {
				t.Errorf("keep-alive %d (at %v): %d answered, want %d", i, time.Since(start), got, i)
			}
		}
		select {
		case err := <-ended:
			t.Fatalf("the connection ended at %v with the handler stuck: %v", time.Since(start), err)
		default:
		}
		_ = ctx
	})
}
