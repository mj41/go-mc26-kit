package memnet

import (
	"io"
	"testing"
	"testing/synctest"
	"time"
)

// What one end writes the other reads, in order, with no limit on what is in flight (both ends may write
// before either reads: net.Pipe would block them); a close is EOF after what was written.
func TestPairBuffersBothWays(t *testing.T) {
	a, b := Pair("a", "b")
	big := make([]byte, 1<<20)
	if _, err := a.Write(big); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(a, got); err != nil || string(got) != "hello" {
		t.Fatalf("a read %q %v", got, err)
	}
	n, err := io.CopyN(io.Discard, b, int64(len(big)))
	if err != nil || n != int64(len(big)) {
		t.Fatalf("b read %d %v", n, err)
	}
	a.Close()
	if _, err := b.Read(got); err != io.EOF {
		t.Errorf("after the other end closed: %v, want EOF", err)
	}
	if _, err := a.Read(got); err == nil {
		t.Error("a closed end read")
	}
}

// A goroutine blocked reading is durably blocked: synctest.Wait returns with it parked, and a write wakes it.
func TestReaderIsDurablyBlocked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := Pair("a", "b")
		got := make(chan string, 1)
		go func() {
			buf := make([]byte, 4)
			n, _ := b.Read(buf)
			got <- string(buf[:n])
		}()
		synctest.Wait() // would hang for a loopback socket
		a.Write([]byte("ping"))
		synctest.Wait()
		select {
		case s := <-got:
			if s != "ping" {
				t.Errorf("read %q", s)
			}
		default:
			t.Error("the reader was not woken")
		}
		a.Close()
		b.Close()
	})
}

// A read deadline falls on the bubble's clock: a blocked read ends with a timeout when virtual time reaches it,
// not before and not by the machine's speed.
func TestReadDeadlineOnTheBubblesClock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := Pair("a", "b")
		a.SetReadDeadline(time.Now().Add(20 * time.Second))
		start := time.Now()
		_, err := a.Read(make([]byte, 1))
		te, ok := err.(interface{ Timeout() bool })
		if !ok || !te.Timeout() {
			t.Fatalf("read: %v, want a timeout", err)
		}
		if d := time.Since(start); d != 20*time.Second {
			t.Errorf("the read ended after %v of virtual time, want 20 s", d)
		}
		a.Close()
	})
}
