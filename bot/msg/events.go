package msg

import (
	"github.com/google/uuid"

	"github.com/mj41/go-mc26/chat"
)

// EventsHandler is a collection of event handlers.
// Fill the fields with your handler functions and pass this struct to [New] to create the msg manager.
// The handler functions will be called when the corresponding event is triggered.
// Leave the fields as nil if you don't want to handle the event.
type EventsHandler struct {
	// SystemChat handles messages sent by gaming system.
	//
	// In vanilla client:
	// If overlay is false, the message will be displayed in the chat box.
	// If overlay is true, the message will be displayed on the top of the hot-bar.
	SystemChat func(msg chat.Message, overlay bool) error

	// PlayerChatMessage handles messages sent by players.
	//
	// Message signing system is added in 1.19. The message and its context could be signed by the player's private key.
	// The manager tries to verify the message signature through the player's public key,
	// and return the result as validated boolean.
	PlayerChatMessage func(msg chat.Message, validated bool) error

	// PlayerChat handles the same messages, and the disguised ones, with what
	// a program answering them needs: who sent it, what they typed, and how
	// (public chat, a whisper — the chat type).
	PlayerChat func(PlayerChat) error

	// DisguisedChat handles DisguisedChat message.
	//
	// DisguisedChat message used to send system chat.
	// Now it is used to send messages from "/say" command from server console.
	DisguisedChat func(msg chat.Message) error
}

// PlayerChat is a message a player sent, as the server relayed it. A
// disguised one — the server sends a message without a signature so, the
// text argument of an unsigned command (/msg, /say) — has no sender id.
type PlayerChat struct {
	Sender    uuid.UUID // uuid.Nil for a disguised message
	Disguised bool
	Name      string // the sender's name: from the player list, or as the chat type names it
	Content   string // what the player typed (the message body)
	// Type is the chat type's name: minecraft:chat for public chat,
	// minecraft:msg_command_incoming for a whisper to this player, … ("" for
	// an inline chat type).
	Type      string
	Message   chat.Message // as the client shows it, decorated by the chat type
	Validated bool         // the signature was verified
}
