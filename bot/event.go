package bot

import (
	"sort"
	"strconv"

	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
)

// Events holds the handlers of the play packets: HandleGame calls the ones
// registered for a packet's id, after the generic ones.
type Events struct {
	generic  []PacketHandler   // for every packet
	handlers [][]PacketHandler // for specific packet id only
	onRead   [][]PacketHandler // run by the connection's reader as a packet is read (AddReadHook)
}

// AddReadHook adds listeners that run on the connection's own reader goroutine, as soon as a packet of their id is
// read — before it waits in the queue for HandleGame, which runs the ordinary listeners one packet after the other
// on the goroutine that calls it. A hook must never wait: not for a lock, a channel or a slow consumer. It is for
// what must not be late whatever user code does: the keep-alive's answer (a server that hears none in 30 s ends
// the connection, and a client whose read deadline is only moved by a handled keep-alive gives up while the
// handler of another packet is still running). Add hooks before joining: the reader reads them without a lock.
func (e *Events) AddReadHook(listeners ...PacketHandler) {
	for _, l := range listeners {
		if l.ID < 0 || int(l.ID) >= len(e.handlers) {
			panic("Invalid packet ID (" + strconv.Itoa(int(l.ID)) + ")")
		}
		if e.onRead == nil {
			e.onRead = make([][]PacketHandler, len(e.handlers))
		}
		e.onRead[l.ID] = append(e.onRead[l.ID], l)
		sortPacketHandlers(e.onRead[l.ID])
	}
}

// read runs the hooks of a packet just read (the reader's goroutine).
func (e *Events) read(p pk.Packet) {
	if e.onRead == nil || p.ID < 0 || int(p.ID) >= len(e.onRead) {
		return
	}
	for _, h := range e.onRead[p.ID] {
		_ = h.F(p) // (a hook's error has nowhere to go: it must not matter)
	}
}

// AddListener registers handlers, each for the clientbound play packet its ID
// names; of the handlers of one id, a higher Priority runs first. An ID out of
// range panics.
func (e *Events) AddListener(listeners ...PacketHandler) {
	for _, l := range listeners {
		// panic if l.ID is invalid
		if l.ID < 0 || int(l.ID) >= len(e.handlers) {
			panic("Invalid packet ID (" + strconv.Itoa(int(l.ID)) + ")")
		}
		if s := e.handlers[l.ID]; s == nil {
			e.handlers[l.ID] = []PacketHandler{l}
		} else {
			e.handlers[l.ID] = append(s, l)
			sortPacketHandlers(e.handlers[l.ID])
		}
	}
}

// AddGeneric adds listeners like AddListener, but the packet ID is ignored.
// Generic listener is always called before specific packet listener.
func (e *Events) AddGeneric(listeners ...PacketHandler) {
	e.generic = append(e.generic, listeners...)
	sortPacketHandlers(e.generic)
}

type (
	// PacketHandlerFunc is the function of a PacketHandler.
	PacketHandlerFunc func(p pk.Packet) error
	// PacketHandler is a handler of one clientbound play packet: F is called
	// with every packet of that ID (decode it with p.Scan into the packet's
	// struct from go-mc26's protocol/play). An error F returns comes back from
	// HandleGame as a PacketHandlerError.
	PacketHandler struct {
		ID       packetid.ClientboundPacketID
		Priority int
		F        func(p pk.Packet) error
	}
)

func sortPacketHandlers(slice []PacketHandler) {
	sort.SliceStable(slice, func(i, j int) bool {
		return slice[i].Priority > slice[j].Priority
	})
}
