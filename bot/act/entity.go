package act

import (
	"errors"
	"math"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26/data/version"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// InteractEntity right-clicks an entity with the held item — shears on a
// sheep, wheat on a cow, an empty hand on a villager — as a person does
// (MultiPlayerGameMode.interact): it looks at the entity and sends the
// interaction with where on it the cursor is; before 26.3 the arm swings.
func (h *Hands) InteractEntity(where func() (entities.Entity, bool), done func(error)) {
	if done == nil {
		done = func(error) {}
	}
	h.start(&interact{where: where, done: done})
}

type interact struct {
	where func() (entities.Entity, bool)
	done  func(error)
	aimed bool
}

func (i *interact) tick(h *Hands, s *control.State, bp *basic.Player) bool {
	e, ok := i.where()
	if !ok {
		i.done(errors.New("act: the entity is gone"))
		return true
	}
	// aim half a block up the entity
	LookAt(bp, s.Input.Shift, e.X, e.Y+0.5, e.Z)
	if !i.aimed {
		i.aimed = true
		return false
	}
	ex, ey, ez := Eye(bp.Position(), s.Input.Shift)
	if math.Sqrt((e.X-ex)*(e.X-ex)+(e.Y+0.5-ey)*(e.Y+0.5-ey)+(e.Z-ez)*(e.Z-ez)) > entityReach(bp)+0.5 {
		i.done(errors.New("act: out of reach"))
		return true
	}
	err := h.send(play.Interact{EntityID: pk.VarInt(e.ID), Hand: types.InteractionHand(0),
		Location: types.LpVec3{X: 0, Y: 0.5, Z: 0}, UsingSecondaryAction: pk.Boolean(s.Input.Shift)})
	if err == nil && version.ProtocolVersion < 777 {
		err = h.swing()
	}
	i.done(err)
	return true
}
