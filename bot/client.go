package bot

import (
	"errors"
	"sync"

	"github.com/google/uuid"

	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/net"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/net/queue"
	"github.com/mj41/go-mc26/registry"
)

// Client is used to access Minecraft server
type Client struct {
	Conn *Conn
	Auth Auth

	// These are filled when login process
	Name       string
	UUID       uuid.UUID
	Registries registry.Registries
	// Tags are the tags the server sent, of every registry, by id.
	Tags    Tags
	Cookies map[string][]byte

	// Ingame packet handlers
	Events Events

	// Login plugins
	LoginPlugin map[string]CustomPayloadHandler

	// Configuration handler
	ConfigHandler

	CustomReportDetails map[string]string

	census census // what the server sent, by packet id (Census)
}

// CustomPayloadHandler is a function handling custom payload
type CustomPayloadHandler func(data []byte) ([]byte, error)

// Close closes the connection to the server; HandleGame then returns. It is
// safe from another goroutine (a signal handler, say).
func (c *Client) Close() error {
	return c.Conn.Close()
}

// NewClient returns a new Client, ready for an offline-mode server under the
// name "Steve" (set Auth.Name to choose another). For an online-mode server,
// log in with package microsoft and fill Auth with its Name, UUID and access
// token.
func NewClient() *Client {
	return &Client{
		Auth:                Auth{Name: "Steve"},
		Registries:          registry.NewNetworkCodec(),
		Events:              Events{handlers: make([][]PacketHandler, packetid.ClientboundPacketIDGuard)},
		LoginPlugin:         make(map[string]CustomPayloadHandler),
		ConfigHandler:       NewDefaultConfigHandler(),
		CustomReportDetails: make(map[string]string),
	}
}

// Conn is a concurrently-safe warpper of net.Conn with packet queue.
// Note that not all methods are concurrently-safe.
type Conn struct {
	*net.Conn
	send, recv queue.Queue[pk.Packet]
	pool       sync.Pool // pool of recv packet data
	rerr       error

	// phase is held while a packet is queued: from the server's StartConfiguration till the configuration
	// finishes (configuring), a play-phase writer — the controller's ticks, a chat — waits, as the game's
	// client sends nothing of play then, and the server reads configuration; only the configuration's own
	// packets go (configWriter).
	phase       sync.Mutex
	phaseChange *sync.Cond
	configuring bool
	closed      bool
}

func warpConn(c *net.Conn, qr, qw queue.Queue[pk.Packet], read func(pk.Packet)) *Conn {
	wc := Conn{
		Conn: c,
		send: qw,
		recv: qr,
		pool: sync.Pool{New: func() any { return []byte{} }},
		rerr: nil,
	}
	wc.phaseChange = sync.NewCond(&wc.phase)
	go func() {
		for {
			// take a buffer from pool, after the packet is handled we put it back
			p := pk.Packet{Data: wc.pool.Get().([]byte)}
			if err := c.ReadPacket(&p); err != nil {
				wc.rerr = err
				break
			}
			if read != nil {
				read(p) // what must not wait for HandleGame (Events.AddReadHook)
			}
			if ok := wc.recv.Push(p); !ok {
				wc.rerr = errors.New("receive queue is full")
				break
			}
		}
		wc.recv.Close()
	}()
	go func() {
		for {
			p, ok := wc.send.Pull()
			if !ok {
				break
			}
			if err := c.WritePacket(p); err != nil {
				break
			}
		}
	}()

	return &wc
}

func (c *Conn) ReadPacket(p *pk.Packet) error {
	packet, ok := c.recv.Pull()
	if !ok {
		return c.rerr
	}
	*p = packet
	return nil
}

// WritePacket queues a packet to the server. While the server has the connection back in its configuration
// phase it waits, and goes when play resumes.
func (c *Conn) WritePacket(p pk.Packet) error {
	c.phase.Lock()
	defer c.phase.Unlock()
	for c.configuring && !c.closed {
		c.phaseChange.Wait()
	}
	return c.push(p)
}

func (c *Conn) push(p pk.Packet) error {
	if ok := c.send.Push(p); !ok {
		return errors.New("queue is full")
	}
	return nil
}

// configure starts (on) or ends the configuration phase the server took the connection back to: the play
// phase's writers wait meanwhile. Started before the acknowledgement, so no play packet follows it.
func (c *Conn) configure(on bool) {
	c.phase.Lock()
	c.configuring = on
	c.phase.Unlock()
	if !on {
		c.phaseChange.Broadcast()
	}
}

// configWriter writes the configuration phase's packets past the wait of WritePacket.
type configWriter struct{ *Conn }

func (c configWriter) WritePacket(p pk.Packet) error { return c.push(p) }

func (c *Conn) Close() error {
	c.phase.Lock()
	c.closed = true
	c.phase.Unlock()
	c.phaseChange.Broadcast() // a writer waiting for play goes, and finds the connection closed
	c.send.Close()
	return c.Conn.Close()
}

// Position is a 3D vector.
type Position struct {
	X, Y, Z int
}
