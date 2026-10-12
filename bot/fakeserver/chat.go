package fakeserver

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/offline"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Chat tells the bot (the first player) that the player named from said text: Player.Chat.
func (s *Server) Chat(from, text string) error { return s.the().Chat(from, text) }

// Kick disconnects the bot (the first player), with the reason it is told.
func (s *Server) Kick(reason string) error { return s.the().Kick(reason) }

// Said waits for the n-th chat message (from 1) the bot (the first player) sends, and returns its text.
func (s *Server) Said(ctx context.Context, n int) (string, error) { return s.the().Said(ctx, n) }

// Chat tells the player that the player named from said text in public chat, as a server relays it: the
// sender put in the player's list first (once; its uuid the one an offline server gives the name), then
// the message, unsigned, decorated "<from> text". The sender need not be a client of the server.
func (p *Player) Chat(from, text string) error {
	p.mu.Lock()
	id, ok := p.listed[from]
	if !ok {
		id = offline.NameToUUID(from)
		if p.listed == nil {
			p.listed = map[string]uuid.UUID{}
		}
		p.listed[from] = id
	}
	p.mu.Unlock()
	if !ok {
		if err := p.Send(infoAdd(&Player{Name: from, UUID: id})); err != nil {
			return err
		}
	}
	return p.Send(playerChat(from, id, text))
}

// Kick disconnects the player, with the reason it is told.
func (p *Player) Kick(reason string) error {
	return p.Send(play.Disconnect{Value: types.Text(chat.Text(reason))})
}

// Said waits for the n-th chat message (from 1) the player sends, and returns its text.
func (p *Player) Said(ctx context.Context, n int) (string, error) {
	got, err := p.WaitFor(ctx, packetid.ServerboundPlayChat, n)
	if err != nil {
		return "", err
	}
	var c play.Chat
	if err := got.Scan(&c); err != nil {
		return "", fmt.Errorf("fakeserver: the player's chat: %w", err)
	}
	return string(c.Message), nil
}
