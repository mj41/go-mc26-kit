package path

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// gatedWorld is a flat world whose far blocks (x over 5) are not read till the gate is opened: a search that
// leaves the start's neighbourhood holds there, as a slow one does (SEARCH.md).
type gatedWorld struct {
	testWorld
	gate chan struct{}
	once sync.Once
}

func (g *gatedWorld) BlockAt(p world.BlockPos) (block.StateID, bool) {
	if p.X > 5 {
		<-g.gate
	}
	return g.testWorld.BlockAt(p)
}

func (g *gatedWorld) open() { g.once.Do(func() { close(g.gate) }) }

func searchWalker(t *testing.T) (*Walker, *gatedWorld, *basic.Player, *control.State) {
	t.Helper()
	gw := &gatedWorld{testWorld: testWorld{}, gate: make(chan struct{})}
	t.Cleanup(gw.open) // (a goroutine left holding would end the bubble in a deadlock)
	bp := basic.NewPlayer(bot.NewClient(), basic.DefaultSettings, basic.EventsListener{})
	bp.SetPosition(basic.Position{X: 0.5, Y: -60, Z: 0.5})
	return &Walker{Graph: &Graph{World: gw, Tags: ladderTags{}}}, gw, bp, &control.State{}
}

// pathEnd is where the walker's path ends (the zero BlockPos and false for none).
func pathEnd(w *Walker) (world.BlockPos, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.path) == 0 {
		return world.BlockPos{}, false
	}
	return w.path[len(w.path)-1].Pos, true
}

// within is whether f returns within d (real time: it guards the failure path, a passing run returns at once).
func within(d time.Duration, f func()) bool {
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// The search does not run under the lock the walker's readers take: Think returns, Status answers while the
// search is held, and the path is taken by the next Think once it ends. Before, Think held the mutex through the
// search and every Status, Goto and NoPath waited for it (a failed search of ~64,000 places took ~4 s on a bench:
// rounds of 8 s).
func TestStatusDoesNotWaitOnASearch(t *testing.T) {
	w, gw, bp, s := searchWalker(t)
	w.Goto(world.BlockPos{X: 20, Y: -60, Z: 0})
	if !within(2*time.Second, func() { w.Think(s, bp) }) {
		t.Fatal("Think did not return while the search runs")
	}
	var st string
	if !within(2*time.Second, func() { st = w.Status() }) {
		t.Fatal("Status waited for the search (the walker's lock is held through it)")
	}
	if st != "walking" {
		t.Errorf("status while searching: %q", st)
	}
	if _, ok := pathEnd(w); ok {
		t.Fatal("a path before the search ended")
	}
	gw.open()
	w.waitSearch()
	w.Think(s, bp) // takes the result
	if end, ok := pathEnd(w); !ok || end != (world.BlockPos{X: 20, Y: -60, Z: 0}) {
		t.Fatalf("path end %v %v, want 20 -60 0", end, ok)
	}
}

// A new Goto supersedes the search running: its result is never applied, and the search ends.
func TestGotoSupersedesASearch(t *testing.T) {
	w, gw, bp, s := searchWalker(t)
	w.Goto(world.BlockPos{X: 20, Y: -60, Z: 0})
	w.Think(s, bp) // the first search, held at the gate
	w.Goto(world.BlockPos{X: 0, Y: -60, Z: 30})
	w.Think(s, bp) // a new search, for the new goal
	gw.open()
	w.waitSearch()
	w.Think(s, bp)
	end, ok := pathEnd(w)
	if !ok || end != (world.BlockPos{X: 0, Y: -60, Z: 30}) {
		t.Fatalf("path end %v %v, want the second goal 0 -60 30", end, ok)
	}
	if st := w.Status(); st != "walking" {
		t.Errorf("status %q", st)
	}
	// the cancelled search was not the one that published: nothing waits to be taken
	w.mu.Lock()
	left, searching := w.result, w.searching
	w.mu.Unlock()
	if left != nil || searching {
		t.Errorf("a result %v or a search %v left over", left, searching)
	}
}

// A search that finds no way is published like any other: the walk ends failed, NoPath says what was found.
func TestNoPathIsPublishedByTheSearch(t *testing.T) {
	w, gw, bp, s := searchWalker(t)
	gw.open()
	gw.fill(19, -60, -1, 21, -58, 1, block.ToStateID[block.Stone{}]) // the goal's cell shut in
	gw.testWorld[world.BlockPos{X: 20, Y: -60, Z: 0}] = block.ToStateID[block.Air{}]
	gw.testWorld[world.BlockPos{X: 20, Y: -59, Z: 0}] = block.ToStateID[block.Air{}]
	w.MaxNodes = 4000
	w.Goto(world.BlockPos{X: 20, Y: -60, Z: 0})
	w.Think(s, bp)
	w.waitSearch()
	if st := w.Status(); st != "walking" {
		t.Fatalf("status before the result is taken: %q", st)
	}
	w.Think(s, bp)
	if st := w.Status(); !strings.HasPrefix(st, "failed: path: no way to the goal") {
		t.Fatalf("status %q", st)
	}
	if _, ok := w.NoPath(); !ok {
		t.Error("NoPath not set")
	}
}
