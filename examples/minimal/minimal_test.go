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
)

// minimal plays its part against a fake server in a bubble: it joins, prints a player's "hi" and answers it,
// and the server's kick ends it with the reason — what the example says it does (examples/README.md).
func TestMinimal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		out := logTo(t)
		s := fakeserver.StartMem(t)
		ended := make(chan error, 1)
		go func() { ended <- run(t.Context(), s.Addr, "Minimal", bot.JoinOptions{MCDialer: s.Dialer}) }()
		if err := s.Joined(5 * time.Second); err != nil {
			t.Fatal(err)
		}
		synctest.Wait() // the bot is in, nothing left to read

		if err := s.Chat("Alice", "hi"); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		if said, err := s.Said(ctx, 1); err != nil || said != "hello Alice" {
			t.Fatalf("the bot said %q (%v), want \"hello Alice\"", said, err)
		}
		if err := s.Kick("that's all"); err != nil {
			t.Fatal(err)
		}
		if err := <-ended; err != nil {
			t.Fatalf("run after the kick: %v", err)
		}
		for _, want := range []string{"Minecraft ", "<Alice> hi", "kicked: that's all"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("the output has no %q:\n%s", want, out)
			}
		}
	})
}

// Ctrl-C (the end of run's context) leaves the server cleanly.
func TestMinimalInterrupted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		out := logTo(t)
		s := fakeserver.StartMem(t)
		ctx, interrupt := context.WithCancel(t.Context())
		ended := make(chan error, 1)
		go func() { ended <- run(ctx, s.Addr, "Minimal", bot.JoinOptions{MCDialer: s.Dialer}) }()
		if err := s.Joined(5 * time.Second); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		interrupt()
		if err := <-ended; err != nil {
			t.Fatalf("run after Ctrl-C: %v", err)
		}
		if !strings.Contains(out.String(), "left the server") {
			t.Errorf("the output has no \"left the server\":\n%s", out)
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
