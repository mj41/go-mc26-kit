// Package simworld is a small Minecraft for tests: the server's part of a game
// played in the test's own process, a flat world the test sets up, a bot of
// the kit on it, and both stepped from one loop on a virtual clock — a step is
// exactly a tick of the client and of the server, in that order, whatever the
// machine's speed, so a run is the same every time.
//
// It models what the bot meets, a rule at a time: the chunks round the
// player at the view distance, a move accepted or turned back by the blocks'
// collision, the damage of a fall. A packet it does not model fails the game
// loudly ("not modelled: …"), never ignored. It is built on bot/fakeserver,
// which holds the handshake and the world's blocks.
package simworld

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Options set up a game.
type Options struct {
	// View is the distance, in chunks, the server sends chunks round the player (default 6).
	View int
	// Seed is the game's chance (loot): the same seed, the same run (default 1).
	Seed int64
	// Day is the game's day: nil, none is told (the bot reads no clock, as a stage that holds it);
	// else the clock the bot reads. "time set" in Setup sets it held (the harness's flat stages hold
	// theirs at a time); Run makes it run, a day 24000 ticks, from the time set.
	Day *Day
	// Setup are the world's lines after the flat ground, in the commands a
	// stage's harness gives the real server: "fill x1 y1 z1 x2 y2 z2 block",
	// "setblock x y z block".
	Setup []string
}

// Day is the clock of the game's day (the overworld's world clock).
type Day struct {
	Time int64 // the time of day at the start, 0 to 23999 (0 the sun up past dawn, 6000 noon, 13000 the night's start)
	Run  bool  // the clock runs, a tick a tick; else it is held at Time
}

// takeTimeout is how long, in real time, the loop waits for the bot to answer
// before it says the game is stuck.
const takeTimeout = 10 * time.Second

// Game is a small Minecraft with one bot in it.
type Game struct {
	T testing.TB
	S *fakeserver.Server
	B *fakeserver.Bot // the kit's bot, on a stepped clock (Start; nil with NewServer)
	// Debug, when set, is told each packet the server plays, with its tick (a debugging aid).
	Debug func(tick int, p fakeserver.Packet)
	// Steps is the clock the client's controller ticks on: stepped by Step
	Steps *control.Stepped
	opt   Options

	mu       sync.Mutex
	cond     *sync.Cond
	queue    []fakeserver.Packet // read, not yet played by the server
	tickEnds int                 // ClientTickEnd packets read
	pongs    int                 // Pong packets read
	pings    int32               // Ping packets sent
	keptAt   int64               // the keep-alive sent and not yet answered (its id: the tick it was sent), 0 none

	// the server's tick count and the player as the server knows it
	Tick     int
	pos      [3]float64
	yaw      float64 // the way it faces, degrees
	onGround bool
	fall     float64 // fallen so far without landing
	Health   float32
	Food     int
	tp       int32 // teleport id last sent
	tpWait   bool  // a teleport sent, not yet confirmed: moves are ignored
	centre   [2]int
	sent     map[[2]int]bool
	failed   error

	exhaustion  float32 // the food's wear (food.go)
	sat         float32 // the saturation
	healTimer   int
	toldHealth  [3]float32 // health, food, saturation as the bot was last told
	sprinting   bool
	eating      *eating
	slots       [46]slotItem // the player's inventory (inventory.go)
	menu0       *menu        // the player's own menu
	open        *menu        // the crafting table's menu, while the player has it open
	containerID int          // the last container id given
	cursor      slotItem     // what the player carries on the cursor
	snapCursor  slotItem
	book        *recipeBook // the crafting recipes, and which the player has
	held        int         // the hotbar slot chosen
	stateID     int32       // the container state id last sent
	dig         *digging
	delayed     *digging // a dig stopped early that goes on (dig.go delayedTick)
	items       []*item
	nextID      int32
	rng         *rand.Rand          // the game's chance: loot, from Options.Seed
	motion      *rand.Rand          // the chance of an item's place and throw when a block breaks (apart, so the loot is the same)
	ai          *rand.Rand          // the mobs' chance: a skeleton's strafe and aim, a blast's rays (apart again)
	taken       []int32             // item entities picked up this tick, not yet told gone
	furnaces    map[[3]int]*furnace // the furnaces placed (furnace.go)
	crops       map[[3]int]bool     // the wheat growing (crops.go)
	cropRand    *rand.Rand          // the random ticks' chance (apart: the loot stays the same)
	day         *Day                // the clock the bot reads (nil: none told)
	gives       []slotItem          // what the player is given when it comes in (the stage's "give" lines)
	mobs        []*mob              // the mobs alive (mob.go)
	arrows      []*arrow            // the arrows shot (skeleton.go)
	blasts      int                 // the explosions so far (creeper.go)
	babies      int                 // a summon's babies: 0 by the game's chance, 1 a baby, 2 an adult (a test's)
	toldAttr    *string             // the held item the player's attributes were last told for ("": a hand)
	dead        bool                // the player died and has not asked to respawn (death.go)
	deaths      int
	invuln      int     // ticks the player takes no blow of the size of the last (20 after a hit)
	lastDamage  float32 // the last blow
	lastSwing   int     // the tick of the last swing (the attack's charge)
}

