package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/data/packetid"
)

// autofish against a fake server that plays the fishing, in a bubble: it casts, reels in at each bite's
// splash and casts again — three fish in a few seconds of virtual time.
func TestAutofish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		out := logTo(t)
		s := fakeserver.StartMem(t)
		s.Fishing = &fakeserver.Fishing{Rod: true}
		ctx, stop := context.WithCancel(t.Context())
		ended := make(chan error, 1)
		go func() { ended <- run(ctx, s.Addr, "Fisher", bot.JoinOptions{MCDialer: s.Dialer}) }()
		for start := time.Now(); s.Fishing.Caught() < 3; time.Sleep(100 * time.Millisecond) {
			if time.Since(start) > 2*time.Minute {
				t.Fatalf("%d fish in two minutes of fishing, want 3:\n%s", s.Fishing.Caught(), out)
			}
		}
		stop()
		if err := <-ended; err != nil {
			t.Fatalf("run: %v", err)
		}
		if n := strings.Count(out.String(), "gra~"); n < 3 {
			t.Errorf("%d reels in the output, want 3 at least:\n%s", n, out)
		}
	})
}

// With no bite in 45 s, the watchdog casts again: it reels the bobber in and casts it, so a line is in the
// water again, not left out till the next timeout.
func TestAutofishRethrow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		out := logTo(t)
		s := fakeserver.StartMem(t)
		s.Fishing = &fakeserver.Fishing{Rod: true, BiteAfter: time.Hour} // nothing bites
		s.KeepAlive = 15 * time.Second                                   // a long wait, as a real server keeps it
		ctx, stop := context.WithCancel(t.Context())
		ended := make(chan error, 1)
		go func() { ended <- run(ctx, s.Addr, "Fisher", bot.JoinOptions{MCDialer: s.Dialer}) }()
		if err := s.Joined(5 * time.Second); err != nil {
			t.Fatal(err)
		}
		time.Sleep(46 * time.Second) // past the watchdog's 45
		synctest.Wait()
		// the first cast, then the rethrow's reel and cast
		if n := len(s.Got(packetid.ServerboundPlayUseItem)); n != 3 {
			t.Errorf("%d uses of the rod after 46 s with no bite, want 3 (cast; reel, cast)", n)
		}
		if !strings.Contains(out.String(), "rethrow") {
			t.Errorf("no rethrow in the output:\n%s", out)
		}
		stop()
		if err := <-ended; err != nil {
			t.Fatalf("run: %v", err)
		}
	})
}

// The rod comes after the game start, as the end-to-end run gives it (/give): the cast at the game start goes out with an empty
// hand and a real server ignores it, so no bobber is out when the watchdog first fires. It must cast then (one use), not
// "reel in, cast" — which is a cast and a reel 300 ms later, and no line in the water for good. With the line out the next
// bite is reeled in and the fish caught.
func TestAutofishRodGivenAfterTheStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		out := logTo(t)
		s := fakeserver.StartMem(t)
		s.Fishing = &fakeserver.Fishing{BiteAfter: 5 * time.Second}
		s.KeepAlive = 15 * time.Second
		ctx, stop := context.WithCancel(t.Context())
		ended := make(chan error, 1)
		go func() { ended <- run(ctx, s.Addr, "Fisher", bot.JoinOptions{MCDialer: s.Dialer}) }()
		if err := s.Joined(5 * time.Second); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if n := len(s.Got(packetid.ServerboundPlayUseItem)); n != 1 || s.Fishing.Out() {
			t.Fatalf("the cast at the game start: %d uses, a bobber out %v (no rod: nothing out)", n, s.Fishing.Out())
		}
		s.Fishing.GiveRod(s.Players()[0])
		time.Sleep(46 * time.Second) // past the watchdog's 45
		synctest.Wait()
		if !s.Fishing.Out() {
			t.Fatalf("no line in the water after the rethrow with the rod given:\n%s", out)
		}
		time.Sleep(10 * time.Second) // the bite, reeled in
		synctest.Wait()
		if s.Fishing.Caught() < 1 || !strings.Contains(out.String(), "gra~") {
			t.Errorf("%d fish caught after the first bite:\n%s", s.Fishing.Caught(), out)
		}
		stop()
		if err := <-ended; err != nil {
			t.Fatalf("run: %v", err)
		}
	})
}

// logTo sends the log package's output (the example's) to a buffer till the test ends.
func logTo(t *testing.T) *syncBuffer {
	var b syncBuffer
	log.SetOutput(&b)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &b
}

// syncBuffer is a bytes.Buffer the log package writes while the test reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
