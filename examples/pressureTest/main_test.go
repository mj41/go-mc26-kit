package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
)

// Three clients log in to a fake server that takes three, each reaches its game start, and the end of the
// context takes them all out — what mc26's end-to-end run checks against a vanilla server.
func TestPressure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		out := logTo(t)
		s := fakeserver.StartMem(t)
		s.MaxPlayers = 3
		ctx, stop := context.WithCancel(t.Context())
		ended := make(chan struct{})
		go func() { run(ctx, s.Addr, 3, bot.JoinOptions{MCDialer: s.Dialer}); close(ended) }()
		if err := s.WaitPlayers(3, 5*time.Second); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		for i := 0; i < 3; i++ {
			for _, want := range []string{fmt.Sprintf("[%d]Login success", i), fmt.Sprintf("[%d]Game start", i)} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("the output has no %q:\n%s", want, out)
				}
			}
		}
		stop()
		<-ended
		synctest.Wait() // the server's side of each leaving
		if n := len(s.Players()); n != 0 {
			t.Errorf("%d players in play after the end, want none", n)
		}
	})
}

// A fourth client to a server that takes three is refused, and says so.
func TestPressureServerFull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		out := logTo(t)
		s := fakeserver.StartMem(t)
		s.MaxPlayers = 3
		ctx, stop := context.WithCancel(t.Context())
		ended := make(chan struct{})
		go func() { run(ctx, s.Addr, 4, bot.JoinOptions{MCDialer: s.Dialer}); close(ended) }()
		if err := s.WaitPlayers(3, 5*time.Second); err != nil {
			t.Fatal(err)
		}
		synctest.Wait() // the fourth refused, waiting its 3 s to try again
		if !strings.Contains(out.String(), "Login fail") || !strings.Contains(out.String(), "server_full") && !strings.Contains(out.String(), "The server is full!") {
			t.Errorf("no client was refused as the server is full:\n%s", out)
		}
		if n := len(s.Players()); n != 3 {
			t.Errorf("%d players in play, want 3", n)
		}
		stop()
		<-ended
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
