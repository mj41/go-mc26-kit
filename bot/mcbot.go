// Package bot is a Minecraft: Java Edition client: it joins a server (login,
// configuration) and hands every play packet to the handlers registered on
// Client.Events. It also pings a server for its list entry (PingAndList).
//
// A bot is built in this order:
//
//	c := bot.NewClient()
//	c.Auth.Name = "Steve"                      // offline mode: a name is all it needs
//	p := basic.NewPlayer(c, basic.DefaultSettings, basic.EventsListener{ /* GameStart, Disconnect, … */ })
//	chat := msg.New(c, p, playerlist.New(c), msg.EventsHandler{ /* PlayerChat, SystemChat */ })
//	err := c.JoinServer("localhost:25565")
//	err = c.HandleGame()                       // blocks until the connection ends
//
// basic.NewPlayer is required: it answers the server's keep-alives and reports
// the game's start, a kick (return an error such as DisconnectErr from its
// Disconnect handler to end HandleGame), health, death and teleports. Without a
// control.Controller, confirm teleports in its Teleported handler
// (Player.AcceptTeleportation). Client.Close ends the connection from another
// goroutine (on Ctrl-C, say).
//
// For an online-mode server, log in with package microsoft and copy the result
// into Auth (Name, UUID, AsTk). To handle any other packet, add a PacketHandler
// with Client.Events.AddListener.
//
// The rest of the client builds on Client and basic.Player: world (the chunks),
// screen (the inventory and open menus), entities, control (the client's 50 ms
// tick: movement, the player-loaded notice, teleport answers), physics, path
// (walking somewhere), act (digging, placing, crafting). Complete programs are in
// the module's examples directory: minimal (the first program: chat and a clean
// exit), daze (a console), robot (all of it).
package bot

import (
	"context"
	"errors"
	"github.com/mj41/go-mc26/protocol/types"
	"net"
	"net/http"
	"strconv"

	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/version"
	mcnet "github.com/mj41/go-mc26/net"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/net/queue"
	"github.com/mj41/go-mc26/protocol/handshake"
	"github.com/mj41/go-mc26/yggdrasil/user"
)

const (
	// ProtocolVersion is the protocol this client speaks: the go-mc26 library's
	// (version.ProtocolVersion), so the Minecraft version of the library this
	// module is built with.
	ProtocolVersion = version.ProtocolVersion
	// DefaultPort is the port an address without one joins (25565).
	DefaultPort = mcnet.DefaultPort
)

// JoinOptions are JoinServerWithOptions' options; the zero value is what
// JoinServer uses.
type JoinOptions struct {
	// MCDialer dials the server; nil is the library's dialer (with SRV lookup).
	MCDialer mcnet.MCDialer
	// Context bounds the dial; it does not end the connection later (Client.Close does).
	Context context.Context

	// NoPublicKey and KeyPair are not used: this client sends no chat-signing
	// key, so its chat is unsigned (a server enforcing secure chat refuses it).
	NoPublicKey bool
	KeyPair     *user.KeyPairResp

	// SessionServer is the session server an online-mode login is announced to (its base URL): empty for
	// Mojang's, https://sessionserver.mojang.com. HTTPClient makes that request (nil: http.DefaultClient). A
	// test gives a fake one; a real client leaves both.
	SessionServer string
	HTTPClient    *http.Client

	// QueueRead and QueueWrite buffer the packets between the connection and
	// HandleGame (and the writers); nil is an unbounded queue.
	QueueRead  queue.Queue[pk.Packet]
	QueueWrite queue.Queue[pk.Packet]
}

// JoinServer connect a Minecraft server for playing the game.
// Using roughly the same way to parse address as minecraft.
func (c *Client) JoinServer(addr string) (err error) {
	return c.JoinServerWithOptions(addr, JoinOptions{})
}

// JoinServerWithDialer is similar to JoinServer but using a net.Dialer.
func (c *Client) JoinServerWithDialer(dialer *net.Dialer, addr string) (err error) {
	return c.JoinServerWithOptions(addr, JoinOptions{
		MCDialer: (*mcnet.Dialer)(dialer),
	})
}

func (c *Client) JoinServerWithOptions(addr string, options JoinOptions) (err error) {
	if options.MCDialer == nil {
		options.MCDialer = &mcnet.DefaultDialer
	}
	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.QueueRead == nil {
		options.QueueRead = queue.NewLinkedQueue[pk.Packet]()
	}
	if options.QueueWrite == nil {
		options.QueueWrite = queue.NewLinkedQueue[pk.Packet]()
	}
	return c.join(addr, options)
}

func (c *Client) join(addr string, options JoinOptions) error {
	// Split Host and Port. The DialMCContext will do this once,
	// but we need the result for sending handshake packet here.
	host, portStr, err := net.SplitHostPort(addr)
	var port uint64
	if err != nil {
		var addrErr *net.AddrError
		const missingPort = "missing port in address"
		if errors.As(err, &addrErr) && addrErr.Err == missingPort {
			host = addr
			port = 25565
		} else {
			return LoginErr{"split address", err}
		}
	} else {
		port, err = strconv.ParseUint(portStr, 0, 16)
		if err != nil {
			return LoginErr{"parse port", err}
		}
	}

	// Dial connection
	conn, err := options.MCDialer.DialMCContext(options.Context, addr)
	if err != nil {
		return LoginErr{"connect server", err}
	}
	joined := false
	defer func() {
		if !joined { // a join that failed (refused, kicked, a broken handshake) leaves no connection open
			_ = conn.Close()
		}
	}()

	// Handshake
	hs := handshake.ClientIntention{
		ProtocolVersion: ProtocolVersion,
		HostName:        pk.String(host),
		Port:            pk.UnsignedShort(port),
		Intention:       types.ClientIntentLogin,
	}
	if err = conn.WritePacket(pk.Marshal(int32(hs.PacketID()), hs)); err != nil {
		return LoginErr{"handshake", err}
	}

	// Login Start
	if err := c.joinLogin(conn, options); err != nil {
		return err
	}

	// Configuration
	if err := c.joinConfiguration(conn); err != nil {
		return err
	}
	c.Conn = warpConn(conn, options.QueueRead, options.QueueWrite, c.Events.read)
	joined = true
	return nil
}

// DisconnectErr is the reason a server gave for ending the connection. Join
// returns it during login or configuration (wrapped in LoginErr or ConfigErr);
// in play, basic.Player's Disconnect handler gets the reason, and returning
// DisconnectErr(reason) from it makes HandleGame return it (wrapped in
// PacketHandlerError). errors.As finds it in all three.
type DisconnectErr chat.Message

func (d DisconnectErr) Error() string {
	return "disconnect because: " + chat.Message(d).String()
}
