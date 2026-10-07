package bot

import (
	"bytes"
	"io"
	"slices"
	"sync"

	pk "github.com/mj41/go-mc26/net/packet"
)

// Tags holds every tag the server sent, of every registry, as the numeric ids
// of its entries: block tags (minecraft:climbable, minecraft:mineable/pickaxe),
// item tags, fluid tags and those of the registries the client also keeps
// entries of. The zero value is ready to use.
type Tags struct {
	mu sync.RWMutex
	m  map[string]map[string][]int32 // registry → tag → ids, sorted
}

// Has reports whether the entry id of registry is in tag
// (Has("minecraft:block", "minecraft:climbable", ladderID)).
func (t *Tags) Has(registry, tag string, id int32) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := slices.BinarySearch(t.m[registry][tag], id)
	return ok
}

// IDs returns the ids in a tag of registry.
func (t *Tags) IDs(registry, tag string) []int32 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return slices.Clone(t.m[registry][tag])
}

// readRegistry reads one registry's tags as ClientboundUpdateTags carries
// them (TagNetworkSerialization.NetworkPayload): a count, then each tag's name
// and the ids of its entries. They replace what the registry had.
func (t *Tags) readRegistry(registry string, r io.Reader) error {
	var count pk.VarInt
	if _, err := count.ReadFrom(r); err != nil {
		return err
	}
	tags := make(map[string][]int32, count)
	for range int(count) {
		var name pk.Identifier
		var n pk.VarInt
		if _, err := name.ReadFrom(r); err != nil {
			return err
		}
		if _, err := n.ReadFrom(r); err != nil {
			return err
		}
		ids := make([]int32, n)
		for i := range ids {
			var id pk.VarInt
			if _, err := id.ReadFrom(r); err != nil {
				return err
			}
			ids[i] = int32(id)
		}
		slices.Sort(ids)
		tags[string(name)] = ids
	}
	t.mu.Lock()
	if t.m == nil {
		t.m = make(map[string]map[string][]int32)
	}
	t.m[registry] = tags
	t.mu.Unlock()
	return nil
}

// ReadUpdateTags reads a ClientboundUpdateTags payload (configuration or play)
// into c.Tags, and hands each registry's part to the registry the client keeps
// entries of, when it keeps one.
func (c *Client) ReadUpdateTags(data []byte) error {
	r := bytes.NewReader(data)
	var length pk.VarInt
	if _, err := length.ReadFrom(r); err != nil {
		return err
	}
	for range int(length) {
		var registryID pk.Identifier
		if _, err := registryID.ReadFrom(r); err != nil {
			return err
		}
		var part bytes.Buffer
		if err := c.Tags.readRegistry(string(registryID), io.TeeReader(r, &part)); err != nil {
			return err
		}
		if registry := c.Registries.Registry(string(registryID)); registry != nil {
			if _, err := registry.ReadTagsFrom(&part); err != nil {
				return err
			}
		}
	}
	return nil
}
