// Robot is a bot that plays the way a person with the vanilla client does: it
// ticks 20 times a second and sends what the client sends (bot/control), and
// it grows a command for every thing it learns to do.
//
// It reads commands on stdin, one per line, and answers each on one line that
// starts with "robot: ", so a test can drive it and check the answers:
//
//	pos                 where the robot is: x y z yaw pitch
//	state               the controller's state: loaded, on ground, ticks, the dimension, what it rides
//	                    (a boat it steers with its position) and the corrections the server sent
//	look <yaw> <pitch>  turn
//	keys <keys> <ticks> [<then>]
//	                    hold keys (forward,back,left,right,jump,shift,sprint or none) for ticks, then let go
//	                    (or hold the keys <then>);
//	                    answers where it ended: x y z onGround
//	wait <ticks>        let ticks pass
//	inv                 the inventory by the player's slot index (0–8 hotbar, 9–35, 36–39 armor, 40 offhand)
//	                    and the selected hotbar slot: held=N i=item*count …
//	hold <0-8>          select a hotbar slot
//	status              health, food, experience level and the effects
//	nearby [radius]     the entities around, closest first: id type x y z; …
//	goto <x> <y> <z>    walk there (or next to it); answers arrived or failed, and where it ended
//	follow <player>     keep within two blocks of a player, until stop
//	stop                stop walking
//	dig <x> <y> <z>     break the block with the held item: ticks=N was=<state>
//	collect [radius]    walk to the dropped items around and pick them up: the count picked up
//	place <x> <y> <z>   put the held block there, against a block next to it: the state placed
//	use <x> <y> <z>     right-click the block (a door, a button): its state after
//	eat                 eat or drink the held item to the end
//	open <x> <y> <z>    open the chest, crafting table or furnace there: id type slots
//	close               close it
//	craft <item> [n]    make n (1) of the item from the recipe book, in the open crafting table or the
//	                    inventory's grid: how many were made
//	store <item>        shift-click every stack of the item into the open container: how many moved
//	take <item>         shift-click every stack of the item out of it
//	smelt <in> <fuel> [n] smelt in the open furnace, take the n results
//	guard [radius] [seconds]
//	                    fight the hostile mobs around with the best weapon in the hotbar until none is
//	                    left: killed=N
//	get <item> [n]      have n (1) of the item: from the inventory, crafted from the recipe book (the
//	                    ingredients got first, a crafting table placed when the recipe needs one), or dug
//	recipe <item>       the book's recipes for the item and what their ingredients take
//	click <slot> [right|shift]
//	                    click a slot of the open menu (the inventory's: 9–35, hotbar 36–44)
//	updates             how many single slots the server has sent (a right prediction brings none)
//	sign <x> <y> <z> <line|line|line|line>
//	                    place the held sign there and write its front
//	interact <entity id> right-click the entity with the held item
//	trade <entity id> <offer> [times]
//	                    open the villager and buy one of its offers
//	build hut [block]   get 25 of the block (cobblestone) and wall the robot in on level ground: a ring
//	                    two high, a roof (a hillside counts as wall)
//	block <x> <y> <z>   the block state there, as a command writes it
//	find <block>...     where the nearest of the blocks is, as the goals look for it
//	entities [radius]   the tracked entities: id type x y z, the name in their data, how many data values
//	                    and equipment slots, the data values their type's layout does not have
//	seen [reset]        the entity types the server added since the last reset, with counts
//	come <player>       walk to where a player stands
//	useon <x> <y> <z>   use the held item on a block that does not change for it (a minecart on a rail)
//	usetoward <x> <y> <z>
//	                    aim at the block's top and use the held item (a boat onto water)
//	dismount            get off the vehicle (shift)
//	say <text>          chat
//	/<command>          a command, as typed in the chat box
//	quit                leave the server
//
// Players command it in chat too: "robot <command>" in public chat, or the
// command whispered (/msg Robot <command>); it answers the same way, and
// follow and come without a name mean the sender. README.md tells what it
// can do and how it is tested.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"math"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/advancements"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/clock"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/msg"
	"github.com/mj41/go-mc26-kit/bot/path"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/playerlist"
	"github.com/mj41/go-mc26-kit/bot/recipes"
	"github.com/mj41/go-mc26-kit/bot/ride"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/entity"
	"github.com/mj41/go-mc26/level/block"
)

var (
	address  = flag.String("address", "127.0.0.1:25565", "The server address")
	name     = flag.String("name", "Robot", "The player's name")
	eventsTo = flag.String("events", "", "append the robot's events to this file, JSON lines (see events.go)")
	eventsHz = flag.Int("events-hz", 2, "state events a second, 2 to 20 (smooth replays); views stay every two seconds")
	memoryAt = flag.String("memory", "", "keep what it knows of its world in this file, and start from it (memory.go)")
)

