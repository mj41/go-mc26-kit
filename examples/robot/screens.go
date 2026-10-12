package main

import (
	"bytes"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// The screen the robot has open, for a watcher to draw as the game would
// show it to a person playing (EVENTS.md, "screen"): the menu, its slots,
// what the cursor carries, a furnace's flame and arrow, the clicks made.
// Crafting by hand happens in the inventory menu, which a person opens with
// E and the server is never told of: it counts as open from a click in it
// till a second after the last.

// opened is the block whose screen the robot opened last (cmdOpen), and when.
var opened struct {
	sync.Mutex
	pos world.BlockPos
	at  time.Time
}

func noteOpened(pos world.BlockPos) {
	opened.Lock()
	opened.pos, opened.at = pos, time.Now()
	opened.Unlock()
}

// inventoryOpen is how long the inventory screen stays open after a click in it.
const inventoryOpen = time.Second

// watchScreens tells the screen whenever it changed, at most once a tick
// (20 a second): opened, a click, a slot the server set, the cursor; a
// furnace's progress alone (it changes every tick while it burns) at most
// four times a second; closed once.
func (r *robot) watchScreens() {
	var last, lastBut []byte
	var lastAt time.Time
	var invUntil time.Time
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
		clicks := r.screens.Clicks()
		for _, c := range clicks {
			if c.ID == 0 {
				invUntil = time.Now().Add(inventoryOpen)
			}
		}
		e := r.screenEvent(clicks, time.Now().Before(invUntil))
		b, _ := json.Marshal(e)
		if bytes.Equal(b, last) {
			continue
		}
		if last == nil && e["open"] == false {
			last = b // nothing was open: nothing to close
			continue
		}
		data := e["data"]
		delete(e, "data")
		but, _ := json.Marshal(e) // all but the data values
		if data != nil {
			e["data"] = data
		}
		if bytes.Equal(but, lastBut) && time.Since(lastAt) < 250*time.Millisecond {
			continue // only the progress moved, and lately told
		}
		last, lastBut, lastAt = b, but, time.Now()
		events.emit("screen", e)
	}
}

// screenEvent is the open menu (or the inventory, inv: crafting by hand),
// with the clicks made in it since the last
func (r *robot) screenEvent(clicks []screen.Clicked, inv bool) map[string]any {
	m, ok := r.screens.Open()
	id, typ, title := m.ID, m.Type, m.Title.ClearString()
	var slots []screen.Slot
	r.screens.Lock()
	cursor := r.screens.Cursor
	if ok {
		slots = m.Slots
	} else if inv {
		id, typ, title = 0, "inventory", ""
		slots = append([]screen.Slot(nil), r.screens.Inventory.Slots[:]...)
	}
	r.screens.Unlock()
	if !ok && !inv {
		return map[string]any{"open": false}
	}
	e := map[string]any{"open": true, "id": id, "type": typ, "size": len(slots)}
	if title != "" {
		e["title"] = title
	}
	var ss [][]any
	for i, s := range slots {
		if s.Count > 0 {
			ss = append(ss, []any{i, itemName(int32(s.Item)), int(s.Count)})
		}
	}
	e["slots"] = ss
	if cursor.Count > 0 {
		e["cursor"] = []any{itemName(int32(cursor.Item)), int(cursor.Count)}
	}
	if len(m.Data) > 0 {
		n := 0
		for k := range m.Data {
			n = max(n, k+1)
		}
		d := make([]int, n)
		for k, v := range m.Data {
			d[k] = v
		}
		e["data"] = d
	}
	var cs []map[string]any
	for _, c := range clicks {
		if c.ID != id {
			continue
		}
		if c.Recipe != "" {
			cs = append(cs, map[string]any{"recipe": c.Recipe})
			continue
		}
		cs = append(cs, map[string]any{"slot": c.Slot, "button": c.Button, "mode": clickMode(c.Mode)})
	}
	if len(cs) > 0 {
		e["clicks"] = cs
	}
	opened.Lock()
	if ok && time.Since(opened.at) < 5*time.Second {
		e["block"] = map[string]int{"x": opened.pos.X, "y": opened.pos.Y, "z": opened.pos.Z}
	}
	opened.Unlock()
	return e
}

// clickMode is a click's kind as the game names it (ContainerInput)
func clickMode(m types.ContainerInput) string {
	names := []string{"pickup", "quick_move", "swap", "clone", "throw", "quick_craft", "pickup_all"}
	if int(m) >= 0 && int(m) < len(names) {
		return names[m]
	}
	return strconv.Itoa(int(m))
}

// blockEventListener tells the block events the server sends (EVENTS.md
// "block_event"): what everyone near sees a block do — a chest's lid opening
// (type 1, data: how many have it open) and closing, a bell rung, a piston
// pushing, a note block played.
func (r *robot) blockEventListener() bot.PacketHandler {
	return bot.PacketHandler{ID: packetid.ClientboundPlayBlockEvent, F: func(p pk.Packet) error {
		var b play.BlockEvent
		if err := p.Scan(&b); err != nil {
			return nil // one read wrong is no reason to drop the connection
		}
		name := ""
		if int(b.Block) >= 0 && int(b.Block) < len(registryid.Block) {
			name = registryid.Block[b.Block]
		}
		events.emit("block_event", map[string]any{"x": b.Pos.X, "y": b.Pos.Y, "z": b.Pos.Z, "block": name, "type": int(b.B0), "data": int(b.B1)})
		return nil
	}}
}
