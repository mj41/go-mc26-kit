package simworld

import "testing"

// The game's keep-alive, counted in its own ticks (never in wall seconds: a stepped game has none to keep, and a
// loaded machine, -race or a slow stage must not change what it does): the server sends one every keepAliveTicks,
// the bot answers it, and a game that runs for several of them goes on. The client of a stepped game holds no wall deadline
// (fakeserver sets Player.KeepAliveTimeout); a client of one's own sets it too (NewServer's doc).
func TestKeepAliveIsAnsweredEveryIntervalOfTicks(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2})
		sent := 0
		g.Debug = nil
		for g.Tick < keepAliveTicks*3+10 {
			g.Step(1)
			if g.Tick%keepAliveTicks == 1 && g.keptAt == 0 && g.Tick > 1 {
				sent++ // (answered by the tick after it was sent: the next interval finds it clear)
			}
		}
		if sent != 3 {
			t.Errorf("%d keep-alives answered in %d ticks, want 3", sent, g.Tick)
		}
		if !g.B.Ctl.State().Loaded {
			t.Errorf("the bot is not in its level at tick %d", g.Tick)
		}
	})
}
