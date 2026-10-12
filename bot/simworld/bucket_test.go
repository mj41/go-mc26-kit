package simworld

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/act"
)

// useToward is the bot's use of what it holds, aimed at the point, waited for.
func useToward(t *testing.T, g *Game, x, y, z float64) {
	t.Helper()
	ch := make(chan act.Use, 1)
	g.B.Hands.UseItemToward(x, y, z, func(u act.Use) { ch <- u })
	if !g.Until(200, func() bool { return len(ch) > 0 }) {
		t.Fatal("the use was not answered")
	}
	g.Step(2)
}

// A bucket takes up the water it is aimed at: from a pond's middle the water
// comes again (two sources beside it), a lone source is gone; the water bucket
// pours a source against the block it is aimed at.
func TestBucket(t *testing.T) {
	Run(t, func(t *testing.T) {
		g := Start(t, Options{View: 2, Setup: []string{"fill 2 63 -1 4 63 1 minecraft:water", "setblock -2 63 0 minecraft:water"}})
		g.Give("minecraft:bucket", 2)
		g.Step(2)
		useToward(t, g, 3.5, 63.9, 0.5) // the pond's middle
		if g.Count("minecraft:water_bucket") != 1 || g.Count("minecraft:bucket") != 1 {
			t.Fatalf("a bucket into the pond: %d water buckets, %d buckets", g.Count("minecraft:water_bucket"), g.Count("minecraft:bucket"))
		}
		if got := blockAt(g, 3, 63, 0); got != "minecraft:water" {
			t.Errorf("the pond's middle after a bucket: %s, want water again", got)
		}
		useToward(t, g, -1.5, 63.9, 0.5) // the lone source
		if got := blockAt(g, -2, 63, 0); got != "minecraft:air" {
			t.Errorf("a lone source after a bucket: %s, want it gone", got)
		}
		if g.Count("minecraft:water_bucket") != 2 {
			t.Fatalf("%d water buckets, want 2", g.Count("minecraft:water_bucket"))
		}
		useToward(t, g, 0.5, 63.9, -1.5) // the ground two off: poured onto it
		if got := blockAt(g, 0, 64, -2); got != "minecraft:water" {
			t.Errorf("a water bucket onto the ground: %s over it, want water", got)
		}
		if g.Count("minecraft:bucket") != 1 || g.Count("minecraft:water_bucket") != 1 {
			t.Errorf("after the pour: %d buckets, %d water buckets", g.Count("minecraft:bucket"), g.Count("minecraft:water_bucket"))
		}
	})
}
