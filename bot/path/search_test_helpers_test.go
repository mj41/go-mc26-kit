package path

import "time"

// waitSearch waits for the search running to end: a test drives Think as fast as it can, with no clock between the
// ticks as a game has, so a search that takes some milliseconds would be outrun by the loop (SEARCH.md).
func (w *Walker) waitSearch() {
	for {
		w.mu.Lock()
		running := w.searching
		w.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(time.Millisecond)
	}
}