// Start serves a game and joins a bot of the kit to it, on a stepped clock; the
// bot is in its level when it returns.
func Start(t testing.TB, o Options) *Game {
	t.Helper()
	g := NewServer(t, o)
	g.B = fakeserver.JoinSteppedOn(t, g.S, "Tester", g.Steps)
	g.Attach(func() bool { return g.B.Ctl.State().Loaded })
	return g
}

// NewServer serves a game with no one in it. A client — the kit's bot, or any
// bot's own parts — joins at Addr with its controller on Steps (Controller.Clock),
// and Attach waits for it to be in its level. A client of its own must hold no wall-clock deadline (a stepped game
// has none to keep): set its basic.Player.KeepAliveTimeout negative.
func NewServer(t testing.TB, o Options) *Game {
	t.Helper()
	if o.View == 0 {
		o.View = 6
	}
	var s *fakeserver.Server
	if bubbles.Load() > 0 {
		s = fakeserver.StartMem(t) // in a bubble: a network in memory (DETERMINISM.md)
	} else {
		s = fakeserver.Start(t)
	}
	s.World.Radius = o.View
	g := &Game{T: t, nextID: 100, book: newBook(), menu0: inventoryMenu(), S: s, opt: o, sent: map[[2]int]bool{}, Health: 20, Food: 20, onGround: true, sat: 5, toldHealth: [3]float32{20, 20, 5},
		Steps: control.NewStepped(time.Unix(0, 0)), rng: rand.New(rand.NewSource(max(o.Seed, 1))), motion: rand.New(rand.NewSource(max(o.Seed, 1) + 7919)),
		ai: rand.New(rand.NewSource(max(o.Seed, 1) + 104729))}
	g.Steps.Settle = settle
	g.cond = sync.NewCond(&g.mu)
	if o.Day != nil {
		d := *o.Day
		g.day = &d
	}
	for _, line := range o.Setup {
		if err := g.setupLine(line); err != nil {
			t.Fatal(err)
		}
	}
	g.cropRand, g.crops = rand.New(rand.NewSource(max(o.Seed, 1)+15485863)), map[[3]int]bool{}
	g.S.World.Each(func(x, y, z int, s block.StateID) {
		if block.StateList[s].ID() == "minecraft:wheat" {
			g.crops[[3]int{x, y, z}] = true
		}
	})
	g.pos = [3]float64{fakeserver.Spawn[0], fakeserver.Spawn[1], fakeserver.Spawn[2]}
	// (the fake server's join sends the chunks round the spawn: they are the first sent)
	for cx := -o.View; cx <= o.View; cx++ {
		for cz := -o.View; cz <= o.View; cz++ {
			g.sent[[2]int{cx, cz}] = true
		}
	}
	s.Hook = g.hook
	return g
}

// Addr is where a client joins.
func (g *Game) Addr() string { return g.S.Addr }

// Join joins a client of the kit to the game, over its network: a loopback port, or memory in a bubble. A client
// of a stepped game also holds no wall-clock keep-alive deadline (basic.Player.KeepAliveTimeout negative).
func (g *Game) Join(c *bot.Client) error { return g.S.Join(c) }