// robot is everything the commands work with.
type robot struct {
	client                   *bot.Client
	player                   *basic.Player
	world                    *world.World
	ctl                      *control.Controller
	chat                     *msg.Manager
	screens                  *screen.Manager
	entities                 *entities.Manager
	players                  *playerlist.PlayerList
	walker                   *path.Walker
	hands                    *act.Hands
	book                     *recipes.Book
	ui                       *act.Screens
	signs                    *act.Signs
	pickedUp                 atomic.Int32
	deaths, orderDeaths      atomic.Int32 // deaths so far; and when the running order began
	splashes                 chan splash  // bobbers' splashes heard (fishing)
	seenMu                   sync.Mutex
	cmdMu                    sync.Mutex // one command at a time (runOrder)
	changeMu                 sync.Mutex // the block changes not told yet (changes events)
	changed                  map[world.BlockPos]block.StateID
	seen                     map[string]int // the types of the entities added since "seen reset"
	stuck                    map[int32]bool // the dropped items collect could not get to (under seenMu)
	orders                   chan order     // commands players gave in chat (obey)
	rider                    *ride.Rider
	clock                    *clock.Clock
	home                     *room                   // the shelter it dug, nil before
	deep                     *spot                   // where its stairs got to, the way they go
	upgraded                 map[string]time.Time    // a better tool last tried (and failed) at
	climbing                 bool                    // digging its way out (goto: not again inside)
	search                   searchState             // its search for what is not in sight (explore)
	legging                  bool                    // walking a far goal in legs (goto: the legs walk plainly)
	field                    *world.BlockPos         // its wheat field's water (farm), nil before
	onBridge                 bool                    // crossing a cave on its bridge: shift held, fights where it stands
	keepOut                  *[2]world.BlockPos      // a box not dug in for what it needs (a plot being levelled), nil for none
	atlas                    atlas                   // what it has seen round home, simplified (mapmem.go)
	waiting                  atomic.Bool             // waiting by choice (furnaces at work, hidden): its stillness is no stuck
	hiding                   bool                    // walled in for the night (hide): not again inside
	healing                  bool                    // resting to heal (defend): not again inside
	restedAt                 time.Time               // when it last rested to heal (defend)
	foodSought               time.Time               // when it last looked for food round it (findFood)
	bucketWater              *world.BlockPos         // still water seen under ground (no water on the land): buckets for the field
	avoid                    map[world.BlockPos]bool // cells not to stand on (reach): the field's pool and channel, while dug
	wild                     *world.BlockPos         // a field round water it found, sown before it had a bucket
	water                    *world.BlockPos         // still water it found (cmdWater): the buckets' water
	bounds                   *bounds                 // where the work at hand may take it (inBounds); nil: anywhere
	retreatsSince            time.Time               // retreats counted since (a mob that keeps up: walled in)
	retreats                 int
	putBackSince             time.Time // the server putting it back: since when counted (relog)
	putBack                  int
	diggingOut               bool      // tunnelToward: stone dug by hand when no pickaxe is left
	newPick                  bool      // making a new pickaxe (clear): not again within it
	defending                int       // defend within defend (its own walks): how deep
	streamFirst, streamQuiet time.Time // the stream reflex: since when counted, quiet till
	streamN                  int
	deathAt                  *world.BlockPos // where it last died, its things lying there (recover)
	deathTime                time.Time
	stairs                   []world.BlockPos // its staircase from home down, step by step (where the feet were)
	onStairs                 map[world.BlockPos]bool
	current                  string  // the command it is on (events)
	phases                   []phase // the times of day it has seen, as they came
	ctx                      context.Context
	food                     atomic.Int32
	health                   atomic.Uint32       // float32 bits, the last health the server sent
	lastHurt                 atomic.Int64        // when it was last hurt, Unix milliseconds
	leftAlone                map[int32]time.Time // mobs a defence could not reach, till when they are left (defend)
	wardenAt                 time.Time           // when it last saw a warden (no mine that night)
	fleeing                  bool                // going away from a warden
}

