package basic

import (
	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// DefaultChunksPerTick is the chunk rate the client asks for after every chunk
// batch. Vanilla servers throttle chunk sending until the client acknowledges a
// batch, and each acknowledgement carries the rate the client is willing to
// receive; the vanilla client derives it from its own timing, a bot has no such
// limit.
const DefaultChunksPerTick = 64

// attachChunkBatch answers every ClientboundChunkBatchFinished with
// ServerboundChunkBatchReceived. That is a duty of every client in play, not
// of one that keeps a world: the server's chunk sender (PlayerChunkSender)
// lets one batch go unanswered, and sends the next — the chunks of a place the
// player was moved to — only after the answer. Until then those chunks are
// pending, and the server tells no entity in them to the player
// (ChunkMap.isChunkTracked is false for a pending chunk): a bot that never
// answered would be sent the chunks of the join and nothing after, and never
// see a bobber, an item or a mob that is in a chunk past them. The answer is
// made here once, so a bot with a world and one without behave alike, and
// bot/world does not send a second.
func (p *Player) attachChunkBatch() {
	p.c.Events.AddListener(bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayChunkBatchFinished, F: p.handleChunkBatchFinished})
}

func (p *Player) handleChunkBatchFinished(packet pk.Packet) error {
	var finished play.ChunkBatchFinished
	if err := packet.Scan(&finished); err != nil {
		return err
	}
	ack := play.ChunkBatchReceived{DesiredChunksPerTick: DefaultChunksPerTick}
	if err := p.c.Conn.WritePacket(pk.Marshal(ack.PacketID(), ack)); err != nil {
		return Error{err}
	}
	return nil
}
