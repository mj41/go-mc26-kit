package fakeserver

import (
	"slices"
	"sync"

	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// chunkSender is a player's chunks as the game's PlayerChunkSender keeps them. The rules a client meets:
//   - a chunk the player's view takes in is pending until it is sent, and sent in a batch (ChunkBatchStart,
//     the chunks, ChunkBatchFinished);
//   - a new sender lets one batch go unacknowledged; the next is sent only when the client answered the one
//     before with ChunkBatchReceived (then up to ten may be out);
//   - an entity is told to a player only for a chunk it has been sent (ChunkMap.isChunkTracked: the view
//     contains the chunk and it is not pending).
//
// A client that never answers ChunkBatchFinished gets the chunks of the join and nothing more: a bot that
// has moved on, or a bobber cast to chunks the view took in since, is never told of.
// Simplified from the game: a batch holds every chunk pending (the game sends a few a tick), and the
// chunks a move leaves are not forgotten.
type chunkSender struct {
	mu      sync.Mutex
	centre  [2]int
	sent    map[[2]int]bool
	pending [][2]int
	unacked int // the batches sent and not answered
	allowed int // how many may be unanswered: 1, ten after the first answer
}

func newChunkSender() *chunkSender {
	return &chunkSender{sent: map[[2]int]bool{}, allowed: 1}
}

// tracked is whether the chunk has been sent: an entity in it is told to the player.
func (c *chunkSender) tracked(cx, cz int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sent[[2]int{cx, cz}]
}

// moveView puts the player's view round the chunk cx, cz: the chunks of it not sent yet are pending, and
// what the sender may send is sent.
func (s *Server) moveView(p *Player, cx, cz int) error {
	c := p.chunks
	c.mu.Lock()
	c.centre = [2]int{cx, cz}
	r := s.World.Radius
	for x := cx - r; x <= cx+r; x++ {
		for z := cz - r; z <= cz+r; z++ {
			at := [2]int{x, z}
			if !c.sent[at] && !slices.Contains(c.pending, at) {
				c.pending = append(c.pending, at)
			}
		}
	}
	c.mu.Unlock()
	if err := p.Send(play.SetChunkCacheCenter{X: pk.VarInt(cx), Z: pk.VarInt(cz)}); err != nil {
		return err
	}
	return s.sendBatch(p)
}

// sendBatch sends the pending chunks as a batch when the sender may (one is not answered yet, at first).
func (s *Server) sendBatch(p *Player) error {
	c := p.chunks
	c.mu.Lock()
	if c.unacked >= c.allowed || len(c.pending) == 0 {
		c.mu.Unlock()
		return nil
	}
	batch := c.pending
	c.pending = nil
	c.unacked++
	for _, at := range batch {
		c.sent[at] = true
	}
	c.mu.Unlock()
	if err := p.Send(play.ChunkBatchStart{}); err != nil {
		return err
	}
	for _, at := range batch {
		if err := p.Send(s.World.chunk(at[0], at[1])); err != nil {
			return err
		}
	}
	if err := p.Send(play.ChunkBatchFinished{BatchSize: pk.VarInt(len(batch))}); err != nil {
		return err
	}
	if s.Fishing != nil {
		s.Fishing.chunksSent(p)
	}
	return nil
}

// batchReceived is the client's ChunkBatchReceived: the next batch may go.
func (s *Server) batchReceived(p *Player) {
	c := p.chunks
	c.mu.Lock()
	c.unacked = max(c.unacked-1, 0)
	c.allowed = 10
	c.mu.Unlock()
	_ = s.sendBatch(p)
}

// Teleport moves the player to x, y, z as a /tp does: the position the client confirms, and the chunks round
// the new place, which the player gets as the sender allows (a client that answers its batches gets them at
// once).
func (s *Server) Teleport(p *Player, x, y, z float64) error {
	p.mu.Lock()
	p.pos = [3]float64{x, y, z}
	p.tpID++
	id := p.tpID
	p.mu.Unlock()
	at := play.PlayerPosition{ID: pk.VarInt(id), Change: types.PositionMoveRotation{Position: types.Vec3{X: pk.Double(x), Y: pk.Double(y), Z: pk.Double(z)}}}
	if err := p.Send(at); err != nil {
		return err
	}
	return s.moveView(p, floorDiv16(x), floorDiv16(z))
}

func floorDiv16(v float64) int {
	i := int(v)
	if float64(i) > v {
		i--
	}
	return i >> 4
}

// Pos is where the player is: Spawn, or where Teleport put it.
func (p *Player) Pos() [3]float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pos
}
