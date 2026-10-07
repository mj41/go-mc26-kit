package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/types"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot/clock"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entity"
	"github.com/mj41/go-mc26/level/block"
)

// The robot's events: an append-only file of JSON lines (-events), for
// whatever wants to follow what the robot does — a viewer draws it in a
// browser — without the robot knowing who reads it. Each line is one event in
// a common envelope (id, time, source, kind, name, data), so a bridge can
// feed the file into an event hub as it is. EVENTS.md is the format, the contract with its readers:
//
//	{"id": time-sortable, "ts": RFC 3339, "source": "mc:<name>",
//	 "kind": "telemetry" | "event", "name": …, "data": {…}}
//
// The names:
//
//	state  (telemetry, twice a second) where it is and looks, health, food, the
//	       time of day, what it holds and carries, what it is doing
//	view   (telemetry, every two seconds) the world as it sees it round it,
//	       as map colours (RGB; 0 for open, "unknown" for not loaded) —
//	       from above at its height ("top"), the land from above ("surface",
//	       the highest block in each column, "height" over or under it), two
//	       slices down through it, west to east ("sliceX") and north to south
//	       ("sliceZ") — a legend (each
//	       colour shown, the commonest block of it) and the entities near
//	blocks (telemetry, with a view when they changed) every block state of
//	       a box round it, 49 by 49 and 32 high: a palette of the states
//	       (name and properties, map colour, outline boxes, fluid) and a cell
//	       of each block, its index in the palette — for a reader to build
//	       the world in 3D
//	cmd    (event) a command done: the line, the answer or the error, how long
//	plan   (event) a line of its plans
//	hurt, ate, phase   (event) as they happen
//	still  (event) it has not moved a block for 30 seconds (again at each
//	       further 30); the state carries "still_s" all along

// eventsSchema is the version of the event format (EVENTS.md): one more
// when a name or a field changes its meaning or goes; new names and fields
// are added without it.
const eventsSchema = 1

// eventLog appends events to a file; a nil one drops them.
type eventLog struct {
	mu     sync.Mutex
	f      *os.File
	enc    *json.Encoder
	source string
}

// events is the robot's event log, nil without -events.
var events *eventLog

func openEvents(path, source string) (*eventLog, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &eventLog{f: f, enc: json.NewEncoder(f), source: source}, nil
}

// telemetry are the names that are measurements, not happenings.
var telemetry = map[string]bool{"state": true, "view": true, "blocks": true}

// emit appends an event named name with data.
func (l *eventLog) emit(name string, data map[string]any) {
	if l == nil {
		return
	}
	now := time.Now()
	b := make([]byte, 4)
	rand.Read(b)
	kind := "event"
	if telemetry[name] {
		kind = "telemetry"
	}
	ev := map[string]any{
		"id": fmt.Sprintf("%016x%s", now.UnixNano(), hex.EncodeToString(b)), "ts": now.Format(time.RFC3339Nano),
		"source": l.source, "kind": kind, "name": name, "data": data,
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.enc.Encode(ev); err != nil {
		log.Printf("robot: events: %v", err)
	}
}

// report writes the robot's state twice a second and its view every two
// seconds, while it runs — a watcher sees it move, not jump.
func (r *robot) report() {
	if events == nil {
		return
	}
	tick := 0
	// still: it has not gone a block from where it was since then — a person
	// playing is on the move, day and night; a robot standing is stuck or idle
	var at struct{ x, y, z float64 }
	since, told := time.Now(), time.Duration(0)
	var lastBlocks []byte // the blocks told last: told again only when changed
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(time.Second / time.Duration(stateHz())):
		}
		if !r.ctl.State().Loaded {
			continue
		}
		p := r.player.Position()
		// moved a block — or waiting by choice (furnaces at work, hidden
		// for the night): not stuck
		if math.Abs(p.X-at.x)+math.Abs(p.Y-at.y)+math.Abs(p.Z-at.z) >= 1 || r.waiting.Load() {
			at.x, at.y, at.z = p.X, p.Y, p.Z
			since, told = time.Now(), 0
		}
		still := time.Since(since).Truncate(time.Second)
		if still >= told+30*time.Second {
			told = still
			log.Printf("robot: still %.0fs at %.1f %.1f %.1f, doing %q", still.Seconds(), p.X, p.Y, p.Z, r.doing())
			events.emit("still", map[string]any{"s": still.Seconds(), "doing": r.doing(), "x": round2(p.X), "y": round2(p.Y), "z": round2(p.Z)})
		}
		st := r.stateEvent()
		st["still_s"] = still.Seconds()
		events.emit("state", st)
		if tick%(2*stateHz()) == 0 { // a view (and blocks) every two seconds, whatever the state's rate
			events.emit("view", r.viewEvent())
			x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
			var b map[string]any
			if b, lastBlocks = r.blocksEvent(x, y, z, lastBlocks); b != nil {
				events.emit("blocks", b)
			}
		}
		tick++
	}
}

