package path

import (
	"fmt"
	"math"
	"sync"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/world"
)

// Walker walks paths: set a control.Controller's Think to its Think, then
// Goto a block or Follow something that moves.
type Walker struct {
	Graph *Graph
	// MaxNodes bounds one search (default 100000).
	MaxNodes int
	// Sneak holds shift, walking and standing still: slower, but the physics
	// keeps the player from going off an edge, pushed too (a bridge over a
	// cave, a mob's hit).
	Sneak bool
	// Sprint runs (away from what is after it).
	Sprint bool
	// Precise ends a walk at the middle of the goal's block (within 0.12),
	// sneaking the last half block, not anywhere within 0.3 of it: the body
	// (0.6 wide) then stands in that block's column alone, as before a block
	// beside it is dug. A walk to the block it stands in centres it there.
	Precise bool

	mu       sync.Mutex
	job      job
	path     []Node
	i        int
	status   string
	best     float64 // the closest the player came to the current node
	sinceBet int     // ticks since it came closer
	replans  int
	ticks    int
	release  bool // the keys of the last job are still held
}

type job struct {
	active bool
	goal   world.BlockPos
	// follow, when set, says where the target is now; the walker keeps
	// within near blocks of it until stopped.
	follow func() (x, y, z float64, ok bool)
	near   float64
}

// Goto starts walking to the block goal (a place to stand, or a block to get
// next to).
func (w *Walker) Goto(goal world.BlockPos) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.job = job{active: true, goal: goal}
	w.path, w.replans, w.status = nil, 0, "walking"
}

// Follow keeps within near blocks of where target says, until Stop.
func (w *Walker) Follow(target func() (x, y, z float64, ok bool), near float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.job = job{active: true, follow: target, near: near}
	w.path, w.replans, w.status = nil, 0, "following"
}

// Stop ends the job and lets go of the keys.
func (w *Walker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.job.active = false
	w.status = "stopped"
}

// Status is "walking", "following", "arrived", "stopped" or "failed: …".
func (w *Walker) Status() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

func (w *Walker) end(s *control.State, status string) {
	w.job.active = false
	w.status = status
	s.Input = control.Input{Shift: w.Sneak}
}

// Think is a control.Controller's Think: one tick of walking.
func (w *Walker) Think(s *control.State, bp *basic.Player) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.job.active {
		if w.Sneak { // standing still, sneak held all the same: an edge stays an edge
			s.Input = control.Input{Shift: true}
			w.release = true
			return
		}
		if w.release {
			s.Input = control.Input{}
			w.release = false
		}
		return
	}
	w.release = true
	w.ticks++
	pos := bp.Position()

	if f := w.job.follow; f != nil {
		tx, ty, tz, ok := f()
		if !ok {
			s.Input = control.Input{}
			return
		}
		if math.Hypot(tx-pos.X, tz-pos.Z) <= w.job.near && math.Abs(ty-pos.Y) < 2 {
			s.Input = control.Input{}
			w.path = nil
			return
		}
		goal := world.BlockPos{X: int(math.Floor(tx)), Y: int(math.Floor(ty + 1e-6)), Z: int(math.Floor(tz))}
		if goal != w.job.goal || w.ticks%20 == 0 {
			w.job.goal, w.path = goal, nil
		}
	}

	if w.path == nil || !w.stillThere() {
		if err := w.plan(pos); err != nil {
			if w.job.follow != nil {
				s.Input = control.Input{} // wait for the target to be reachable
				w.path = nil
				return
			}
			w.end(s, "failed: "+err.Error())
			return
		}
	}
	if w.i >= len(w.path) {
		if w.job.follow == nil {
			w.end(s, "arrived")
		} else {
			s.Input = control.Input{}
		}
		return
	}

	n := w.path[w.i]
	tx, tz := float64(n.Pos.X)+0.5, float64(n.Pos.Z)+0.5
	dist := math.Hypot(tx-pos.X, tz-pos.Z)
	dy := pos.Y - n.Floor
	near := 0.3
	last := w.Precise && w.job.follow == nil && w.i == len(w.path)-1
	if last {
		near = 0.12
	}
	if dist < near && (math.Abs(dy) < 0.6 || (n.Kind == Climb || n.Kind == Swim) && dy > -0.2) {
		w.i++
		w.best, w.sinceBet = math.MaxFloat64, 0
		if w.i >= len(w.path) {
			s.Input = control.Input{}
			return
		}
		n = w.path[w.i]
		tx, tz = float64(n.Pos.X)+0.5, float64(n.Pos.Z)+0.5
		dist = math.Hypot(tx-pos.X, tz-pos.Z)
		dy = pos.Y - n.Floor
	}

	// stuck: no closer to the node for two seconds
	if d := dist + math.Abs(dy); d < w.best-0.05 {
		w.best, w.sinceBet = d, 0
	} else if w.sinceBet++; w.sinceBet > 40 {
		w.replans++
		if w.replans > 5 && w.job.follow == nil {
			w.end(s, fmt.Sprintf("failed: stuck before %v", n.Pos))
			return
		}
		w.path = nil
		return
	}

	in := control.Input{Shift: w.Sneak || last && dist < 0.6, Sprint: w.Sprint && !w.Sneak && !last}
	if dist > 0.05 {
		yaw := float32(-math.Atan2(tx-pos.X, tz-pos.Z) * 180 / math.Pi)
		bp.UpdatePosition(func(p basic.Position) basic.Position {
			p.Yaw, p.Pitch = yaw, 0
			return p
		})
		in.Forward = true
	}
	switch n.Kind {
	case Ascend:
		in.Jump = s.OnGround && dist < 1.3 && dy < -maxStep
		if dy < -0.2 && w.climbing(bp) {
			in.Jump = true
		}
	case GapJump:
		in.Sprint = true
		in.Jump = s.OnGround && dist < 1.6
	case Climb:
		in.Jump = dy < 0.1
	case Swim:
		in.Jump = dy < 0.3
	}
	if w.inWater(bp) && dy < 0.3 {
		in.Jump = true // keep the head up, climb out at the edge
	}
	s.Input = in
}

func (w *Walker) climbing(bp *basic.Player) bool {
	p := bp.Position()
	return w.Graph.climbable(world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))})
}

func (w *Walker) inWater(bp *basic.Player) bool {
	p := bp.Position()
	return w.Graph.water(world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))})
}

// plan searches from where the player is.
func (w *Walker) plan(pos basic.Position) error {
	max := w.MaxNodes
	if max == 0 {
		max = 100000 // a way out of a tunnel and round a hill
	}
	start, _ := w.Graph.Start(pos.X, pos.Y, pos.Z)
	path, err := w.Graph.Find(start, w.job.goal, max)
	if err != nil {
		return err
	}
	if len(path) == 0 && w.Precise && w.job.follow == nil {
		path = []Node{start} // there already: to its middle
	}
	w.path, w.i = path, 0
	w.best, w.sinceBet = math.MaxFloat64, 0
	return nil
}

// stillThere checks the next few nodes are still places to be: a block put
// in the way makes the walker plan again.
func (w *Walker) stillThere() bool {
	for k := w.i; k < len(w.path) && k < w.i+4; k++ {
		if _, ok := w.Graph.floor(w.path[k].Pos); !ok {
			return false
		}
	}
	return true
}
