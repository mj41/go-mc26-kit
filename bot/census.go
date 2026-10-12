package bot

import (
	"sort"
	"sync"

	"github.com/mj41/go-mc26/data/packetid"
)

// The packet census: what the server sent over a game, by packet id — how many, how many bytes, whether
// any handler is registered for the id, and the handlers' errors (a packet a handler could not read ends
// HandleGame with a PacketHandlerError, and the handlers after it never see the packet; a bot that goes on
// reading hides that). A bot asks for it now and then (Client.Census) and tells what it finds.

// CensusEntry is one packet id's count.
type CensusEntry struct {
	ID         packetid.ClientboundPacketID
	Name       string
	Read       uint64
	Bytes      uint64
	NoHandler  bool   // no handler is registered for this id (the generic ones aside)
	Errors     uint64 // times a handler returned an error for it
	FirstError string // the first of them
}

type census struct {
	mu sync.Mutex
	m  map[packetid.ClientboundPacketID]*CensusEntry
}

// note counts one packet read by handlePacket: its handlers' error (nil: none), and whether any is registered.
func (c *census) note(id packetid.ClientboundPacketID, size int, handlers int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[packetid.ClientboundPacketID]*CensusEntry{}
	}
	e := c.m[id]
	if e == nil {
		e = &CensusEntry{ID: id, Name: id.String(), NoHandler: handlers == 0}
		c.m[id] = e
	}
	e.Read++
	e.Bytes += uint64(size)
	if err != nil {
		if e.Errors == 0 {
			e.FirstError = err.Error()
		}
		e.Errors++
	}
}

// Census is what the client has read so far, by packet id.
func (c *Client) Census() []CensusEntry {
	c.census.mu.Lock()
	defer c.census.mu.Unlock()
	out := make([]CensusEntry, 0, len(c.census.m))
	for _, e := range c.census.m {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// CensusEvent is what a bot tells of the census at each status line: the ids read since the last time (prev, the
// census then, by id; nil the first time) with their packets and bytes, those no handler is registered for flagged,
// and the handlers' errors with the first text. It returns nil data when nothing was read, and the census to pass
// next time.
func CensusEvent(prev map[packetid.ClientboundPacketID]CensusEntry, cur []CensusEntry) (data map[string]any, next map[packetid.ClientboundPacketID]CensusEntry) {
	next = make(map[packetid.ClientboundPacketID]CensusEntry, len(cur))
	var rows []map[string]any
	var total uint64
	for _, e := range cur {
		next[e.ID] = e
		p := prev[e.ID]
		if e.Read == p.Read {
			continue
		}
		row := map[string]any{"id": int32(e.ID), "name": e.Name, "read": e.Read - p.Read, "bytes": e.Bytes - p.Bytes}
		total += e.Read - p.Read
		if e.NoHandler {
			row["nohandler"] = true
		}
		if e.Errors > p.Errors {
			row["errors"] = e.Errors - p.Errors
			row["firstError"] = e.FirstError
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, next
	}
	return map[string]any{"read": total, "ids": rows}, next
}
