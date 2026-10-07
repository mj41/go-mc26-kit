package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// The robot's memory (-memory <file>): what it knows of the world it made
// its own — its shelter, its stairs, how deep they got, its field, its
// search — kept in a file, so a robot started again (a new build, after a
// crash) goes on where the last left off, in the same world, as a person
// who logs in again remembers where their home is. The server keeps the rest:
// where it stands, what it carries.

type memory struct {
	Home   *memRoom         `json:"home,omitempty"`
	Deep   *memSpot         `json:"deep,omitempty"`
	Stairs []world.BlockPos `json:"stairs,omitempty"`
	Field  *world.BlockPos  `json:"field,omitempty"`
	Wild   *world.BlockPos  `json:"wild,omitempty"`  // a field round water found (no bucket yet)
	Water  *world.BlockPos  `json:"water,omitempty"` // still water found, for buckets
	Under  *world.BlockPos  `json:"under,omitempty"` // still water under ground, for buckets
	Search *memSearch       `json:"search,omitempty"`
	Map    []mappedArea     `json:"map,omitempty"` // what it saw round home (mapmem.go)
}

type memRoom struct {
	Front world.BlockPos `json:"front"`
	D     [2]int         `json:"d"`
	Y     int            `json:"y"`
}

type memSpot struct {
	At world.BlockPos `json:"at"`
	D  [2]int         `json:"d"`
}

type memSearch struct {
	Center world.BlockPos `json:"center"`
	Ring   int            `json:"ring"`
	Point  int            `json:"point"`
}

// recall is what it knows now, as memory.
func (r *robot) recall() memory {
	var m memory
	if r.home != nil {
		m.Home = &memRoom{Front: r.home.front, D: r.home.d, Y: r.home.y}
	}
	if r.deep != nil {
		m.Deep = &memSpot{At: r.deep.at, D: r.deep.d}
	}
	m.Stairs = append([]world.BlockPos(nil), r.stairs...)
	m.Field = r.field
	m.Wild, m.Water, m.Under = r.wild, r.water, r.bucketWater
	if r.search.on {
		m.Search = &memSearch{Center: r.search.center, Ring: r.search.ring, Point: r.search.point}
	}
	m.Map = r.atlas.all()
	return m
}

// loadMemory reads its memory from path, if there is one there.
func (r *robot) loadMemory(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var m memory
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if m.Home != nil {
		r.home = &room{front: m.Home.Front, d: m.Home.D, y: m.Home.Y}
	}
	if m.Deep != nil {
		r.deep = &spot{at: m.Deep.At, d: m.Deep.D}
	}
	for _, s := range m.Stairs {
		r.addStair(s)
	}
	r.field = m.Field
	r.wild, r.water, r.bucketWater = m.Wild, m.Water, m.Under
	if m.Search != nil && m.Search.Center != (world.BlockPos{}) { // none: an older memory's
		r.search = searchState{on: true, center: m.Search.Center, ring: m.Search.Ring, point: m.Search.Point}
	}
	for _, a := range m.Map {
		r.atlas.put(areaKey{a.X, a.Z}, a.area)
	}
	log.Printf("robot: remembered home %v, %d stairs, field %v", m.Home != nil, len(m.Stairs), m.Field != nil)
	return nil
}

// keepMemory writes its memory to path every ten seconds when it changed,
// whole (a new file renamed over the old), while it runs, and once more as it
// stops.
func (r *robot) keepMemory(path string) {
	var last []byte
	write := func() {
		b, err := json.MarshalIndent(r.recall(), "", " ")
		if err != nil || string(b) == string(last) {
			return
		}
		tmp := path + ".new"
		if err := os.WriteFile(tmp, b, 0o644); err != nil {
			log.Printf("robot: memory: %v", err)
			return
		}
		if err := os.Rename(tmp, path); err != nil {
			log.Printf("robot: memory: %v", err)
			return
		}
		last = b
	}
	// stopped from outside (a run held, a delivery): its memory written
	// first, then terminated as it would have been
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	for {
		select {
		case <-r.ctx.Done():
			write() // ended: what changed in its last seconds too
			return
		case sig := <-stop:
			write()
			signal.Reset(sig)
			_ = syscall.Kill(os.Getpid(), sig.(syscall.Signal))
			return
		case <-time.After(10 * time.Second):
		}
		write()
	}
}
