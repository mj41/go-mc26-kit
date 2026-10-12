package control

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Clock is where a controller's ticks and waits come from. The wall's clock
// (the default) ticks twenty times a second in real time; a stepped one
// (NewStepped) ticks only when told to, as fast as the machine goes, so a
// game can be run to the second without waiting for it.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
	NewTicker(d time.Duration) Ticker
}

// Ticker delivers ticks on C. The controller calls Done when it has finished
// the tick it took: a stepped clock waits for it before it goes on.
type Ticker interface {
	C() <-chan time.Time
	Stop()
	Done()
}

// Wall is the real clock.
var Wall Clock = wall{}

type wall struct{}

func (wall) Now() time.Time                         { return time.Now() }
func (wall) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (wall) NewTicker(d time.Duration) Ticker       { return wallTicker{time.NewTicker(d)} }

type wallTicker struct{ t *time.Ticker }

func (w wallTicker) C() <-chan time.Time { return w.t.C }
func (w wallTicker) Stop()               { w.t.Stop() }
func (w wallTicker) Done()               {}

// takeTimeout is how long, in real time, a stepped clock waits for its
// controller to take a tick before it says so: a clock stepped with no
// controller running would wait for ever.
const takeTimeout = 10 * time.Second

// Stepped is a clock that moves only when Step is called. Step delivers each
// tick to its ticker and waits until the controller has finished it, so one
// call is exactly that many ticks of the game.
type Stepped struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*stepTimer
	tickers []*stepTicker
	seq     int

	// Settle, when set, is called after each timer fires and before the next
	// event: the goroutines it woke run to the end of what they do (to be
	// parked again) before anything else happens. Without it two goroutines
	// woken by the clock race, and a game stepped twice differs.
	Settle func()
}

type stepTimer struct {
	at  time.Time
	seq int // the order it was set in: timers due together fire in it
	c   chan time.Time
}

type stepTicker struct {
	s       *Stepped
	period  time.Duration
	next    time.Time
	c       chan time.Time
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

// NewStepped is a stepped clock that reads start.
func NewStepped(start time.Time) *Stepped { return &Stepped{now: start} }

// Now is the time of the clock.
func (s *Stepped) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now
}

// After fires when the clock has been stepped d on.
func (s *Stepped) After(d time.Duration) <-chan time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	t := &stepTimer{at: s.now.Add(d), seq: s.seq, c: make(chan time.Time, 1)}
	s.timers = append(s.timers, t)
	return t.c
}

// NewTicker ticks every d of the clock.
func (s *Stepped) NewTicker(d time.Duration) Ticker {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := &stepTicker{s: s, period: d, next: s.now.Add(d), c: make(chan time.Time), done: make(chan struct{}, 1), stopped: make(chan struct{})}
	s.tickers = append(s.tickers, t)
	return t
}

func (t *stepTicker) C() <-chan time.Time { return t.c }
func (t *stepTicker) Done() {
	select {
	case t.done <- struct{}{}:
	default:
	}
}
func (t *stepTicker) Stop() {
	t.once.Do(func() {
		close(t.stopped)
		t.s.mu.Lock()
		defer t.s.mu.Unlock()
		for i, o := range t.s.tickers {
			if o == t {
				t.s.tickers = append(t.s.tickers[:i], t.s.tickers[i+1:]...)
				break
			}
		}
	})
}

// Step moves the clock d on, firing what falls due on the way in the order of
// the times; a tick is delivered, and finished by the controller, before the
// next. It returns an error when a ticker is not taken (nothing runs it).
func (s *Stepped) Step(d time.Duration) error {
	s.mu.Lock()
	target := s.now.Add(d)
	s.mu.Unlock()
	for {
		s.mu.Lock()
		var tk *stepTicker
		at := target
		for _, t := range s.tickers {
			if !t.next.After(at) && (tk == nil || t.next.Before(tk.next)) {
				tk, at = t, t.next
			}
		}
		// the timers due before that, in order
		sort.Slice(s.timers, func(i, j int) bool {
			a, b := s.timers[i], s.timers[j]
			return a.at.Before(b.at) || a.at.Equal(b.at) && a.seq < b.seq
		})
		fired := false
		for i, t := range s.timers {
			if !t.at.After(at) {
				s.now = t.at
				t.c <- t.at
				s.timers = append(s.timers[:i], s.timers[i+1:]...)
				fired = true
				break
			}
		}
		if fired {
			s.mu.Unlock()
			if s.Settle != nil {
				s.Settle()
			}
			continue
		}
		s.now = at
		if tk == nil {
			s.mu.Unlock()
			return nil
		}
		tk.next = tk.next.Add(tk.period)
		s.mu.Unlock()
		select {
		case tk.c <- at:
		case <-tk.stopped:
			continue
		case <-time.After(takeTimeout):
			return fmt.Errorf("control: the stepped clock's tick was not taken in %s: no controller runs on it", takeTimeout)
		}
		select {
		case <-tk.done:
		case <-tk.stopped:
		case <-time.After(takeTimeout):
			return fmt.Errorf("control: a tick taken was not finished in %s", takeTimeout)
		}
		if s.Settle != nil { // what the tick's end woke runs before the next event
			s.Settle()
		}
	}
}