func (r *robot) stateEvent() map[string]any {
	p := r.player.Position()
	st := r.player.Status()
	e := map[string]any{
		"x": round2(p.X), "y": round2(p.Y), "z": round2(p.Z), "yaw": round2(float64(p.Yaw)), "pitch": round2(float64(p.Pitch)),
		"health": st.Health, "food": r.food.Load(), "dim": string(r.player.Spawn.Dimension),
		"doing": r.doing(),
	}
	if t, ok := r.clock.TimeOfDay(); ok {
		total, _ := r.clock.DayTicks()
		e["day"], e["tick"], e["phase"] = total/clock.DayLength+1, t, clock.Phase(t)
	}
	// the block its hands are on, for a watcher to outline
	if pos, what, ok := r.hands.Target(); ok {
		t := map[string]any{"x": pos.X, "y": pos.Y, "z": pos.Z, "what": what}
		if p, digging := r.hands.DigProgress(); digging && what == "dig" {
			t["progress"] = math.Round(float64(p)*100) / 100 // the cracks a reader draws
		}
		e["target"] = t
	}
	if r.hands.Swinging() {
		e["swinging"] = true
	}
	// its eyes' height: it stands or sneaks (it does not swim or crawl)
	e["eye"] = 1.62
	if r.ctl.State().Input.Shift {
		e["eye"] = 1.27
	}
	// the biome at its feet, the light at its eyes
	feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))}
	if b, ok := r.world.BiomeAt(feet); ok {
		e["biome"] = b
	}
	eye := feet
	eye.Y = int(math.Floor(p.Y + e["eye"].(float64)))
	if sky, block, ok := r.world.LightAt(eye); ok {
		e["light"] = map[string]int{"sky": sky, "block": block}
	}
	held := r.heldStack()
	if held.Count > 0 {
		e["held"] = itemName(int32(held.Item))
	}
	inv := map[string]int{}
	armor := map[string]string{}
	r.screens.Lock()
	slots := r.screens.Inventory.Slots
	for _, s := range slots[9:45] {
		if s.Count > 0 {
			inv[strings.TrimPrefix(itemName(int32(s.Item)), "minecraft:")] += int(s.Count)
		}
	}
	// what it wears (the inventory menu's slots 5–8) and holds in its other hand (45)
	for i, part := range []string{"head", "chest", "legs", "feet"} {
		if s := slots[5+i]; s.Count > 0 {
			armor[part] = itemName(int32(s.Item))
		}
	}
	if s := slots[45]; s.Count > 0 {
		e["offhand"] = itemName(int32(s.Item))
	}
	r.screens.Unlock()
	e["inv"] = inv
	if len(armor) > 0 {
		e["armor"] = armor
	}
	return e
}

// viewSize is how far the view reaches each way from the robot.
const viewSize = 32

// unknown is the colour of a block not known (not loaded): beyond RGB, so no
// map colour is it.
const unknown = 1 << 24