// Attach waits for the client that joined to be in the game, steps it till
// loaded holds (its level is in), and tells it the recipes it has at once.
func (g *Game) Attach(loaded func() bool) {
	g.T.Helper()
	if err := g.S.Joined(5 * time.Second); err != nil {
		g.T.Fatal(err)
	}
	g.tp = 1                                  // (the join's teleport)
	if err := g.unlockRecipes(); err != nil { // the recipes unlocked at once (the crafting table)
		g.T.Fatal(err)
	}
	for _, it := range g.gives {
		g.Give(it.item, it.n)
	}
	g.gives = nil
	if err := g.tellTime(); err != nil {
		g.T.Fatal(err)
	}
	// the player's health and food, as the game tells them when a player comes in
	if err := g.S.Send(play.SetHealth{Health: pk.Float(g.Health), Food: pk.VarInt(g.Food), Saturation: 5}); err != nil {
		g.T.Fatal(err)
	}
	g.sync() // the client has read everything sent so far
	if !g.Until(int(takeTimeout/control.TickInterval), loaded) {
		g.T.Fatal("simworld: the client's level never loaded")
	}
}

// hook is called for every packet the bot sends (the fake server's reader).
func (g *Game) hook(p fakeserver.Packet) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch p.ID {
	case packetid.ServerboundPlayClientTickEnd:
		g.tickEnds++
	case packetid.ServerboundPlayPong:
		g.pongs++
	}
	g.queue = append(g.queue, p)
	g.cond.Broadcast()
}

// waitFor waits, in real time, for cond to hold.
func (g *Game) waitFor(what string, cond func() bool) {
	deadline := time.AfterFunc(takeTimeout, func() {
		g.mu.Lock()
		if g.failed == nil {
			g.failed = fmt.Errorf("simworld: stuck waiting for %s", what)
			buf := make([]byte, 1<<22)
			os.Stderr.Write(append([]byte("simworld: the goroutines when it stuck:\n"), buf[:runtime.Stack(buf, true)]...))
		}
		g.mu.Unlock()
		g.cond.Broadcast()
	})
	defer deadline.Stop()
	g.mu.Lock()
	defer g.mu.Unlock()
	for !cond() && g.failed == nil {
		g.cond.Wait()
	}
	if g.failed != nil {
		g.T.Fatal(g.failed)
	}
}

// sync waits till the bot has read and handled everything the server sent: a
// ping, answered when the packets before it have been.
func (g *Game) sync() {
	g.T.Helper()
	g.mu.Lock()
	g.pings++
	want, id := g.pongs+1, g.pings
	g.mu.Unlock()
	if err := g.S.Send(play.Ping{ID: pk.Int(id)}); err != nil {
		g.T.Fatal(err)
	}
	g.waitFor("the bot's pong", func() bool { return g.pongs >= want })
}

// Step plays n ticks: in each, the client's tick, then the server's.
func (g *Game) Step(n int) {
	g.T.Helper()
	for i := 0; i < n; i++ {
		g.mu.Lock()
		want := g.tickEnds + 1
		g.mu.Unlock()
		if err := g.Steps.Step(control.TickInterval); err != nil {
			g.T.Fatal(err)
		}
		g.waitFor("the bot's tick end", func() bool { return g.tickEnds >= want })
		g.serverTick()
		g.sync()
		settle()
	}
}

// settle waits, in real time, till the bot's own goroutines have nothing to
// run — each parked on the clock, a channel, a socket. What the bot computes
// between two ticks takes no game time, as it would on a machine fast enough:
// without this, the game runs fifty times faster than its code (a tick is a
// millisecond here, fifty on a server) and a computation of a fifth of a
// second is four seconds of game.
func settle() {
	if bubbles.Load() > 0 {
		synctest.Wait() // in a bubble the runtime says it: every goroutine durably blocked (DETERMINISM.md)
		return
	}
	settleByStacks()
}

// Settle waits till the bot's own goroutines have nothing to run (what Step does after each tick): a test that
// starts something between two Steps — the brain's round starts a job on a goroutine of its own — calls it before the
// next Step, or the job's first moment is before the tick or after it by the scheduler's choice.
func (g *Game) Settle() { settle() }

