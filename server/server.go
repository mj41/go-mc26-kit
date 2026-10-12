// Package server is a framework for a Minecraft server: the server is built by
// combining the parts provided here.
//
// # This package is under rapid development, and any API may be subject to break changes
//
// A server is roughly divided into two parts: Gate and GamePlay
//
//	+------------------------------------------------------------------------------+
//	|                             Go-MC Server Framework                           |
//	|--------------------------------------+---------------------------------------|
//	|               Gate                   |                GamePlay               |
//	|--------------------+-----------------+---------------+-----------------------|
//	|    LoginHandler    |         ListPingHandler         |        Others..       |
//	|--------------------|------------+----+---------------|-----------------------+
//	| MojangLoginHandler |  PingInfo  |     PlayerList     |   your own, etc.      |
//	+--------------------+------------+--------------------+-----------------------+
//
// Gate, which is used to respond to the client login request, provide login verification,
// respond to the List Ping Request and providing the online players' information.
//
// Gameplay, which is used to handle all things after a player successfully logs in
// (that is, after the LoginSuccess package is sent),
// and is responsible for functions including player status, chunk management, keep alive, chat, etc.
//
// This module has no Gameplay: a server built on this package provides its own.
// A list-ping handler is a PingInfo and a PlayerList together; a player's chat
// reaches a client of the kit only after the player is in that client's player
// list (a player info update first).
package server

import (
	"encoding/json"
	"errors"
	"github.com/mj41/go-mc26/protocol/types"
	"log"

	"github.com/mj41/go-mc26/data/version"
	"github.com/mj41/go-mc26/net"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/configuration"
	"github.com/mj41/go-mc26/protocol/login"
)

const (
	ProtocolName    = version.Name
	ProtocolVersion = version.ProtocolVersion
)

type Server struct {
	*log.Logger
	ListPingHandler
	LoginHandler
	ConfigHandler
	GamePlay
}

func (s *Server) Listen(addr string) error {
	listener, err := net.ListenMC(addr)
	if err != nil {
		return err
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.AcceptConn(&conn)
	}
}

func (s *Server) AcceptConn(conn *net.Conn) {
	defer conn.Close()
	protocol, intention, err := s.handshake(conn)
	if err != nil {
		return
	}

	switch intention {
	case types.ClientIntentStatus:
		s.acceptListPing(conn, protocol)
	case types.ClientIntentLogin, types.ClientIntentTransfer:
		name, id, profilePubKey, properties, err := s.AcceptLogin(conn, protocol)
		if err != nil {
			var loginErr LoginFailErr
			if errors.As(err, &loginErr) {
				// The login-state disconnect carries the reason as JSON text.
				if reason, jsonErr := json.Marshal(loginErr.reason); jsonErr == nil {
					disconnect := login.LoginDisconnect{Reason: pk.String(reason)}
					_ = conn.WritePacket(pk.Marshal(disconnect.PacketID(), disconnect))
				}
			}
			if s.Logger != nil {
				s.Logger.Printf("client %v login error: %v", conn.Socket.RemoteAddr(), err)
			}
			return
		}
		err = s.AcceptConfig(conn)
		if err != nil {
			var configErr ConfigFailErr
			if errors.As(err, &configErr) {
				disconnect := configuration.Disconnect{Value: configErr.reason}
				_ = conn.WritePacket(pk.Marshal(disconnect.PacketID(), disconnect))
			}
			if s.Logger != nil {
				s.Logger.Printf("client %v config error: %v", conn.Socket.RemoteAddr(), err)
			}
			return
		}
		s.AcceptPlayer(name, id, profilePubKey, properties, protocol, conn)
	}
}
