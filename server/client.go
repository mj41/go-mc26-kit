package server

import (
	"strconv"

	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/net/queue"
)

type WritePacketError struct {
	Err error
	ID  int32
}

func (s WritePacketError) Error() string {
	return "server: send packet " + strconv.FormatInt(int64(s.ID), 16) + " error: " + s.Err.Error()
}

func (s WritePacketError) Unwrap() error {
	return s.Err
}

type PacketQueue = queue.Queue[pk.Packet]
