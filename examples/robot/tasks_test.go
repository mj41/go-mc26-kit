package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestGatePause: a pause holds the order at its next safe point, resume
// lets it on; a pause the order does not reach in time is no hold.
func TestGatePause(t *testing.T) {
	var g gate
	stepped := make(chan bool)
	go func() {
		for i := 0; i < 3; i++ {
			time.Sleep(20 * time.Millisecond) // a step between safe points
			stepped <- g.point()
		}
	}()
	if !g.pause(time.Second) {
		t.Fatal("pause: the order not held at its safe point")
	}
	select {
	case <-stepped:
		t.Fatal("the order went on while paused")
	case <-time.After(100 * time.Millisecond):
	}
	g.resume()
	if held := <-stepped; !held {
		t.Fatal("the point it was held at says it was not held")
	}
	if held := <-stepped; held {
		t.Fatal("a point after the resume says it was held")
	}
	<-stepped

	// none at a safe point within the time: not held, and released
	var h gate
	if h.pause(30 * time.Millisecond) {
		t.Fatal("pause with no order: held")
	}
	h.release()
	done := make(chan bool)
	go func() { done <- h.point() }()
	select {
	case held := <-done:
		if held {
			t.Fatal("released, still held")
		}
	case <-time.After(time.Second):
		t.Fatal("released, the order still waits")
	}
}

// TestStopEndsOrder: stop given while an order runs ends it at its next
// safe point; the next order begins unstopped.
func TestStopEndsOrder(t *testing.T) {
	r := &robot{}
	r.orderStops.Store(r.stops.Load()) // the order begins
	if err := r.orderOver(); err != nil {
		t.Fatalf("a new order is over: %v", err)
	}
	r.stops.Add(1) // stop
	if err := r.orderOver(); !errors.Is(err, errStopped) {
		t.Fatalf("stopped order: %v, want errStopped", err)
	}
	r.orderStops.Store(r.stops.Load()) // the next order
	if err := r.orderOver(); err != nil {
		t.Fatalf("the next order is over: %v", err)
	}
}

// TestGateSecondCaller: while the order is held at its safe point, the
// reflex round's own safe points (its walks, its digs) are not held.
func TestGateSecondCaller(t *testing.T) {
	var g gate
	held := make(chan bool, 1)
	go func() { // the order, its safe points one after another
		for !g.point() {
			time.Sleep(5 * time.Millisecond)
		}
		held <- true
	}()
	if !g.pause(time.Second) {
		t.Fatal("the order not held")
	}
	done := make(chan bool)
	go func() { done <- g.point() }() // the reflex round's walk
	select {
	case h := <-done:
		if h {
			t.Fatal("the reflex's own safe point was held")
		}
	case <-time.After(time.Second):
		t.Fatal("the reflex's own safe point waits for the order it paused")
	}
	g.resume()
	<-held
}

// TestStuckRetry: an order the watchdog stopped (stuckStop, stops) runs once
// again; its second answer is the order's.
func TestStuckRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &robot{ctx: ctx, queue: make(chan task, 1)}
	go r.runOrders()
	calls := 0
	cmd := func(r *robot, _ []string) (string, error) {
		calls++
		if calls == 1 {
			r.stuckStop.Store(true) // the watchdog, mid-order
			r.stops.Add(1)
			_, err := r.safePoint()
			return "", fmt.Errorf("step 31: %w", err)
		}
		return "done", nil
	}
	got := make(chan string, 1)
	r.queue <- task{words: []string{"descend"}, src: source{kind: "console"}, cmd: cmd, reply: func(a string, err error) {
		got <- fmt.Sprint(a, err)
	}}
	select {
	case a := <-got:
		if calls != 2 || a != "done<nil>" {
			t.Fatalf("calls %d, answer %q: want the order once again, done", calls, a)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no answer")
	}
}
