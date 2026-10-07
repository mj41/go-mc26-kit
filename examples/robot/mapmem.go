package main

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// The robot's map: what it has seen round home, simplified as a person
// remembers a place — where the woods are, the water, lava, grass for
// seeds, sand — in areas of sixteen by sixteen blocks (a chunk's columns),
// each with what lies on its surface and how high the ground is. It learns
// as it goes (what is loaded round it, every half minute), keeps it in its
// memory (memory.go), and goes to the nearest area that has what it needs
// when none is in sight, before it searches blind; scouting by day (scout)
// fills it in round home.

// area is one map square: what its surface has, how high, when last seen.
type area struct {
	Logs    int   `json:"logs,omitempty"`
	Water   int   `json:"water,omitempty"` // still water at the surface
	Lava    int   `json:"lava,omitempty"`
	Grass   int   `json:"grass,omitempty"` // short and tall grass, ferns: seeds
	Sand    int   `json:"sand,omitempty"`
	Village int   `json:"village,omitempty"` // a bell, villagers seen
	Y       int   `json:"y"`                 // the ground's height, the middle of what was seen
	SeenAt  int64 `json:"seen"`              // Unix seconds
	// Cold: its ground in the cold (a snowy biome, a mountain's height):
	// water there freezes, snow falls — no field, no home
	Cold bool `json:"cold,omitempty"`
}

// areaKey is an area's place: its x and z divided by sixteen (rounded down).
type areaKey struct{ X, Z int }

func keyOf(x, z int) areaKey { return areaKey{x >> 4, z >> 4} }

// centre is the middle column of an area.
func (k areaKey) centre() (int, int) { return k.X*16 + 8, k.Z*16 + 8 }

// atlas is the robot's map.
type atlas struct {
	mu    sync.Mutex
	areas map[areaKey]*area
}

func (a *atlas) get(k areaKey) (area, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.areas[k]
	if !ok {
		return area{}, false
	}
	return *v, true
}

func (a *atlas) put(k areaKey, v area) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.areas == nil {
		a.areas = map[areaKey]*area{}
	}
	a.areas[k] = &v
}

// all is a copy of every area, sorted by place.
func (a *atlas) all() []mappedArea {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]mappedArea, 0, len(a.areas))
	for k, v := range a.areas {
		out = append(out, mappedArea{X: k.X, Z: k.Z, area: *v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		return out[i].Z < out[j].Z
	})
	return out
}

// mappedArea is an area with its place, as memory and the events keep it.
type mappedArea struct {
	X int `json:"x"` // the area's x / 16
	Z int `json:"z"`
	area
}

// learnRadius is how far round it the robot maps, every half minute.
const learnRadius = 48

// learn maps the areas round it from what is loaded: each column's surface
// (the first block down from 32 over its feet), every other column across
// (enough to know an area by).
func (r *robot) learn() { r.learnRound(learnRadius, 2, 32, 32) }

