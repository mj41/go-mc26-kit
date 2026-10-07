package world

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/mj41/go-mc26/level"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// BlockPos is a block's position in the world.
type BlockPos struct{ X, Y, Z int }

func (p BlockPos) String() string { return fmt.Sprintf("%d %d %d", p.X, p.Y, p.Z) }

// Chunk returns the position of the chunk column the block is in.
func (p BlockPos) Chunk() level.ChunkPos { return level.ChunkPos{int32(p.X >> 4), int32(p.Z >> 4)} }

// BlockAt returns the block state at pos, and false when its chunk is not
// loaded or pos is outside the dimension's height.
func (w *World) BlockAt(pos BlockPos) (block.StateID, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	sec, i, ok := w.section(pos)
	if !ok {
		return 0, false
	}
	return block.StateID(sec.GetBlock(i)), true
}

// SetBlock changes the block state at pos, as a block update from the server
// does; it reports false when the chunk is not loaded or pos is outside the
// dimension's height.
func (w *World) SetBlock(pos BlockPos, state block.StateID) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.setBlock(pos, state)
}

func (w *World) setBlock(pos BlockPos, state block.StateID) bool {
	sec, i, ok := w.section(pos)
	if !ok {
		return false
	}
	sec.SetBlock(i, level.BlocksState(state))
	return true
}

// section finds the section holding pos and the block's index in it; the
// caller holds w.mu.
func (w *World) section(pos BlockPos) (*level.Section, int, bool) {
	c, ok := w.Columns[pos.Chunk()]
	if !ok {
		return nil, 0, false
	}
	y := pos.Y - w.minY
	if y < 0 || y >= len(c.Sections)*16 {
		return nil, 0, false
	}
	return &c.Sections[y>>4], (y&15)<<8 | (pos.Z&15)<<4 | pos.X&15, true
}

// StateString is a block state as the game writes it in a command,
// "minecraft:oak_stairs[facing=east,half=bottom,shape=straight,waterlogged=false]":
// the block's id, then each property in the order the block declares them.
func StateString(id block.StateID) string {
	if int(id) < 0 || int(id) >= len(block.StateList) {
		return fmt.Sprintf("unknown state %d", id)
	}
	b := block.StateList[id]
	v := reflect.ValueOf(b)
	if v.Kind() != reflect.Struct || v.NumField() == 0 {
		return b.ID()
	}
	props := make([]string, 0, v.NumField())
	for i := 0; i < v.NumField(); i++ {
		name, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("nbt"), ",")
		text, err := v.Field(i).Interface().(interface{ MarshalText() ([]byte, error) }).MarshalText()
		if err != nil {
			text = []byte("?")
		}
		props = append(props, name+"="+string(text))
	}
	return b.ID() + "[" + strings.Join(props, ",") + "]"
}

// handleBlockUpdate applies ClientboundBlockUpdate (ClientPacketListener.handleBlockUpdate).
func (w *World) handleBlockUpdate(packet pk.Packet) error {
	var u play.BlockUpdate
	if err := packet.Scan(&u); err != nil {
		return err
	}
	pos, state := BlockPos{u.Pos.X, u.Pos.Y, u.Pos.Z}, block.StateID(u.BlockState)
	if w.SetBlock(pos, state) && w.events.BlockChange != nil {
		w.events.BlockChange(pos, state)
	}
	return nil
}

// handleSectionBlocksUpdate applies ClientboundSectionBlocksUpdate: a section's
// position, then every change as state << 12 | x << 8 | z << 4 | y within it
// (SectionPos, ClientboundSectionBlocksUpdatePacket).
func (w *World) handleSectionBlocksUpdate(packet pk.Packet) error {
	var u play.SectionBlocksUpdate
	if err := packet.Scan(&u); err != nil {
		return err
	}
	sec := int64(u.SectionPos)
	sx, sy, sz := int(sec>>42), int(sec<<44>>44), int(sec<<22>>42)
	type change struct {
		pos   BlockPos
		state block.StateID
	}
	var told []change
	w.mu.Lock()
	for _, ch := range u.States {
		c := int64(ch)
		rel := c & 0xfff
		pos, state := BlockPos{sx<<4 | int(rel>>8&15), sy<<4 | int(rel&15), sz<<4 | int(rel>>4&15)}, block.StateID(c>>12)
		if w.setBlock(pos, state) && w.events.BlockChange != nil {
			told = append(told, change{pos, state})
		}
	}
	w.mu.Unlock()
	for _, c := range told { // with the world free
		w.events.BlockChange(c.pos, c.state)
	}
	return nil
}