// settleByStacks is settle outside a bubble: the goroutines' states, read from runtime.Stack.
func settleByStacks() {
	buf := make([]byte, 1<<18)
	quiet := 0
	for spins := 0; quiet < 2 && spins < 200000; spins++ {
		n := runtime.Stack(buf, true)
		for n == len(buf) { // (all of it did not fit)
			buf = make([]byte, 2*len(buf))
			n = runtime.Stack(buf, true)
		}
		dump := buf[:n]
		// the caller is running; anything else running or runnable is the bot's, with something to do
		if bytes.Count(dump, []byte("[running")) > 1 || bytes.Contains(dump, []byte("[runnable")) || bytes.Contains(dump, []byte("[syscall")) {
			quiet = 0
			runtime.Gosched()
			time.Sleep(20 * time.Microsecond)
			continue
		}
		quiet++
	}
}

// Until steps one tick at a time, at most max, till done holds: it says whether it did.
func (g *Game) Until(max int, done func() bool) bool {
	g.T.Helper()
	for i := 0; i < max; i++ {
		if done() {
			return true
		}
		g.Step(1)
	}
	return done()
}

// serverTick plays the packets the bot sent in its tick, then the server's own work.
func (g *Game) serverTick() {
	g.T.Helper()
	g.mu.Lock()
	in := g.queue
	g.queue = nil
	g.mu.Unlock()
	g.Tick++
	for _, p := range in {
		if g.Debug != nil {
			g.Debug(g.Tick, p)
		}
		if err := g.play(p); err != nil {
			g.T.Fatal(err)
		}
	}
	if err := g.foodTick(); err != nil {
		g.T.Fatal(err)
	}
	if err := g.furnacesTick(); err != nil {
		g.T.Fatal(err)
	}
	if err := g.cropsTick(); err != nil {
		g.T.Fatal(err)
	}
	if err := g.delayedTick(); err != nil {
		g.T.Fatal(err)
	}
	if g.invuln > 0 {
		g.invuln--
	}
	if err := g.mobsTick(); err != nil {
		g.T.Fatal(err)
	}
	if err := g.arrowsTick(); err != nil {
		g.T.Fatal(err)
	}
	if err := g.itemsTick(); err != nil {
		g.T.Fatal(err)
	}
	if err := g.pickups(); err != nil {
		g.T.Fatal(err)
	}
	g.broadcast() // (the slots a pick-up changed: told at the tick's end, after the pick-up itself)
	if err := g.removeTaken(); err != nil {
		g.T.Fatal(err)
	}
	if err := g.keepAlive(); err != nil {
		g.T.Fatal(err)
	}
	if g.Tick%20 == 0 { // the world's age, and the day's clock, every second (ClientboundSetTime)
		if err := g.tellTime(); err != nil {
			g.T.Fatal(err)
		}
	}
	if err := g.sendChunks(); err != nil {
		g.T.Fatal(err)
	}
}

// keepAliveTicks is how often the server sends a keep-alive: every 15 s (ServerCommonPacketListenerImpl), 300
// ticks of the game here — its clock is the stepped one.
const keepAliveTicks = 300

// keepAlive is the server's keep-alive: one every keepAliveTicks, its id the tick; one not answered by the next
// is the game's "timed out". The client holds its connection open only while they come (its deadline is on
// the wall clock, basic.KeepAliveTimeout): without them a game that runs longer than that on the wall
// clock — a long stage, a busy machine — loses its bot at whatever tick it has reached.
func (g *Game) keepAlive() error {
	if g.Tick%keepAliveTicks != 0 {
		return nil
	}
	if g.keptAt != 0 {
		return fmt.Errorf("simworld: the bot did not answer the keep-alive of tick %d (the game's \"timed out\")", g.keptAt)
	}
	g.keptAt = int64(g.Tick)
	return g.S.Send(play.ClientboundKeepAlive{ID: pk.Long(g.keptAt)})
}

// Pos is where the server has the player.
func (g *Game) Pos() [3]float64 { return g.pos }

