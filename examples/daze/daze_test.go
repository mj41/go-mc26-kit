package main

import (
	"bytes"
	"context"
	"io"
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

// daze plays its part against a fake server in a bubble, the way mc26's end-to-end run drives it against a
// vanilla one: it joins and says hello, a console line goes out as chat, a player's message is printed, the
// chunks are loaded, and the server's kick ends it (examples/README.md).
func TestDaze(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		out := logTo(t)
		s := fakeserver.StartMem(t)
		console, typed := io.Pipe()
		defer typed.Close() // ends the console's reader
		ended := make(chan error, 1)
		go func() { ended <- run(s.Addr, bot.Auth{Name: "Daze"}, bot.JoinOptions{MCDialer: s.Dialer}, console) }()
		if err := s.Joined(5 * time.Second); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		if said, err := s.Said(ctx, 1); err != nil || said != "Hello, world" {
			t.Fatalf("the bot's first chat %q (%v), want \"Hello, world\"", said, err)
		}

		if _, err := io.WriteString(typed, "typed on the console\n"); err != nil {
			t.Fatal(err)
		}
		if said, err := s.Said(ctx, 2); err != nil || said != "typed on the console" {
			t.Fatalf("the console line went out as %q (%v)", said, err)
		}
		if err := s.Chat("Alice", "hi"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait() // the message read and printed
		if err := s.Kick("that's all"); err != nil {
			t.Fatal(err)
		}
		if err := <-ended; err != nil {
			t.Fatalf("run after the kick: %v", err)
		}
		// the lines mc26's end-to-end run waits for, among them
		for _, want := range []string{"Login success", "Game start", "Load chunk", "Player: <Alice> hi", "Disconnect, reason: that's all"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("the output has no %q:\n%s", want, out)
			}
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
