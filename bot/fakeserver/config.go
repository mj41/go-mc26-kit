package fakeserver

import (
	"bytes"
	"sort"

	"github.com/mj41/go-mc26-kit/server"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/data/tags"
	mcnet "github.com/mj41/go-mc26/net"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/configuration"
	"github.com/mj41/go-mc26/registry"
)

// config is the server's configuration: the version's registries (as
// server.Configurations sends them), then the tags of the data pack — what a
// tool mines, what a recipe takes — as UpdateTags, built from data/tags, then
// the end of the configuration. The client reads them as it reads a real
// server's.
type config struct{ registries registry.Registries }

func (c *config) AcceptConfig(conn *mcnet.Conn) error {
	err := c.registries.EachRegistry(func(id string, reg pk.FieldEncoder) error {
		return conn.WritePacket(pk.Marshal(configuration.RegistryData{}.PacketID(), pk.Identifier(id), reg))
	})
	if err != nil {
		return err
	}
	if err := conn.WritePacket(updateTags()); err != nil {
		return err
	}
	finish := configuration.ClientboundFinishConfiguration{}
	return conn.WritePacket(pk.Marshal(finish.PacketID(), finish))
}

var _ server.ConfigHandler = (*config)(nil)

// updateTags is ClientboundUpdateTags: per registry (block, item) a count, then
// each tag's name and the registry ids of its entries.
func updateTags() pk.Packet {
	var b bytes.Buffer
	w := func(f pk.FieldEncoder) { _, _ = f.WriteTo(&b) }
	w(pk.VarInt(2))
	for _, r := range []struct {
		registry string
		ids      []string
		names    []string
		members  func(tag string) []string
	}{
		{"minecraft:block", registryid.Block, tags.BlockTags(), tags.Block},
		{"minecraft:item", registryid.Item, tags.ItemTags(), tags.Item},
	} {
		index := make(map[string]int, len(r.ids))
		for i, n := range r.ids {
			index[n] = i
		}
		w(pk.Identifier(r.registry))
		w(pk.VarInt(len(r.names)))
		for _, name := range r.names {
			w(pk.Identifier(name))
			members := r.members(name)
			ids := make([]int, 0, len(members))
			for _, m := range members {
				if i, ok := index[m]; ok {
					ids = append(ids, i)
				}
			}
			sort.Ints(ids)
			w(pk.VarInt(len(ids)))
			for _, i := range ids {
				w(pk.VarInt(i))
			}
		}
	}
	return pk.Packet{ID: int32(configuration.UpdateTags{}.PacketID()), Data: b.Bytes()}
}
