package main

import (
	"sync"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// The walker's search asks fieldWaterAt (the graph's Avoid) from a goroutine of its own, on a real clock, while the
// robot's jobs write the field and the cells being dug: what it reads must be a published whole, never r.field or
// r.avoid themselves. Under -race this is the proof; before, the search read both unlocked.
func TestFieldWaterAtWhileTheFieldChanges(t *testing.T) {
	r := &robot{}
	c := world.BlockPos{X: 10, Y: 64, Z: 10}
	pool := fieldPool(c)

	// the answer for what is published
	r.field = &c
	r.publishAvoid()
	if !r.fieldWaterAt(pool[0]) || r.fieldWaterAt(world.BlockPos{X: 0, Y: 64, Z: 0}) {
		t.Fatalf("the field's pool: %v, a cell far off: %v", r.fieldWaterAt(pool[0]), r.fieldWaterAt(world.BlockPos{}))
	}
	r.field = nil
	r.avoid = map[world.BlockPos]bool{{X: 1, Y: 2, Z: 3}: true}
	r.publishAvoid()
	if !r.fieldWaterAt(world.BlockPos{X: 1, Y: 2, Z: 3}) || r.fieldWaterAt(pool[0]) {
		t.Fatalf("a cell being dug: %v, the pool of a field forgotten: %v", r.fieldWaterAt(world.BlockPos{X: 1, Y: 2, Z: 3}), r.fieldWaterAt(pool[0]))
	}
	r.avoid = nil
	r.publishAvoid()
	if r.fieldWaterAt(world.BlockPos{X: 1, Y: 2, Z: 3}) {
		t.Fatal("a cell no longer dug is still avoided")
	}

	// the writer is the robot's job (a field made, forgotten, cells dug); the reader the search's goroutine
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			f := world.BlockPos{X: i % 7, Y: 64, Z: i % 5}
			r.field = &f
			r.publishAvoid()
			r.avoid = map[world.BlockPos]bool{{X: i, Y: 64, Z: 0}: true}
			r.publishAvoid()
			r.field, r.avoid = nil, nil
			r.publishAvoid()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = r.fieldWaterAt(world.BlockPos{X: i % 9, Y: 64, Z: i % 6})
		}
	}()
	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}
