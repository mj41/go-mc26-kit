package bot_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/configuration"
	"github.com/mj41/go-mc26/protocol/play"
)

// The server takes a playing bot back to the configuration phase (StartConfiguration) and on to play again:
// the bot acknowledges, then speaks the configuration phase only — none of its play packets (its controller's
// movement and tick ends) while the server reads configuration — till it acknowledges the finish, and then
// plays on.
func TestReconfiguration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		var mu sync.Mutex
		var sent []packetid.ServerboundPacketID // every packet the bot sends, in order
		s.Hook = func(p fakeserver.Packet) {
			mu.Lock()
			defer mu.Unlock()
			sent = append(sent, p.ID)
		}
		b := fakeserver.Join(t, s, "Bot")
		if err := b.Loaded(5 * time.Second); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		if err := s.Send(play.StartConfiguration{}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.WaitFor(ctx, packetid.ServerboundPlayConfigurationAcknowledged, 1); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		acked := len(sent) // the acknowledgement is the last of the play phase
		mu.Unlock()

		time.Sleep(time.Second) // a second of the client's ticks, in the configuration phase
		// (the fake server records ids as numbers: a play packet earlier can have the finish's number)
		finished := packetid.ServerboundPacketID(packetid.ServerboundConfigurationFinishConfiguration)
		earlier := len(s.Got(finished))
		finish := configuration.ClientboundFinishConfiguration{}
		if err := s.SendRaw(pk.Marshal(finish.PacketID(), finish)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.WaitFor(ctx, finished, earlier+1); err != nil {
			t.Fatal(err)
		}

		mu.Lock()
		during := append([]packetid.ServerboundPacketID(nil), sent[acked:]...)
		mu.Unlock()
		// what came after the finish is play again (the controller resuming): nothing may come before it
		at := slices.Index(during, finished)
		if at < 0 {
			t.Fatalf("after the acknowledgement the bot sent %v, and no configuration finish", during)
		}
		if at > 0 {
			t.Errorf("in the configuration phase the bot sent %d packets before its finish (ids %v): the play phase's, to a server reading configuration", at, during[:at])
		}

		// and play again: the controller's ticks resume
		mu.Lock()
		before := len(sent)
		mu.Unlock()
		time.Sleep(time.Second)
		mu.Lock()
		after := len(sent)
		mu.Unlock()
		if after == before {
			t.Error("no packet from the bot in a second of play after the configuration")
		}
	})
}