func (r *robot) viewEvent() map[string]any {
	p := r.player.Position()
	x, y, z := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	// each colour shown, and the blocks behind it, for the legend
	seen := map[uint32]map[string]int{}
	color := func(pos world.BlockPos) uint32 {
		s, ok := r.world.BlockAt(pos)
		if !ok {
			return unknown
		}
		c := block.MapColor(s)
		if c != 0 && int(s) < len(block.StateList) {
			if seen[c] == nil {
				seen[c] = map[string]int{}
			}
			seen[c][strings.TrimPrefix(block.StateList[s].ID(), "minecraft:")]++
		}
		return c
	}
	// from above at its height: the first block down from its head, a few
	// blocks deep at most (a cave's floor, the ground; under a roof, the
	// room it is in)
	n := 2*viewSize + 1
	top := make([]uint32, n*n)
	depth := make([]int8, n*n)
	for dz := -viewSize; dz <= viewSize; dz++ {
		for dx := -viewSize; dx <= viewSize; dx++ {
			i := (dz+viewSize)*n + dx + viewSize
			for dy := 1; dy >= -8; dy-- {
				if c := color(world.BlockPos{X: x + dx, Y: y + dy, Z: z + dz}); c != 0 {
					top[i], depth[i] = c, int8(dy)
					break
				}
			}
		}
	}
	// the land from above: the highest block in each column, down from the
	// world's top, and how far over or under the robot it is — where it is
	// underground, the land far over its head
	const worldTop = 320
	surface := make([]uint32, n*n)
	height := make([]int16, n*n)
	for dz := -viewSize; dz <= viewSize; dz++ {
		for dx := -viewSize; dx <= viewSize; dx++ {
			i := (dz+viewSize)*n + dx + viewSize
			for h := worldTop - 1; h >= y-48; h-- {
				if c := color(world.BlockPos{X: x + dx, Y: h, Z: z + dz}); c != 0 {
					surface[i], height[i] = c, int16(h-y)
					break
				}
			}
		}
	}
	// two slices down through it, the same way whichever way it looks: west
	// to east ("sliceX") and north to south ("sliceZ"), 16 up and 24 down
	const up, down = 16, 24
	sliceX := make([]uint32, n*(up+down+1))
	sliceZ := make([]uint32, n*(up+down+1))
	for row := 0; row <= up+down; row++ {
		for i := -viewSize; i <= viewSize; i++ {
			sliceX[row*n+i+viewSize] = color(world.BlockPos{X: x + i, Y: y + up - row, Z: z})
			sliceZ[row*n+i+viewSize] = color(world.BlockPos{X: x, Y: y + up - row, Z: z + i})
		}
	}
	d := r.facing()
	// the legend: each colour by how much of it there is, named by its
	// commonest block ("diorite" for the white in the rock)
	type key struct {
		c     uint32
		name  string
		count int
	}
	var keys []key
	for c, names := range seen {
		k := key{c: c}
		for n, v := range names {
			k.count += v
			if v > names[k.name] || v == names[k.name] && n < k.name {
				k.name = n
			}
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].count > keys[j].count })
	legend := []map[string]any{}
	for _, k := range keys {
		legend = append(legend, map[string]any{"c": k.c, "name": k.name, "n": k.count})
	}
	var near []map[string]any
	for _, e := range r.entities.Nearby(p.X, p.Y, p.Z, viewSize) {
		en := map[string]any{"id": e.ID, "type": strings.TrimPrefix(e.Type, "minecraft:"), "x": round2(e.X), "y": round2(e.Y), "z": round2(e.Z), "yaw": round2(float64(e.Yaw))}
		if t, ok := entity.ByName[e.Type]; ok { // the type's box (an adult's)
			en["w"], en["h"] = t.Width, t.Height
		}
		r.entityLooks(e, en)
		near = append(near, en)
	}
	sort.Slice(near, func(i, j int) bool { return near[i]["type"].(string) < near[j]["type"].(string) })
	return map[string]any{
		"x": x, "y": y, "z": z, "size": viewSize, "facing": d, "unknown": unknown,
		"top": top, "depth": depth, "surface": surface, "height": height,
		"sliceX": sliceX, "sliceZ": sliceZ, "up": up, "down": down,
		"legend": legend, "entities": near,
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// doing is the command the robot is on, "" when idle.
func (r *robot) doing() string {
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	return r.current
}

// running notes the command the robot is on for the events, and how it ended.
func (r *robot) running(line string) func(answer string, err error) {
	r.seenMu.Lock()
	r.current = line
	r.seenMu.Unlock()
	start := time.Now()
	return func(answer string, err error) {
		r.seenMu.Lock()
		r.current = ""
		r.seenMu.Unlock()
		e := map[string]any{"line": line, "ms": time.Since(start).Milliseconds()}
		if err != nil {
			e["error"] = err.Error()
		} else {
			e["answer"] = answer
		}
		events.emit("cmd", e)
	}
}

// stateHz is how many state events a second (-events-hz, 2 to 20).
func stateHz() int {
	return min(20, max(2, *eventsHz))
}

// equipmentSlots are the names of EquipmentSlot's ordinals, as set_equipment
// numbers them.
var equipmentSlots = []string{"mainhand", "offhand", "feet", "legs", "chest", "head", "body", "saddle"}

// dyeColors are DyeColor's names by id (a sheep's wool).
var dyeColors = []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
	"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}

// entityLooks adds to a view's entity what a watcher needs to draw it as the
// game does: what it holds and wears (set_equipment), whether it is a baby
// (its type's DATA_BABY_ID), a sheep's wool colour and whether it is shorn.
func (r *robot) entityLooks(e entities.Entity, en map[string]any) {
	r.entityVariant(e, en)
	eq := map[string]string{}
	for slot, st := range e.Equipment {
		if st.Count > 0 && slot >= 0 && slot < len(equipmentSlots) {
			eq[equipmentSlots[slot]] = itemName(int32(st.Item))
		}
	}
	if len(eq) > 0 {
		en["equipment"] = eq
	}
	for _, f := range entitydata.Fields[e.Type] {
		if f.Name != "DATA_BABY_ID" {
			continue
		}
		if v, ok := e.Data[uint8(f.Index)]; ok {
			if b, ok := v.Value.(*pk.Boolean); ok && bool(*b) {
				en["baby"] = true
			}
		}
	}
	if e.Type == "minecraft:item" { // a dropped item: which, how many
		if v, ok := e.Data[entitydata.ItemEntityItem]; ok {
			if st, ok := v.Value.(*types.ItemStack); ok && st.Count > 0 {
				en["item"], en["count"] = itemName(int32(st.Item)), int(st.Count)
			}
		}
	}
	if e.Type == "minecraft:sheep" {
		if v, ok := e.Data[entitydata.SheepWool]; ok {
			if b, ok := v.Value.(*pk.Byte); ok {
				en["color"] = dyeColors[int(*b)&15]
				if int(*b)&16 != 0 {
					en["sheared"] = true
				}
			}
		}
	}
}

// The variants told as numbers, by type (their Java enums' order).
var intVariants = map[string]struct {
	index uint8
	names []string
}{
	"minecraft:llama":        {entitydata.LlamaVariant, []string{"creamy", "white", "brown", "gray"}},
	"minecraft:trader_llama": {entitydata.LlamaVariant, []string{"creamy", "white", "brown", "gray"}},
	"minecraft:parrot":       {entitydata.ParrotVariant, []string{"red_blue", "blue", "green", "yellow_blue", "gray"}},
	"minecraft:axolotl":      {entitydata.AxolotlVariant, []string{"lucy", "wild", "gold", "cyan", "blue"}},
	"minecraft:fox":          {entitydata.FoxType, []string{"red", "snow"}},
	"minecraft:mooshroom":    {entitydata.MushroomCowType, []string{"red", "brown"}},
	"minecraft:salmon":       {entitydata.SalmonType, []string{"small", "medium", "large"}},
}

var rabbitTypes = map[int]string{0: "brown", 1: "white", 2: "black", 3: "white_splotched", 4: "gold", 5: "salt", 99: "evil"}

var horseColors = []string{"white", "creamy", "chestnut", "brown", "black", "gray", "dark_brown"}
var horseMarkings = []string{"none", "white", "white_field", "white_dots", "black_dots"}

// entityVariant adds what kind of its type an entity is: a cat's coat, a
// llama's, a horse's colour and markings, a frog's, a villager's land and
// trade (variant, as the registry or enum names it, no namespace); a salmon's
// size; a falling block's block; an arrow's pitch; a chest on a llama, a
// donkey, a mule.
func (r *robot) entityVariant(e entities.Entity, en map[string]any) {
	num := func(i uint8) (int, bool) {
		v, ok := e.Data[i]
		if !ok {
			return 0, false
		}
		switch x := v.Value.(type) {
		case *pk.VarInt:
			return int(*x), true
		case *pk.Int:
			return int(*x), true
		}
		return 0, false
	}
	short := func(k string, ok bool) (string, bool) { return strings.TrimPrefix(k, "minecraft:"), ok }
	reg := r.client.Registries
	var name string
	var ok bool
	switch e.Type {
	case "minecraft:cat":
		if id, has := num(entitydata.CatVariant); has {
			name, ok = short(reg.CatVariant.KeyOf(int32(id)))
		}
	case "minecraft:wolf":
		if id, has := num(entitydata.WolfVariant); has {
			name, ok = short(reg.WolfVariant.KeyOf(int32(id)))
		}
	case "minecraft:frog":
		if id, has := num(entitydata.FrogVariant); has {
			name, ok = short(reg.FrogVariant.KeyOf(int32(id)))
		}
	case "minecraft:pig":
		if id, has := num(entitydata.PigVariant); has {
			name, ok = short(reg.PigVariant.KeyOf(int32(id)))
		}
	case "minecraft:cow":
		if id, has := num(entitydata.CowVariant); has {
			name, ok = short(reg.CowVariant.KeyOf(int32(id)))
		}
	case "minecraft:chicken":
		if id, has := num(entitydata.ChickenVariant); has {
			name, ok = short(reg.ChickenVariant.KeyOf(int32(id)))
		}
	case "minecraft:rabbit":
		if id, has := num(entitydata.RabbitType); has {
			name, ok = rabbitTypes[id]
		}
	case "minecraft:horse":
		if v, has := num(entitydata.HorseTypeVariant); has && v&0xff < len(horseColors) && v>>8 < len(horseMarkings) {
			name, ok = horseColors[v&0xff]+"/"+horseMarkings[v>>8], true
		}
	case "minecraft:villager", "minecraft:zombie_villager":
		i := uint8(entitydata.VillagerVillagerData)
		if e.Type == "minecraft:zombie_villager" {
			i = entitydata.ZombieVillagerVillagerData
		}
		if v, has := e.Data[i]; has {
			if d, isData := v.Value.(*types.VillagerData); isData {
				if int(d.Type) < len(registryid.VillagerType) {
					name, ok = strings.TrimPrefix(registryid.VillagerType[d.Type], "minecraft:"), true
				}
				if int(d.Profession) < len(registryid.VillagerProfession) {
					en["profession"] = strings.TrimPrefix(registryid.VillagerProfession[d.Profession], "minecraft:")
				}
			}
		} else {
			name, ok = "plains", true // the default, not sent
		}
	case "minecraft:falling_block":
		if s := block.StateID(e.SpawnData); int(s) >= 0 && int(s) < len(block.StateList) {
			en["block"] = world.StateString(s)
		}
	case "minecraft:arrow", "minecraft:spectral_arrow", "minecraft:trident", "minecraft:squid", "minecraft:glow_squid":
		en["pitch"] = round2(float64(e.Pitch))
	}
	if iv, has := intVariants[e.Type]; has {
		if v, has := num(iv.index); has && v >= 0 && v < len(iv.names) {
			if e.Type == "minecraft:salmon" {
				en["size"] = iv.names[v]
			} else {
				name, ok = iv.names[v], true
			}
		}
	}
	if ok && name != "" {
		en["variant"] = name
	}
	switch e.Type {
	case "minecraft:llama", "minecraft:trader_llama", "minecraft:donkey", "minecraft:mule":
		if v, has := e.Data[entitydata.AbstractChestedHorseChest]; has {
			if b, isBool := v.Value.(*pk.Boolean); isBool && bool(*b) {
				en["chest"] = true
			}
		}
	}
}
