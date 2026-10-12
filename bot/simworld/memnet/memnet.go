// Package memnet is a network in memory for a game of simworld: a net.Conn pair with unbounded buffers, a
// listener and a dialer for the kit's bot (bot.JoinOptions.MCDialer). A goroutine reading from it blocks
// on a sync.Cond, which testing/synctest counts as durably blocked — a real loopback socket it does not:
// a game in a bubble (DETERMINISM.md) must talk over this.
package memnet

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	mcnet "github.com/mj41/go-mc26/net"
)

// half is one direction of a pair: what was written, not yet read.
type half struct {
	mu     sync.Mutex
	cond   sync.Cond
	buf    []byte
	closed bool // the writer closed it: a reader drains, then gets EOF
}

func newHalf() *half {
	h := &half{}
	h.cond.L = &h.mu
	return h
}

// Conn is one end of a pair.
type Conn struct {
	in, out       *half
	local, remote addr

	mu            sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
	dead          bool
	timer         *time.Timer // wakes a reader when its deadline falls
}

type addr string

func (a addr) Network() string { return "mem" }
func (a addr) String() string  { return string(a) }

// Pair is two connected ends: what one writes the other reads.
func Pair(clientAddr, serverAddr string) (client, server *Conn) {
	a, b := newHalf(), newHalf()
	client = &Conn{in: a, out: b, local: addr(clientAddr), remote: addr(serverAddr)}
	server = &Conn{in: b, out: a, local: addr(serverAddr), remote: addr(clientAddr)}
	return client, server
}

func (c *Conn) Read(p []byte) (int, error) {
	c.in.mu.Lock()
	defer c.in.mu.Unlock()
	for {
		c.mu.Lock()
		dead, dl := c.dead, c.readDeadline
		c.mu.Unlock()
		switch {
		case dead:
			return 0, net.ErrClosed
		case len(c.in.buf) > 0:
			n := copy(p, c.in.buf)
			c.in.buf = c.in.buf[n:]
			if len(c.in.buf) == 0 {
				c.in.buf = nil
			}
			return n, nil
		case c.in.closed:
			return 0, io.EOF
		case len(p) == 0:
			return 0, nil
		case !dl.IsZero() && !time.Now().Before(dl):
			return 0, timeoutError{}
		}
		c.in.cond.Wait()
	}
}

func (c *Conn) Write(p []byte) (int, error) {
	c.mu.Lock()
	dead := c.dead
	c.mu.Unlock()
	if dead {
		return 0, net.ErrClosed
	}
	c.out.mu.Lock()
	defer c.out.mu.Unlock()
	if c.out.closed {
		return 0, io.ErrClosedPipe
	}
	c.out.buf = append(c.out.buf, p...)
	c.out.cond.Broadcast()
	return len(p), nil
}

// Close closes this end: its reads fail, the other end reads what was written, then EOF.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.dead {
		c.mu.Unlock()
		return nil
	}
	c.dead = true
	if c.timer != nil {
		c.timer.Stop()
	}
	c.mu.Unlock()
	c.out.mu.Lock()
	c.out.closed = true
	c.out.cond.Broadcast()
	c.out.mu.Unlock()
	c.in.mu.Lock()
	c.in.cond.Broadcast()
	c.in.mu.Unlock()
	return nil
}

func (c *Conn) LocalAddr() net.Addr  { return c.local }
func (c *Conn) RemoteAddr() net.Addr { return c.remote }

func (c *Conn) SetDeadline(t time.Time) error {
	_ = c.SetReadDeadline(t)
	return c.SetWriteDeadline(t)
}

// SetReadDeadline: a read blocked past t fails with a timeout, on the clock the caller's goroutine is on
// (the bubble's, in a bubble).
func (c *Conn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if !t.IsZero() {
		c.timer = time.AfterFunc(time.Until(t), func() {
			c.in.mu.Lock()
			c.in.cond.Broadcast()
			c.in.mu.Unlock()
		})
	}
	c.mu.Unlock()
	c.in.mu.Lock()
	c.in.cond.Broadcast() // a reader blocked now looks again at the new deadline
	c.in.mu.Unlock()
	return nil
}

// SetWriteDeadline is kept and not enforced: a write never blocks.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.writeDeadline = t
	c.mu.Unlock()
	return nil
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "memnet: i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// Listener is a listener of this network: Dial hands it a connection, Accept takes it.
type Listener struct {
	name   string
	mu     sync.Mutex
	cond   sync.Cond
	queue  []net.Conn
	closed bool
	n      int
}

// Listen starts a listener of the name (what a client's address is: "mem:0").
func Listen(name string) *Listener {
	l := &Listener{name: name}
	l.cond.L = &l.mu
	return l
}

func (l *Listener) Accept() (net.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for len(l.queue) == 0 && !l.closed {
		l.cond.Wait()
	}
	if len(l.queue) == 0 {
		return nil, net.ErrClosed
	}
	c := l.queue[0]
	l.queue = l.queue[1:]
	return c, nil
}

func (l *Listener) Close() error {
	l.mu.Lock()
	l.closed = true
	l.cond.Broadcast()
	l.mu.Unlock()
	return nil
}

func (l *Listener) Addr() net.Addr { return addr(l.name) }

// MC is the listener as the kit's server takes it.
func (l *Listener) MC() *mcnet.Listener { return &mcnet.Listener{Listener: l} }

// Dialer dials a Listener: it implements mcnet.MCDialer (bot.JoinOptions.MCDialer).
type Dialer struct{ L *Listener }

var errNoListener = errors.New("memnet: the listener is closed")

// DialMCContext connects to the listener (the address is not looked at: there is one).
func (d Dialer) DialMCContext(ctx context.Context, _ string) (*mcnet.Conn, error) {
	d.L.mu.Lock()
	if d.L.closed {
		d.L.mu.Unlock()
		return nil, errNoListener
	}
	d.L.n++
	n := d.L.n
	client, server := Pair("mem-client:"+itoa(n), d.L.name)
	d.L.queue = append(d.L.queue, server)
	d.L.cond.Broadcast()
	d.L.mu.Unlock()
	return mcnet.WrapConn(client), ctx.Err()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for ; n > 0; n /= 10 {
		i--
		b[i] = byte('0' + n%10)
	}
	return string(b[i:])
}