// Teleport puts the player there, as the game's teleport does: the position
// sent, the fall forgotten.
func (g *Game) Teleport(x, y, z float64) {
	g.T.Helper()
	g.tp++
	g.pos, g.fall, g.tpWait = [3]float64{x, y, z}, 0, true
	at := play.PlayerPosition{ID: pk.VarInt(g.tp), Change: types.PositionMoveRotation{Position: types.Vec3{X: pk.Double(x), Y: pk.Double(y), Z: pk.Double(z)}}}
	if err := g.S.Send(at); err != nil {
		g.T.Fatal(err)
	}
	g.sync() // the bot has taken it (and confirmed it) before its next tick
}

// SetBlock changes a block while the game runs and tells the player (a block update), as a cell turning to
// water under the flow does after the player dug it. state is the block's name (its plain state) or, with
// properties, the text the game writes ("minecraft:water[level=0]").
func (g *Game) SetBlock(x, y, z int, state string) {
	g.T.Helper()
	var st block.StateID
	if strings.Contains(state, "[") {
		st = stateByText(state)
	} else {
		st = fakeserver.State(state)
	}
	g.S.World.SetState(x, y, z, st)
	if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x, Y: y, Z: z}, BlockState: pk.VarInt(st)}); err != nil {
		g.T.Fatal(err)
	}
}

// play is the server's answer to one packet.
func (g *Game) play(p fakeserver.Packet) error {
	// the arm's swing is a packet of another name in each version (ServerboundSwing,
	// from 26.3 ServerboundPunch): found by its name; nothing hears it
	if n := p.ID.String(); n == "ServerboundSwing" || n == "ServerboundPunch" {
		return nil
	}
	switch p.ID {
	case packetid.ServerboundPlayClientTickEnd, packetid.ServerboundPlayPong,
		packetid.ServerboundPlayClientInformation, packetid.ServerboundPlayChunkBatchReceived,
		packetid.ServerboundPlayPlayerLoaded, packetid.ServerboundPlayPlayerInput,
		packetid.ServerboundPlayCustomPayload: // the client's brand
		return nil // known: the server keeps nothing of them yet
	case packetid.ServerboundPlayKeepAlive:
		var k play.ServerboundKeepAlive
		if err := p.Scan(&k); err != nil {
			return err
		}
		if int64(k.ID) != g.keptAt {
			return fmt.Errorf("simworld: the bot answered the keep-alive %d, the one sent was %d", k.ID, g.keptAt)
		}
		g.keptAt = 0
		return nil
	case packetid.ServerboundPlayBundleItemSelected:
		// (id 3 with no data is the configuration's own "finish", its last packet, which the
		// fake server reads as the first of the play state: no bundle was selected)
		if len(p.Data) == 0 {
			return nil
		}
	case packetid.ServerboundPlaySetCarriedItem:
		var c play.SetCarriedItem
		if err := p.Scan(&c); err != nil {
			return err
		}
		if c.Slot < 0 || c.Slot > 8 {
			return fmt.Errorf("simworld: hotbar slot %d", c.Slot)
		}
		g.held = int(c.Slot)
		g.eating = nil
		return nil
	case packetid.ServerboundPlayPlayerCommand:
		var c play.PlayerCommand
		if err := p.Scan(&c); err != nil {
			return err
		}
		switch c.Action {
		case types.PlayerCommandActionStartSprinting:
			g.sprinting = true
		case types.PlayerCommandActionStopSprinting:
			g.sprinting = false
		}
		return nil
	case packetid.ServerboundPlayAttack:
		var a play.Attack
		if err := p.Scan(&a); err != nil {
			return err
		}
		return g.attackMob(int32(a.EntityID))
	case packetid.ServerboundPlayClientCommand:
		var c play.ClientCommand
		if err := p.Scan(&c); err != nil {
			return err
		}
		if c.Action == types.ClientCommandActionPerformRespawn {
			return g.respawn()
		}
		return fmt.Errorf("simworld: not modelled: the client command %v", c.Action)
	case packetid.ServerboundPlayUseItem:
		var u play.UseItem
		if err := p.Scan(&u); err != nil {
			return err
		}
		if h := g.heldItem().item; h == "minecraft:bucket" || h == "minecraft:water_bucket" {
			return g.useBucket(u)
		}
		if slot := armorSlot(g.heldItem().item); slot > 0 {
			return g.wear(slot, u.Sequence)
		}
		return g.startEating(u)
	case packetid.ServerboundPlayUseItemOn:
		return g.useItemOn(p)
	case packetid.ServerboundPlayContainerClose:
		var c play.ServerboundContainerClose
		if err := p.Scan(&c); err != nil {
			return err
		}
		return g.closeMenu(int(c.ContainerID))
	case packetid.ServerboundPlayPlaceRecipe:
		var r play.PlaceRecipe
		if err := p.Scan(&r); err != nil {
			return err
		}
		return g.placeRecipe(r)
	case packetid.ServerboundPlayContainerClick:
		var c play.ContainerClick
		if err := p.Scan(&c); err != nil {
			return err
		}
		return g.click(c)
	case packetid.ServerboundPlayPlayerAction:
		var a play.PlayerAction
		if err := p.Scan(&a); err != nil {
			return err
		}
		return g.playAction(a)
	case packetid.ServerboundPlayAcceptTeleportation:
		var a play.AcceptTeleportation
		if err := p.Scan(&a); err != nil {
			return err
		}
		if int32(a.ID) == g.tp {
			g.tpWait = false
		}
		return nil
	case packetid.ServerboundPlayMovePlayerPos:
		var m play.MovePlayerPos
		if err := p.Scan(&m); err != nil {
			return err
		}
		return g.move(float64(m.X), float64(m.Y), float64(m.Z), m.Flags.OnGround)
	case packetid.ServerboundPlayMovePlayerPosRot:
		var m play.MovePlayerPosRot
		if err := p.Scan(&m); err != nil {
			return err
		}
		g.yaw = float64(m.YRot)
		return g.move(float64(m.X), float64(m.Y), float64(m.Z), m.Flags.OnGround)
	case packetid.ServerboundPlayMovePlayerRot:
		var m play.MovePlayerRot
		if err := p.Scan(&m); err != nil {
			return err
		}
		g.yaw = float64(m.YRot)
		return nil
	case packetid.ServerboundPlayMovePlayerStatusOnly:
		var m play.MovePlayerStatusOnly
		if err := p.Scan(&m); err != nil {
			return err
		}
		return g.move(g.pos[0], g.pos[1], g.pos[2], m.Value.OnGround)
	}
	return fmt.Errorf("simworld: not modelled: %v", p.ID)
}