func main() {
	flag.Parse()
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	r := &robot{client: bot.NewClient(), orders: make(chan order, 4)}
	serverTags = &r.client.Tags // logs, ores, crops: as the server's data pack has them (data.go)
	r.client.Auth = bot.Auth{Name: *name}
	r.food.Store(20)
	r.health.Store(math.Float32bits(20))
	r.player = basic.NewPlayer(r.client, basic.DefaultSettings, basic.EventsListener{
		GameStart: func() error {
			log.Println("Game start")
			return nil
		},
		Disconnect: func(reason chat.Message) error { return bot.DisconnectErr(reason) },
		// the server putting it back (a move it would not take: a block the
		// robot's world lacks, a fall it did not believe): logged, for the
		// cases where the robot is stuck jumping at nothing (control accepts)
		Teleported: func(x, y, z float64, _, _ float32, flags int32, id int32) error {
			log.Printf("robot: the server moved it to %.2f %.2f %.2f (flags %d, teleport %d)", x, y, z, flags, id)
			// put back tick after tick: the server's world and the robot's
			// disagree (a block it thinks dug that is not). A player logs out
			// and in again: the robot ends, and is started again (relog)
			now := time.Now()
			if now.Sub(r.putBackSince) > 10*time.Second {
				r.putBackSince, r.putBack = now, 0
			}
			if r.putBack++; r.putBack > 40 {
				log.Printf("robot: relog: put back %d times in %s at %.2f %.2f %.2f", r.putBack, now.Sub(r.putBackSince).Round(time.Second), x, y, z)
				os.Exit(3)
			}
			return nil
		},
		HealthChange: func(health float32, food int32, _ float32) error {
			r.food.Store(food)
			// a hurt logged with where: what the world did to it
			if last := math.Float32frombits(r.health.Swap(math.Float32bits(health))); health < last {
				r.lastHurt.Store(time.Now().UnixMilli())
				p := r.player.Position()
				log.Printf("robot: hurt %.1f → %.1f at %.2f %.2f %.2f", last, health, p.X, p.Y, p.Z)
				events.emit("hurt", map[string]any{"from": last, "to": health, "x": round2(p.X), "y": round2(p.Y), "z": round2(p.Z)})
			}
			return nil
		},
		Death: func() error {
			log.Println("Died")
			r.deaths.Add(1) // the running order ends at its next step
			// where: its things lie there for five minutes (recover)
			p := r.player.Position()
			r.seenMu.Lock()
			r.deathAt = &world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
			r.deathTime = time.Now()
			r.seenMu.Unlock()
			events.emit("died", map[string]any{"x": round2(p.X), "y": round2(p.Y), "z": round2(p.Z)})
			// a person looks at the death screen for a moment, then clicks respawn
			time.AfterFunc(time.Second, func() {
				if err := r.player.Respawn(); err != nil {
					log.Print(err)
				}
			})
			return nil
		},
	})
	r.players = playerlist.New(r.client)
	r.chat = msg.New(r.client, r.player, r.players, msg.EventsHandler{
		SystemChat: func(c chat.Message, overlay bool) error {
			if !overlay {
				log.Printf("System: %v", c)
			}
			return nil
		},
		PlayerChatMessage: func(c chat.Message, _ bool) error {
			log.Printf("Player: %v", c)
			return nil
		},
		PlayerChat: r.onChat,
	})
	r.world = world.NewWorld(r.client, r.player, world.EventsListener{BlockChange: r.noteChange})
	r.screens = screen.NewManager(r.client, screen.EventsListener{})
	r.seen = map[string]int{}
	r.stuck = map[int32]bool{}
	r.entities = entities.New(r.client, entities.Events{
		Added: func(e entities.Entity) {
			r.seenMu.Lock()
			r.seen[e.Type]++
			r.seenMu.Unlock()
		},
		PickedUp: func(_, collector int32, amount int) {
			if collector == int32(r.player.Login.PlayerID) {
				r.pickedUp.Add(int32(amount))
			}
		},
	})
	r.clock = clock.New(r.client)
	r.splashes = make(chan splash, 8)
	r.client.Events.AddListener(r.soundListener())
	// its advancements, as the game shows them: one done is an event (its
	// badge), those done before it joined too, said so
	advancements.New(r.client, func(a advancements.Advancement, initial bool) {
		p := r.player.Position()
		log.Printf("robot: advancement %s (%s)%s", a.ID, a.Display.Title, map[bool]string{true: ", done before"}[initial])
		e := map[string]any{"id": a.ID, "title": a.Display.Title, "description": a.Display.Description, "frame": a.Display.Frame,
			"x": round2(p.X), "y": round2(p.Y), "z": round2(p.Z)}
		if initial {
			e["initial"] = true
		}
		events.emit("advancement", e)
	})
	r.ctl = control.New(r.client, r.player, r.world)
	r.ctl.Loaded = func() { log.Println("robot: loaded") }
	tags := physics.ClientTags{T: &r.client.Tags}
	mover := &physics.Mover{World: solids{r}, Tags: tags, Food: func() int { return int(r.food.Load()) }}
	r.walker = &path.Walker{Graph: &path.Graph{World: r.world, Tags: tags}}
	// round its field's pool and channels, not through them, nor across at
	// the surface (in the graph the cell over water is a swimmer's place)
	r.walker.Graph.Avoid = func(p world.BlockPos) bool {
		return r.fieldWaterAt(p) || r.fieldWaterAt(world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z})
	}
	r.ctl.Think = r.walker.Think
	r.hands = &act.Hands{C: r.client, World: r.world, Screens: r.screens}
	r.ctl.Act = r.hands.Act
	r.book = recipes.New(r.client)
	r.ui = &act.Screens{C: r.client, Ctl: r.ctl, Screens: r.screens, Book: r.book}
	r.signs = act.NewSigns(r.client)
	r.ctl.Step = mover.Step
	r.rider = ride.New(r.client, r.entities, r.world)
	r.ctl.Ride = r.rider.Ride
	if os.Getenv("ROBOT_TRACE") != "" {
		mover.Trace = func(p *physics.Player) {
			log.Printf("trace pos=%.4f,%.4f,%.4f vel=%.4f,%.4f,%.4f ground=%v crouch=%v fall=%.3f water=%v", p.Pos.X, p.Pos.Y, p.Pos.Z, p.Vel.X, p.Vel.Y, p.Vel.Z, p.OnGround, p.Crouching, p.FallDistance, p.InWater)
		}
	}

	if err := r.client.JoinServer(*address); err != nil {
		log.Fatal(err)
	}
	log.Println("Login success")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.ctx = ctx
	go func() {
		if err := r.ctl.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("control: %v", err)
		}
	}()
	if *eventsTo != "" {
		l, err := openEvents(*eventsTo, "mc:"+*name)
		if err != nil {
			log.Fatal(err)
		}
		events = l
		// the first line of a run: which format the rest is (EVENTS.md)
		events.emit("start", map[string]any{"schema": eventsSchema, "robot": *name, "server": *address})
	}
	if *memoryAt != "" {
		if err := r.loadMemory(*memoryAt); err != nil {
			log.Printf("robot: memory: %v", err)
		}
		go r.keepMemory(*memoryAt)
	}
	go r.obey()
	go r.report()
	go r.reportChanges()
	go r.keepLearning()
	go r.watchClock()
	go dumpOnSignal()
	go r.console(os.Stdin, cancel)

	for {
		err := r.client.HandleGame()
		var handlerErr bot.PacketHandlerError
		var disconnect bot.DisconnectErr
		switch {
		case errors.As(err, &disconnect):
			log.Printf("Disconnect, reason: %v", chat.Message(disconnect))
			return
		case errors.As(err, &handlerErr):
			log.Print(handlerErr) // a packet the robot could not handle; play on
		case ctx.Err() != nil:
			return
		default:
			log.Fatal(err)
		}
	}
}

