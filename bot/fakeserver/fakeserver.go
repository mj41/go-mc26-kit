// Package fakeserver is a test helper: a Minecraft server inside the test's
// own process, on the loopback, that a bot of this kit logs in to. It speaks
// the version's handshake, an offline login and the configuration (the
// version's registries), puts the player into an overworld (the play login,
// a position), and from then on records every packet the bot sends, while the
// test sends the bot whatever packets it needs.
//
// No game runs on it: nothing happens unless the test plays the server's part
// (a screen opened, a block changed, an item given). It is not a server to
// play on, it starts no Java and opens no port but a loopback one; a test
// with it takes well under a second.
//
// Start serves one for a test (closed when the test ends), StartMem one in
// memory for a test in a testing/synctest bubble, and Listen one for a program
// with no testing.T, closed with Close (ExampleListen). Chat tells the player a
// player's message, Kick kicks it, Said waits for what the player says.
//
// One player by default; with MaxPlayers more, each a Player (Players, Player, WaitPlayers), the world
// shared, and every player told of the others' arrival, chat and leaving as a vanilla server tells them
// (MULTIPLAYER.md). The single-player calls are the first player's.
//
// The bot's side is Join's: a client, its player, a world, the controller
// ticking, the screens — the parts the bot's own code is built on.
package fakeserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/clock"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/path"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/recipes"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/simworld/memnet"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26-kit/server"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/version"
	mcnet "github.com/mj41/go-mc26/net"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
	"github.com/mj41/go-mc26/registry"
	"github.com/mj41/go-mc26/yggdrasil/user"
)

// FastTick is the tick of a bot that Join puts on the fake server: a fifth
// of the game's, its ticks the same (a walk, a dig take as many), done sooner.
const FastTick = control.TickInterval / 5

// Spawn is where the player is put: the overworld's ground of a flat world.
var Spawn = [3]float64{0.5, 64, 0.5}

// Packet is one packet the bot sent, with when it came and who sent it.
type Packet struct {
	ID   packetid.ServerboundPacketID
	Data []byte
	At   time.Time
	From string // the name of the player who sent it
}

// Scan decodes the packet into fields (play.ContainerClick, …).
func (p Packet) Scan(fields ...pk.FieldDecoder) error {
	return pk.Packet{ID: int32(p.ID), Data: p.Data}.Scan(fields...)
}

// Server is the fake server of one test: its address, its players (one, unless MaxPlayers says more), the
// packets they sent.
type Server struct {
	Addr string
	// Dialer, when set, is how a client joins: the server listens on a network in memory (StartMem) and a client
	// dials it with this (Join does). Nil: the server is on a loopback TCP port at Addr.
	Dialer mcnet.MCDialer
	// World is what its chunks hold, sent when the bot joins: set it up
	// before Join (World.Set, World.Fill); after it, SetBlock.
	World *World
	// Digs plays the game's part in digging: a dig the bot finishes turns the
	// block to air, told back as the game does (the block, the sequence's
	// ack). Off, a dig changes nothing unless the test says so.
	Digs bool
	// Hook, when set, is called with every packet the bot sends, in the order
	// they were read, from the goroutine that reads them (a game that plays
	// the server's part queues them and answers when it ticks: bot/simworld).
	Hook func(Packet)
	// MaxPlayers is how many players the server takes at once (0: one). Set it before the first client joins;
	// past it a client is refused at login, as a full vanilla server refuses it. With more than one, the server
	// tells the players of each other — who joined and left, their chat (MULTIPLAYER.md).
	MaxPlayers int
	// Fishing, when set, plays the server's part in fishing (FISHING.md): set it before the player joins.
	Fishing *Fishing
	// KeepAlive, when set, is how often the server sends each player a keep-alive, as a vanilla server does
	// every 15 s. Unset, as by default, it sends none: a test that runs longer than the client's keep-alive
	// timeout (basic.KeepAliveTimeout, 20 s) sets it, or the client gives the connection up.
	KeepAlive time.Duration

	listener *mcnet.Listener

	mu      sync.Mutex
	first   *Player   // made with the server: the first client to join is this player
	players []*Player // in play, in join order
	lastID  int32     // the last entity id given
}