// learnRound maps the areas within radius round it, every step-th column
// across, each column's surface looked for from up over its feet down to down
// under them: wide and deep, a look down into the valley from a mountain.
func (r *robot) learnRound(radius, step, up, down int) {
	p := r.player.Position()
	x0, y0, z0 := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	logs, leaves := tagBlocks("minecraft:logs"), tagBlocks("minecraft:leaves")
	type tally struct {
		a       area
		ys      []int
		temps   []float64
		columns int
	}
	seen := map[areaKey]*tally{}
	for dx := -radius; dx <= radius; dx += step {
		for dz := -radius; dz <= radius; dz += step {
			x, z := x0+dx, z0+dz
			k := keyOf(x, z)
			t := seen[k]
			if t == nil {
				t = &tally{}
				seen[k] = t
			}
			for y := y0 + up; y >= y0-down; y-- {
				s, ok := r.world.BlockAt(world.BlockPos{X: x, Y: y, Z: z})
				if !ok {
					break // not loaded: nothing learnt of this column
				}
				if block.IsAir(s) || int(s) >= len(block.StateList) {
					continue
				}
				id := block.StateList[s].ID()
				switch {
				case slices.Contains(logs, id):
					t.a.Logs++
					continue // the trunk: the ground is under it
				case id == "minecraft:short_grass" || id == "minecraft:tall_grass" || id == "minecraft:fern" || id == "minecraft:large_fern":
					t.a.Grass++
					continue
				case id == "minecraft:bell":
					t.a.Village++
					continue
				case len(block.CollisionShape(s)) == 0 && block.FluidOf(s) == nil:
					continue // leaves aside, flowers, snow: on down to the ground
				}
				if f := block.FluidOf(s); f != nil {
					if f.Name == "minecraft:water" && f.Source {
						t.a.Water++
					} else if f.Name == "minecraft:lava" || f.Name == "minecraft:flowing_lava" {
						t.a.Lava++
					}
				} else if id == "minecraft:sand" || id == "minecraft:red_sand" {
					t.a.Sand++
				}
				if !slices.Contains(leaves, id) { // under a tree's crown: on down
					t.ys = append(t.ys, y)
					if temp, ok := r.world.TemperatureAt(world.BlockPos{X: x, Y: y + 1, Z: z}); ok {
						t.temps = append(t.temps, float64(temp))
					}
					t.columns++
					break
				}
			}
		}
	}
	now := time.Now().Unix()
	// a coarser look (every 4th column, from afar) samples fewer columns:
	// its counts scaled to the every-other-column ones, and it never
	// overwrites an area seen the finer way
	scale := (step / 2) * (step / 2)
	for k, t := range seen {
		if t.columns == 0 {
			continue
		}
		if step > 2 {
			if _, known := r.atlas.get(k); known {
				continue
			}
			for _, c := range []*int{&t.a.Logs, &t.a.Water, &t.a.Lava, &t.a.Grass, &t.a.Sand, &t.a.Village} {
				*c *= scale
			}
		}
		sort.Ints(t.ys)
		t.a.Y, t.a.SeenAt = t.ys[len(t.ys)/2], now
		if len(t.temps) > 0 {
			sort.Float64s(t.temps)
			t.a.Cold = t.temps[len(t.temps)/2] < coldBelow
		}
		r.atlas.put(k, t.a)
	}
}

// coldBelow is the temperature under which water open to the sky freezes
// and snow falls (Biome.coldEnoughToSnow).
const coldBelow = 0.15

// coldAt reports whether the ground's cell p is in the cold: water poured
// there freezes, a field there has no water. Not known is not cold.
func (r *robot) coldAt(p world.BlockPos) bool {
	t, ok := r.world.TemperatureAt(p)
	return ok && t < coldBelow
}

// warmLand is the middle of the nearest area it knows (from p) whose ground
// is not in the cold and has grass — the green land below the snow — no
// higher than maxY, and whether there is one.
func (r *robot) warmLand(p world.BlockPos, maxY int) (world.BlockPos, bool) {
	best, bestD, found := world.BlockPos{}, math.MaxFloat64, false
	for _, m := range r.atlas.all() {
		if m.Cold || m.Grass == 0 || m.Y > maxY {
			continue
		}
		cx, cz := areaKey{m.X, m.Z}.centre()
		if r.coldAt(world.BlockPos{X: cx, Y: m.Y + 1, Z: cz}) {
			continue // its middle in the cold (the map's median warm)
		}
		if d := math.Hypot(float64(cx-p.X), float64(cz-p.Z)); d < bestD {
			best, bestD, found = world.BlockPos{X: cx, Y: m.Y + 1, Z: cz}, d, true
		}
	}
	return best, found
}

// keepLearning maps round it every half minute while it runs, and tells the
// events (map).
func (r *robot) keepLearning() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
		if !r.ctl.State().Loaded {
			continue
		}
		r.learn()
		r.noteBucketWater()
		r.markVillagers()
		events.emit("map", map[string]any{"areas": r.atlas.all()})
	}
}