// solids is the world the robot's physics moves in: the blocks, and the
// entities a player collides with — it stands on a boat it got out of.
type solids struct{ r *robot }

func (s solids) BlockAt(p world.BlockPos) (block.StateID, bool) { return s.r.world.BlockAt(p) }

func (s solids) EntityBoxes(area physics.AABB) []physics.AABB {
	feet := s.r.player.Position().Y
	cx, cy, cz := (area.MinX+area.MaxX)/2, (area.MinY+area.MaxY)/2, (area.MinZ+area.MaxZ)/2
	var out []physics.AABB
	for _, e := range s.r.entities.Nearby(cx, cy, cz, 8) {
		solid := strings.HasSuffix(e.Type, "_boat") || strings.HasSuffix(e.Type, "_raft") || e.Type == "minecraft:shulker"
		t := entity.ByName[e.Type]
		if t == nil {
			continue
		}
		if e.Type == "minecraft:happy_ghast" && feet >= e.Y+t.Height-1e-7 {
			solid = true // a platform from above (an adult's: a baby is smaller)
		}
		if !solid {
			continue
		}
		w := t.Width / 2
		out = append(out, physics.AABB{MinX: e.X - w, MinY: e.Y, MinZ: e.Z - w, MaxX: e.X + w, MaxY: e.Y + t.Height, MaxZ: e.Z + w})
	}
	return out
}

// dumpOnSignal logs every goroutine's stack on SIGUSR1: what the robot is
// doing when a command takes too long (a test sends it).
func dumpOnSignal() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGUSR1)
	for range c {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		log.Printf("robot: goroutines\n%s", buf[:n])
	}
}

// bounds is a circle the work at hand keeps within.
type bounds struct {
	at world.BlockPos
	r  float64
}