// move is a move the bot sent: turned back if the player would be inside
// blocks, else taken — and the fall counted, its damage dealt on landing.
func (g *Game) move(x, y, z float64, onGround bool) error {
	if g.tpWait { // the game ignores moves till the teleport is confirmed
		return nil
	}
	if g.collides(x, y, z) {
		return g.turnBack()
	}
	dy := y - g.pos[1]
	if g.sprinting && onGround { // (the game counts a sprint by the block)
		g.exhaust(float32(math.Hypot(x-g.pos[0], z-g.pos[2])) * exhaustSprint)
	}
	if g.onGround && !onGround && dy > 0 { // a jump
		if g.sprinting {
			g.exhaust(exhaustSprintJmp)
		} else {
			g.exhaust(exhaustJump)
		}
	}
	g.pos = [3]float64{x, y, z}
	// Entity.checkFallDamage: falling adds the drop; landing deals what is past three blocks —
	// in water the fall is forgotten (Entity.updateInWaterStateAndDoFluidPushing: resetFallDistance;
	// a swim's bobbing counted as falls once: four hearts lost climbing out of a pond)
	if g.bodyInWater() {
		g.fall = 0
	}
	if onGround {
		if g.fall > 3 {
			if err := g.hurt(float32(math.Ceil(g.fall - 3))); err != nil {
				return err
			}
		}
		g.fall = 0
	} else if dy < 0 {
		g.fall -= dy
	}
	g.onGround = onGround
	return nil
}

// turnBack puts the player where the server last had it.
func (g *Game) turnBack() error {
	g.tp++
	g.tpWait = true
	at := play.PlayerPosition{ID: pk.VarInt(g.tp), Change: types.PositionMoveRotation{Position: types.Vec3{X: pk.Double(g.pos[0]), Y: pk.Double(g.pos[1]), Z: pk.Double(g.pos[2])}}}
	return g.S.Send(at)
}