// nearestKnown is the middle of the nearest area it knows to have what has
// says (logs, water…), and whether there is one.
func (r *robot) nearestKnown(has func(area) bool) (world.BlockPos, bool) {
	// nearest to home, not to where it is: from a far place the nearest is
	// further on yet, and it drifts away a trip at a time
	p := r.player.Position()
	if r.home != nil {
		p.X, p.Z = float64(r.home.front.X), float64(r.home.front.Z)
	}
	most := math.MaxFloat64
	if r.player.Status().Health < 10 {
		most = 48 // hurt: only near home
	}
	best, bestD, found := world.BlockPos{}, most, false
	for _, m := range r.atlas.all() {
		if !has(m.area) {
			continue
		}
		cx, cz := areaKey{m.X, m.Z}.centre()
		if d := math.Hypot(float64(cx)-p.X, float64(cz)-p.Z); d < bestD {
			best, bestD, found = world.BlockPos{X: cx, Y: m.Y + 1, Z: cz}, d, true
		}
	}
	return best, found
}

// goToKnown walks to the nearest area known to have what has says, to its
// ground. It reports whether it went.
func (r *robot) goToKnown(what string, has func(area) bool) bool {
	c, ok := r.nearestKnown(has)
	if !ok {
		return false
	}
	g, ok := r.groundAt(c.X, c.Z, c.Y)
	if !ok {
		g = c
	}
	plan("%s: to the area it knows has some, at %v", what, g)
	if _, err := cmdGoto(r, []string{strconv.Itoa(g.X), strconv.Itoa(g.Y), strconv.Itoa(g.Z)}); err != nil {
		plan("%s: %v", what, err)
		return false
	}
	return true
}

// areaHas says, for what the robot wants, which areas have it (nil: the map
// does not know it): the logs of a wood, still water for a bucket, grass for
// seeds, sand.
func areaHas(item string) func(area) bool {
	switch {
	case item == "logs" || slices.Contains(logItems(), item):
		return func(a area) bool { return a.Logs > 0 }
	case item == "minecraft:water_bucket":
		return func(a area) bool { return a.Water > 0 }
	case item == "minecraft:wheat_seeds":
		return func(a area) bool { return a.Grass > 0 }
	case item == "minecraft:sand":
		return func(a area) bool { return a.Sand > 0 }
	}
	return nil
}

// cmdScout walks round home, as a person gets to know a place: to eight
// places radius off (48) and back home, mapping as it goes. It answers what
// it knows: how many areas, and how many with wood, water, lava.
func cmdScout(r *robot, args []string) (string, error) {
	if h := r.player.Status().Health; h < 10 && r.countAll(foods) == 0 {
		return fmt.Sprintf("staying in: health %.1f and nothing to eat", h), nil // no walk far, hurt and not healing
	}
	radius := 48
	if len(args) == 1 {
		v, err := strconv.Atoi(args[0])
		if err != nil {
			return "", err
		}
		radius = v
	}
	p := r.player.Position()
	c := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	if r.home != nil {
		c = r.home.front
	}
	for k := 0; k < 8; k++ {
		if err := r.dayWork(); err != nil { // after dark: not out (in, it stays in)
			break
		}
		a := float64(k) * math.Pi / 4
		x, z := c.X+int(math.Round(math.Cos(a)*float64(radius))), c.Z+int(math.Round(math.Sin(a)*float64(radius)))
		g, ok := r.groundAt(x, z, c.Y)
		if !ok {
			continue
		}
		plan("scout: toward %s, to %v", compass(a), g)
		if _, err := cmdGoto(r, []string{strconv.Itoa(g.X), strconv.Itoa(g.Y), strconv.Itoa(g.Z)}); err != nil {
			plan("scout: %v", err)
		}
		r.learn()
		if r.night() {
			plan("scout: dusk: home")
			break
		}
	}
	if r.home != nil {
		if _, err := cmdGoto(r, []string{strconv.Itoa(c.X), strconv.Itoa(c.Y), strconv.Itoa(c.Z)}); err != nil {
			plan("scout: home: %v", err)
		}
	}
	n, wood, water, lava := 0, 0, 0, 0
	for _, m := range r.atlas.all() {
		n++
		if m.Logs > 0 {
			wood++
		}
		if m.Water > 0 {
			water++
		}
		if m.Lava > 0 {
			lava++
		}
	}
	return fmt.Sprintf("areas=%d wood=%d water=%d lava=%d", n, wood, water, lava), nil
}