// Start serves until the test ends.
func Start(t testing.TB) *Server {
	t.Helper()
	s, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// Listen serves a fake server on a loopback port, for a program that has no testing.T — a godoc example, a
// bot's own tool — the way net/http's httptest.NewServer serves an HTTP one: a client joins Addr (or with
// Join), one player at a time, in offline mode, into a flat world round Spawn. Close it when done
// (defer s.Close()), or the port stays open.
func Listen() (*Server, error) {
	l, err := mcnet.ListenMC("127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	return serve(l, l.Addr().String(), nil), nil
}

// Close stops the server: no more clients, and the players' connections closed.
func (s *Server) Close() error {
	err := s.listener.Close()
	s.mu.Lock()
	ps := append([]*Player{s.first}, s.players...)
	s.mu.Unlock()
	for _, p := range ps {
		p.close()
	}
	return err
}

// StartMem is Start on a network in memory (bot/simworld/memnet): no port, no socket. A client joins with the
// server's Join. It is what a game in a testing/synctest bubble needs — a goroutine blocked on a real socket is
// never durably blocked, so the bubble's Wait would never return.
func StartMem(t testing.TB) *Server {
	t.Helper()
	l := memnet.Listen("mem-server")
	s := serve(l.MC(), "mem-server", memnet.Dialer{L: l})
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// Join joins the client to the server, over its network.
func (s *Server) Join(c *bot.Client) error {
	if s.Dialer == nil {
		return c.JoinServer(s.Addr)
	}
	return c.JoinServerWithOptions(s.Addr, bot.JoinOptions{MCDialer: s.Dialer})
}

func serve(l *mcnet.Listener, addr string, dialer mcnet.MCDialer) *Server {
	s := &Server{Addr: addr, Dialer: dialer, World: NewWorld(), listener: l, first: newPlayer()}
	srv := &server.Server{
		ListPingHandler: struct {
			*server.PingInfo
			*roster
		}{server.NewPingInfo("fake", version.ProtocolVersion, chat.Text("a test's server"), nil), (*roster)(s)},
		LoginHandler:  &server.MojangLoginHandler{OnlineMode: false, Threshold: -1, LoginChecker: (*roster)(s)},
		ConfigHandler: &config{registries()},
		GamePlay:      s,
	}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go srv.AcceptConn(&conn)
		}
	}()
	return s
}

// AcceptPlayer is the play state of a client that logged in: put in the overworld at Spawn (told of the
// other players, and they of it, when the server takes more than one), then every packet it sends recorded
// till it leaves.
func (s *Server) AcceptPlayer(name string, id uuid.UUID, _ *user.PublicKey, _ []user.Property, _ int32, conn *mcnet.Conn) {
	p := s.admit(name, id, conn)
	if p == nil { // two logins at once passed the check: the second is refused here, for the same reason
		full := play.Disconnect{Value: types.Text(chat.TranslateMsg("multiplayer.disconnect.server_full"))}
		_ = conn.WritePacket(pk.Marshal(full.PacketID(), full))
		return
	}
	defer s.leave(p)
	login := play.Login{
		PlayerID: pk.Int(p.EntityID), Levels: []pk.Identifier{"minecraft:overworld"}, MaxPlayers: pk.VarInt(s.maxPlayers()), ChunkRadius: 2, SimulationDistance: 2,
		ShowDeathScreen: true,
		CommonPlayerSpawnInfo: types.CommonPlayerSpawnInfo{DimensionType: 0, Dimension: "minecraft:overworld",
			GameType: 0, PreviousGameType: -1, SeaLevel: 63},
	}
	if err := p.Send(login); err != nil {
		return
	}
	at := play.PlayerPosition{ID: 1, Change: types.PositionMoveRotation{Position: types.Vec3{X: pk.Double(Spawn[0]), Y: pk.Double(Spawn[1]), Z: pk.Double(Spawn[2])}}}
	if err := p.Send(at); err != nil {
		return
	}
	if err := s.sendChunks(p); err != nil {
		return
	}
	if s.multi() {
		s.presence(p)
	}
	close(p.joined)
	if s.KeepAlive > 0 {
		go s.keepAlive(p)
	}
	for {
		var raw pk.Packet
		if err := conn.ReadPacket(&raw); err != nil {
			return
		}
		got := Packet{ID: packetid.ServerboundPacketID(raw.ID), Data: append([]byte(nil), raw.Data...), At: time.Now(), From: p.Name}
		s.mu.Lock()
		digs, hook := s.Digs, s.Hook
		s.mu.Unlock()
		if hook != nil {
			hook(got)
		}
		if p.Hook != nil {
			p.Hook(got)
		}
		if digs && got.ID == packetid.ServerboundPlayPlayerAction {
			s.dig(p, got)
		}
		if s.multi() && got.ID == packetid.ServerboundPlayChat {
			s.relayChat(p, got)
		}
		if got.ID == packetid.ServerboundPlayChunkBatchReceived {
			s.batchReceived(p)
		}
		if s.Fishing != nil && got.ID == packetid.ServerboundPlayUseItem {
			s.Fishing.use(s, p)
		}
		// recorded after the server's answer to it: a test that waits for the
		// packet (WaitFor) finds the world as the server leaves it
		p.mu.Lock()
		p.got = append(p.got, got)
		p.mu.Unlock()
		p.signal()
	}
}

// keepAlive sends the player a keep-alive every KeepAlive till it leaves.
func (s *Server) keepAlive(p *Player) {
	tick := time.NewTicker(s.KeepAlive)
	defer tick.Stop()
	for id := int64(1); ; id++ {
		select {
		case <-p.left:
			return
		case <-tick.C:
			if p.Send(play.ClientboundKeepAlive{ID: pk.Long(id)}) != nil {
				return
			}
		}
	}
}

// sendChunks sends a player the world's chunks round the spawn: the first batch of its chunk sender (chunks.go),
// the rest as the client answers the batches.
func (s *Server) sendChunks(p *Player) error {
	if err := s.moveView(p, 0, 0); err != nil {
		return err
	}
	// the level's chunks are loading: from this event the client waits for
	// its own chunk, then tells it is in (PlayerLoaded) — as the game does
	return p.Send(play.GameEvent{Event: gameEventLevelChunksLoadStart})
}

// gameEventLevelChunksLoadStart is ClientboundGameEventPacket.LEVEL_CHUNKS_LOAD_START.
const gameEventLevelChunksLoadStart = 13

// SetBlock changes a block of the world while the bot is in it, told to every player as the game tells it
// (a block update).
func (s *Server) SetBlock(x, y, z int, name string) error {
	st := State(name)
	s.World.setState(x, y, z, st)
	return s.Broadcast(play.BlockUpdate{Pos: pk.Position{X: x, Y: y, Z: z}, BlockState: pk.VarInt(st)})
}

// dig is the game's part in a dig (Digs): a dig finished (or started on
// what breaks at once) turns the block to air, the change and the sequence's
// ack told back.
func (s *Server) dig(by *Player, p Packet) {
	var a play.PlayerAction
	if err := p.Scan(&a); err != nil {
		return
	}
	switch a.Action {
	case types.PlayerActionActionStopDestroyBlock:
	default:
		return
	}
	x, y, z := a.Pos.X, a.Pos.Y, a.Pos.Z
	s.World.setState(x, y, z, 0)
	_ = s.Broadcast(play.BlockUpdate{Pos: a.Pos, BlockState: 0}) // the block, to everyone in view
	_ = by.Send(play.BlockChangedAck{Sequence: a.Sequence})      // the ack, to the digger
}

// Joined waits for the bot (the first player) to be in play, put at Spawn.
func (s *Server) Joined(timeout time.Duration) error {
	select {
	case <-s.the().joined:
		return nil
	case <-time.After(timeout):
		return errors.New("fakeserver: no bot joined")
	}
}

// Clientbound is a packet the server sends (play.OpenScreen, …).
type Clientbound interface {
	PacketID() packetid.ClientboundPacketID
	pk.FieldEncoder
}

// Send sends the bot (the first player) a packet.
func (s *Server) Send(p Clientbound) error { return s.the().Send(p) }

// SendRaw sends the bot a packet already made.
func (s *Server) SendRaw(p pk.Packet) error { return s.the().SendRaw(p) }

// Got is every packet of the kind the bot sent so far.
func (s *Server) Got(id packetid.ServerboundPacketID) []Packet { return s.the().Got(id) }

// WaitFor waits for the n-th packet (from 1) of the kind the bot sends.
func (s *Server) WaitFor(ctx context.Context, id packetid.ServerboundPacketID, n int) (Packet, error) {
	return s.the().WaitFor(ctx, id, n)
}

// Bot is a bot of the kit joined to the fake server, its parts as the bot's
// own code puts them together, its controller ticking.
type Bot struct {
	C        *bot.Client
	Player   *basic.Player
	World    *world.World
	Ctl      *control.Controller
	Screens  *screen.Manager
	Act      *act.Screens
	Book     *recipes.Book     // the recipe book the server shows
	Hands    *act.Hands        // ticked by the controller, as the bot's own are
	Entities *entities.Manager // the entities the server says
	Mover    *physics.Mover    // its physics: the controller's step
	Walker   *path.Walker      // its walks: the controller's think
	Clock    *clock.Clock      // the game time and the day the server says
	Steps    *control.Stepped  // the controller's clock when the bot joined stepped (JoinStepped): its ticks come only from Step; nil: the wall's

	mu  sync.Mutex
	err error // why its game ended, once it has
}

// Err is why the bot's game ended (nil: it goes on) — a packet the client
// could not read ends it.
func (b *Bot) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

// Join logs a bot named name in to s and runs it till the test ends.
func Join(t testing.TB, s *Server, name string) *Bot {
	t.Helper()
	return join(t, s, name, nil)
}

// JoinStepped is Join with the controller on a stepped clock: its ticks are
// the game's length and come only when the test steps them (Bot.Step, Loaded,
// Until) — as many as it asks, as fast as the machine goes.
func JoinStepped(t testing.TB, s *Server, name string) *Bot {
	t.Helper()
	return join(t, s, name, control.NewStepped(time.Unix(0, 0)))
}

// JoinSteppedOn is JoinStepped on a clock the test made: a game that steps
// the server and the client together owns it.
func JoinSteppedOn(t testing.TB, s *Server, name string, steps *control.Stepped) *Bot {
	t.Helper()
	return join(t, s, name, steps)
}

func join(t testing.TB, s *Server, name string, steps *control.Stepped) *Bot {
	t.Helper()
	c := bot.NewClient()
	c.Auth.Name = name
	b := &Bot{C: c}
	b.Player = basic.NewPlayer(c, basic.DefaultSettings, basic.EventsListener{})
	if steps != nil {
		b.Player.KeepAliveTimeout = -1 // a stepped game has no wall time to keep: it ends only as its server ends it
	}
	b.World = world.NewWorld(c, b.Player, world.EventsListener{})
	b.Ctl = control.New(c, b.Player, b.World)
	if steps != nil {
		b.Ctl.Clock, b.Steps = steps, steps
	} else {
		b.Ctl.Tick = FastTick // the same ticks, faster: the fake server checks no speed
	}
	b.Screens = screen.NewManager(c, screen.EventsListener{})
	b.Book = recipes.New(c)
	b.Entities = entities.New(c, entities.Events{})
	b.Act = &act.Screens{C: c, Ctl: b.Ctl, Screens: b.Screens, Book: b.Book}
	b.Hands = &act.Hands{C: c, World: b.World, Screens: b.Screens}
	b.Ctl.Act = b.Hands.Act
	tags := physics.ClientTags{T: &c.Tags}
	b.Mover = &physics.Mover{World: b.World, Tags: tags, Food: func() int { return int(b.Player.Status().Food) }}
	b.Walker = &path.Walker{Graph: &path.Graph{World: b.World, Tags: tags}}
	b.Ctl.Think, b.Ctl.Step = b.Walker.Think, b.Mover.Step
	b.Clock = clock.New(c)
	if steps != nil {
		b.Clock.Now = steps.Now // (its time is the stepped clock's)
	}
	if err := s.Join(c); err != nil {
		t.Fatalf("fakeserver: join: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		err := c.HandleGame() // never nil: it returns when the game ends
		b.mu.Lock()
		b.err = err
		b.mu.Unlock()
		if ctx.Err() == nil {
			t.Logf("fakeserver: the bot's game ended: %v", err)
		}
		close(done)
	}()
	go func() { _ = b.Ctl.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = c.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	if err := s.joinedAs(name, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	return b
}

// Loaded waits for the bot to be in its level: its chunk come, the
// controller's PlayerLoaded sent (its hands act from then on).
func (b *Bot) Loaded(timeout time.Duration) error {
	if b.Steps != nil {
		if !b.Until(int(timeout/control.TickInterval), func() bool { return b.Ctl.State().Loaded }) {
			return errors.New("fakeserver: the bot's level never loaded")
		}
		return nil
	}
	for t0 := time.Now(); time.Since(t0) < timeout; time.Sleep(5 * time.Millisecond) {
		if b.Ctl.State().Loaded {
			return nil
		}
		if err := b.Err(); err != nil {
			return fmt.Errorf("fakeserver: the bot's game ended: %w", err)
		}
	}
	return errors.New("fakeserver: the bot's level never loaded")
}

// registries are the version's registries the client is sent, the overworld's
// dimension type in them: its chunks are read by its height (y −64, 384 high).
func registries() registry.Registries {
	r := registry.NewNetworkCodec()
	var dark int32 // monsters spawn in light 0 (a value: an empty Either is no NBT)
	overworld := registry.DimensionType{HasSkylight: true, CoordinateScale: 1,
		MinY: -64, Height: 384, LogicalHeight: 384}
	overworld.MonsterSpawnLightLevel = registry.Either[int32, registry.IntProviders]{Left: &dark}
	r.DimensionType.Put("minecraft:overworld", overworld)
	r.WorldClock.Put("minecraft:overworld", registry.WorldClock{}) // (the day: its clock is told in SetTime)
	// the damage types a game tells (simworld's damageTypes, in that order: a type is its place in it)
	for _, n := range []string{"minecraft:generic", "minecraft:mob_attack", "minecraft:player_attack", "minecraft:fall", "minecraft:arrow", "minecraft:explosion", "minecraft:on_fire"} {
		scaling := registry.DamageScalingWhenCausedByLivingNonPlayer
		if n == "minecraft:explosion" {
			scaling = registry.DamageScalingAlways // (the data pack's damage_type/explosion.json)
		}
		r.DamageType.Put(n, registry.DamageType{MessageID: strings.TrimPrefix(n, "minecraft:"), Scaling: scaling, Exhaustion: 0.1})
	}
	return r
}

// Step runs n ticks of a bot that joined stepped.
func (b *Bot) Step(n int) {
	for i := 0; i < n; i++ {
		if err := b.Steps.Step(control.TickInterval); err != nil {
			panic(err)
		}
		time.Sleep(200 * time.Microsecond) // the server's packets are read in goroutines of their own
	}
}

// Until steps one tick at a time, at most max, till done holds: it says whether it did.
func (b *Bot) Until(max int, done func() bool) bool {
	for i := 0; i < max; i++ {
		if done() {
			return true
		}
		b.Step(1)
	}
	return done()
}