// hurt takes health off and tells the bot.
func (g *Game) hurt(d float32) error {
	before := g.toldHealth
	g.Health = max(0, g.Health-d)
	g.exhaust(exhaustHurt)
	return g.tellHealth(before)
}

// collides reports whether the player's box (0.6 wide, 1.8 high) at x, y, z is inside a block.
func (g *Game) collides(x, y, z float64) bool {
	const w, h = 0.3, 1.8
	const eps = 1e-7
	for bx := int(math.Floor(x - w + eps)); bx <= int(math.Floor(x+w-eps)); bx++ {
		for by := int(math.Floor(y + eps)); by <= int(math.Floor(y+h-eps)); by++ {
			for bz := int(math.Floor(z - w + eps)); bz <= int(math.Floor(z+w-eps)); bz++ {
				for _, c := range block.CollisionShape(g.S.World.At(bx, by, bz)) {
					if x+w > float64(bx)+c.MinX+eps && x-w < float64(bx)+c.MaxX-eps &&
						y+h > float64(by)+c.MinY+eps && y < float64(by)+c.MaxY-eps &&
						z+w > float64(bz)+c.MinZ+eps && z-w < float64(bz)+c.MaxZ-eps {
						return true
					}
				}
			}
		}
	}
	return false
}

// sendChunks keeps the chunks within the view of the player's chunk sent, and
// forgets the ones that left it.
func (g *Game) sendChunks() error {
	cx, cz := int(math.Floor(g.pos[0]))>>4, int(math.Floor(g.pos[2]))>>4
	if g.centre == [2]int{cx, cz} {
		return nil // (the join sent the view round the spawn, in chunk 0 0)
	}
	g.centre = [2]int{cx, cz}
	v := g.opt.View
	if err := g.S.Send(play.SetChunkCacheCenter{X: pk.VarInt(cx), Z: pk.VarInt(cz)}); err != nil {
		return err
	}
	for c := range g.sent {
		if abs(c[0]-cx) > v || abs(c[1]-cz) > v {
			if err := g.S.Send(play.ForgetLevelChunk{Pos: types.ChunkPos{int32(c[0]), int32(c[1])}}); err != nil {
				return err
			}
			delete(g.sent, c)
		}
	}
	var fresh [][2]int
	for x := cx - v; x <= cx+v; x++ {
		for z := cz - v; z <= cz+v; z++ {
			if !g.sent[[2]int{x, z}] {
				fresh = append(fresh, [2]int{x, z})
			}
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	if err := g.S.Send(play.ChunkBatchStart{}); err != nil {
		return err
	}
	for _, c := range fresh {
		if err := g.S.Send(g.S.World.Chunk(c[0], c[1])); err != nil {
			return err
		}
		g.sent[c] = true
	}
	return g.S.Send(play.ChunkBatchFinished{BatchSize: pk.VarInt(len(fresh))})
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// setup plays a stage's line on the world: "fill x1 y1 z1 x2 y2 z2 block", the same
// "replace air" (only where there is air), and "setblock x y z block"; a block is
// written as the game writes it, with the properties it sets in brackets.
func setup(w *fakeserver.World, line string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("simworld: %q: %v", line, r)
		}
	}()
	f := strings.Fields(line)
	num := func(i int) int {
		n, e := strconv.Atoi(f[i])
		if e != nil {
			panic(e)
		}
		return n
	}
	switch {
	case (len(f) == 8 || len(f) == 10 && f[8] == "replace" && f[9] == "minecraft:air") && f[0] == "fill":
		st := stateOfText(f[7])
		onlyAir := len(f) == 10
		for x := min(num(1), num(4)); x <= max(num(1), num(4)); x++ {
			for y := min(num(2), num(5)); y <= max(num(2), num(5)); y++ {
				for z := min(num(3), num(6)); z <= max(num(3), num(6)); z++ {
					if !onlyAir || w.At(x, y, z) == 0 {
						w.SetState(x, y, z, st)
					}
				}
			}
		}
	case len(f) == 5 && f[0] == "setblock":
		w.SetState(num(1), num(2), num(3), stateOfText(f[4]))
	default:
		return fmt.Errorf("simworld: not modelled: the world line %q", line)
	}
	return nil
}

// stateOfText is the state a command's block names: "minecraft:oak_leaves[persistent=true]". What
// the brackets do not set is the block's default — for leaves, a distance of 7; any other block
// with a property left open is not modelled.
func stateOfText(text string) block.StateID {
	name, props, _ := strings.Cut(strings.TrimSuffix(text, "]"), "[")
	if !strings.HasPrefix(name, "minecraft:") {
		name = "minecraft:" + name
	}
	if props == "" {
		return fakeserver.State(name)
	}
	var found []block.StateID
	for i := range block.StateList {
		if block.StateList[i].ID() != name {
			continue
		}
		s := world.StateString(block.StateID(i))
		ok := true
		for _, p := range strings.Split(props, ",") {
			ok = ok && (strings.Contains(s, "["+p+",") || strings.Contains(s, ","+p+",") || strings.Contains(s, ","+p+"]") || strings.Contains(s, "["+p+"]"))
		}
		if ok {
			found = append(found, block.StateID(i))
		}
	}
	if len(found) == 0 {
		panic("no such state: " + text)
	}
	if strings.HasSuffix(name, "_leaves") { // (distance 7 is the default: the last of 1..7)
		return found[len(found)-1]
	}
	if len(found) > 1 {
		panic("not modelled: the default of the properties left open in " + text)
	}
	return found[0]
}

// tellTime sends the world's age and, when the game has a day, the overworld clock: where it is and
// how fast it runs (a tick a tick, or held).
func (g *Game) tellTime() error {
	st := play.SetTime{GameTime: pk.Long(g.Tick)}
	if g.day != nil {
		total, rate := g.day.Time, float32(0)
		if g.day.Run {
			total, rate = g.day.Time+int64(g.Tick), 1
		}
		st.ClockUpdates = append(st.ClockUpdates, types.Entry[pk.VarInt, types.ClockNetworkState]{Key: 0,
			Val: types.ClockNetworkState{TotalTicks: pk.VarLong(total), Rate: pk.Float(rate)}})
	}
	return g.S.Send(st)
}

// DayTime is the time of day now, 0 to 23999.
func (g *Game) DayTime() int64 {
	if g.day == nil {
		return 0
	}
	t := g.day.Time
	if g.day.Run {
		t += int64(g.Tick)
	}
	return ((t % 24000) + 24000) % 24000
}

// setupLine plays one of a stage's lines: the world's (fill, setblock), the clock ("time set 11800", "time set
// noon": held there), and what the player is given ("give Builder minecraft:oak_log 16").
func (g *Game) setupLine(line string) error {
	f := strings.Fields(line)
	switch {
	case len(f) == 3 && f[0] == "time" && f[1] == "set":
		times := map[string]int64{"day": 1000, "noon": 6000, "night": 13000, "midnight": 18000}
		n, ok := times[f[2]]
		if !ok {
			v, err := strconv.ParseInt(f[2], 10, 64)
			if err != nil {
				return fmt.Errorf("simworld: %q: %v", line, err)
			}
			n = v
		}
		run := g.day != nil && g.day.Run
		g.day = &Day{Time: n, Run: run}
		return nil
	case len(f) >= 3 && f[0] == "give":
		n := 1
		if len(f) == 4 {
			v, err := strconv.Atoi(f[3])
			if err != nil {
				return fmt.Errorf("simworld: %q: %v", line, err)
			}
			n = v
		}
		item := f[2]
		if !strings.HasPrefix(item, "minecraft:") {
			item = "minecraft:" + item
		}
		g.gives = append(g.gives, slotItem{item: item, n: n})
		return nil
	}
	return setup(g.S.World, line)
}

// bubbles counts the games run by Run: while one is, a game keeps to memory and settles by the runtime.
var bubbles atomic.Int32

// Run plays a game in a testing/synctest bubble (DETERMINISM.md): every goroutine of the game, the bot's
// included, is in it, time in it is virtual, and a game's Step waits for the bot with synctest.Wait — the
// runtime's own knowledge that every goroutine is durably blocked, not a guess from stack texts. Start and
// NewServer called in f join over memory. Games run one at a time (as go test runs a package's tests): the
// bubble count is process-wide.
func Run(t *testing.T, f func(t *testing.T)) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		bubbles.Add(1)
		defer bubbles.Add(-1)
		f(t)
	})
}
