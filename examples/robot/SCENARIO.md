# The robot's first days

What the robot does in a new survival world, day by day, and the rules it keeps while doing it.
The course is the one a person new to a world follows: wood, a pickaxe, a shelter before dark,
down for ore at night, a field and more wood by day, an iron pickaxe and diamonds.

mc26's end-to-end scenario `days` drives the robot through this course on the game's own clock
(see "How it is tested" at the end). Each step is a robot command; the robot works out the
rest itself: recipes, tools, where to dig, how to get there.

## The world it is made for

- **The overworld, a normal biome:** trees within sight of the start, stone hills near, the
  usual caves below. A desert, an ocean, a bare mountain top or a mushroom island is not what
  the course expects. The robot still searches for what it needs (below), but the scenario
  assumes trees and stone are close.
- **A start in the cold** (a snowy mountain's slopes, a grove: under 0.15 of the game's
  temperature, less a little for every block over y 80) is no place to settle: water freezes
  there and snow falls. Before its shelter it walks down to the nearest green land below the
  snow it can see from up there (below).
- **Survival, normal difficulty:** mobs at night and in the dark, hunger, falls.
- **An empty inventory at the start.**

## Day 1: wood, tools, a shelter

| Step | Command | What happens |
|---|---|---|
| 1 | `get wooden_pickaxe` | Logs cut, planks, sticks, a crafting table placed and used. |
| 2 | `get logs 16` | Logs of any tree, the nearest kind each time. |
| 3 | `shelter` | A room dug into the nearest hill (below). Its stone is the first cobblestone. |
| 4 | `get stone_sword` | |
| 5 | `get stone_axe` | Logs cut faster from here on. |
| 6 | `get torch 16` | Coal it can walk to, else charcoal smelted from logs. |
| 7 | `get stick 8` | |
| 8 | `in` | Back into the shelter. The room's torches go up if they are not up yet. |

Then, until dusk, round after round: `out`, `get logs` (16 more each round), `in`. At dusk it
goes in and the way in is shut behind it.

It leaves the animals alone. It eats the apples that falling leaves drop, fish it catches, and later the field's
bread.

### The shelter

A room three by three and two high, dug into a hill that has solid rock all round it. The hill
is the nearest the robot can walk to, within 16 blocks, else within 32. It tries up to six
places before it builds a cobblestone hut on open ground instead.

The way in is shut with cobblestone behind it for the night and dug open in the morning,
until the end of the first night: coming up its stairs in the morning, it makes a wooden
door of the wood it carries and hangs it in the way in, put from inside looking out. From
then on `in` shuts the door, and `out` opens it, steps out and shuts it behind.

Standing in the cold when it is time for the shelter, it first looks down from where it is
(its map, 160 blocks round and 96 below) for the nearest land with grass that is not in the
cold and no higher than it stands, and walks down there; the hill is then the nearest one to
that place, the mountain's foot.

```
          ← into the hill
   tunnel  T T T          three blocks on, where the stairs down start
   room    * . F          F furnace
           . . C          C chest     * torch on the left wall
           * . W          W crafting table
   way in    .            shut with cobblestone behind the robot
          ← out
```

Furniture stands along the right wall going in. The torches are on the left wall at head
height. The furnace is made inside the room, from the stone the room gave.

In the afternoon, `water`: still water found and remembered (a pond, a river, a lake: a
source of water under the open sky with more of it beside, not a lone one in a cave). In sight,
or where its map has seen some, or along the search's rings round home, as far as the light
allows. The place is kept in its memory: it is the field's water. With a bucket (the mines'
iron) its two buckets for the pool come from there; before there is iron, the field is made
round that water itself.

## Night 1: down to the iron

| Step | Command | What happens |
|---|---|---|
| 1 | `get stone_pickaxe 4` | Spares: pickaxes wear out. |
| 2 | `descend 16` | Stairs down from the end of the shelter's tunnel to y 16. |
| 3 | `mine` | A mine at the bottom of the stairs (below). |

Then, until morning: on down the stairs about 12 blocks and another mine, each with longer
tunnels, down to the diamonds' level (y −54). Work begun at night stops when the morning comes,
and the robot goes up for the day's work.

### The stairs

- A step forward and down at a time, three blocks high so they can be climbed back.
- A torch on the right wall every six steps. Going back up, the torches are on the left.
- Always into the hill: the direction with the most rock overhead. A step that would come out
  under the sky turns back into the rock.
- The ores it passes are dug out.
- Water or lava next to a step turns the stairs another way. It never digs into a fluid.
  Where every way down is dangerous, the stairs end there if they are within 6 blocks of
  their level. Otherwise it goes back up four steps and leads the stairs on another way.
- A cave under the next step is crossed on a bridge of cobblestone, level, up to 256 blocks —
  under the rock only. Where the stairs come out of the hill into the open air (a hillside, a
  valley), there is no bridge: they turn back into the rock. On the land the robot never puts
  a block over the air: it walks round, digs its way, or builds a stair of blocks up.
  The bridge goes on five blocks into the rock past the cave before the stairs go down again.
  The robot sneaks all the way across, standing still and fighting too, so a mob's hit
  cannot push it off. On a bridge it fights where it stands: it does not follow a mob or run.
- It remembers every step. Nothing is ever put down on them, and it climbs back the way it
  came, clearing gravel or anything else that has fallen in.

### The mine

Seen from above, with the stairs coming up the page as the robot walks them down, so its
right is the drawing's right:

```
              ↑ the way the stairs go

   12   ·  · · · ·  M M  b b b b b …   the first branch: 5 past the room's corner
        ·  · · · ·  M M
        ·  · · · ·  M M
        ·  · · · ·  M M
        ·  · · · ·  M M
    7   ·  r r r r  M M
        ·  r r r r  M M      S  the stairs, down to the mine
        ·  r r r r  M M      r  the room: 4 wide, 8 long, 2 high
        ·  r r r r  M M      M  the main tunnel: 2 wide, 3 high, along
        ·  r r r r  M M         the stairs, on past the room both ways
        ·  r r r r  M M      b  a branch: 1 wide, 2 high, to the right,
        ·  r r r r  M M         5 blocks of rock to the next
    0   S  r r r r  M M
        S  · · · ·  M M
        S  · · · ·  M M
        S  · · · ·  M M
        S  · · · ·  M M
   -5   S  · · · ·  M M  b b b b b …   the first branch the other way
```

- **The room:** four by eight and two high, to the right of the stairs. It holds a workshop:
  a crafting table, a furnace, and a chest for the rubble.
- **The main tunnel:** on the room's far side, two wide and three high, along the stairs'
  direction and the other way. It starts 24 blocks long and grows by 16 each round.
- **The branches:** off the main tunnel to the right, one wide and two high, 16 blocks long,
  with torches every eight blocks.
- **Ores:** every ore in the walls is dug.
- **Floors:** a hole in the floor ahead (a cave under the tunnel) gets a block before the robot
  steps on, as on a bridge. If it can't be filled, the tunnel ends there.
- **Danger:** a branch that meets water or lava stops, and the next branch is dug. Water or
  lava in the room's rock moves the whole mine further down the stairs.

## Day 2: a field

`in` (up the stairs, home), `out`, then `farm`, a wheat field by home:
- **The layout:** a pool of water 2x2 at the west end, two channels of 10 water blocks from it
  (three apart), and four rows of farmland along them — seed, water, seed, seed, water, seed
  across the field: 40 plants, all within the 4 blocks
  water keeps farmland moist.
- **The plot:** about thirteen by six (pool, channels and a headland across the east end, the
  way on foot round the water), within 32 blocks of the shelter but more than 12 from its room
  and the top of its stairs: the one needing the least preparation, then with the most soil at
  one level, then the nearest; in the sun (not under a hill's overhang), no hole deeper than a
  block. Snow-covered or snowy ground
  is no soil, and plots more than 8 blocks above home are passed over, so a home in the
  mountains gets its field lower down. A plot in the cold (its pool, the channel's middle or
  its far end) is passed over too: its water would freeze. A home in the cold gets its field
  round the nearest warm land with grass its map knows, no more than 8 blocks above home.
- **No round the water found in the cold:** the field round the day-1 water is made only where
  that water is not in the cold.
- **Levelling:** a plot that isn't flat soil all over is made so. What stands above the level
  is dug off (three high, trees felled whole round it), and holes and blocks that aren't soil are replaced with dirt, dug
  from round the plot (never from the plot itself).
- **Water:** the pool and the channel are dug one deep and closed in first: a solid block under
  every cell, round the pool's outer sides, at the channel's far end and along its sides (dirt,
  as they are the soil rows), so no water runs over the plot's edge or into a cave. Then a
  bucket of water is poured into two opposite corners of the pool, and the other two corners
  fill by themselves: a source of still water that never runs dry. The two buckets are
  fetched from wherever there is water, by day, exploring for it if none is in sight (never
  from its own pool while it holds one source: alone, that one would run dry). Then the
  channel is filled from the pool, a bucket at a time, until all 8 are still water. The bucket
  is made from the iron of its mines. Without one yet, the field is dry for now (wheat grows
  on dry farmland too, more slowly), and its water is made at a later harvest.
- **No iron yet:** without a bucket, the field is made round the water found on day 1: the
  grass and dirt at the water's level within 4 blocks of it, tilled and sown. The field by home,
  with its pool and channel, comes at a later harvest, once a bucket is to be had; both are
  tended from then on.
- **Sowing:** the rows are tilled with a hoe and sown with seeds broken out of grass and ferns,
  the rows next to the channel first, as far as the seeds go.

Then logs until dusk, and `in`.

## Night 2: diamonds

`down` (back down its own stairs), `get iron_pickaxe` (if the mines have not made one yet),
`descend -54`, `mine`; then more mines until morning.

## Day 3 and on: a life

Every day: `in` (up its stairs), `out`, then rounds of `harvest` and `get logs +16` until
dusk, and `in`. Every night: `down` its stairs, `mine`, then on down and more mines until
morning.

`harvest` tends the field:
- The ripe wheat is cut and picked up.
- The bare farmland is sown again, and more is tilled as the seeds allow, up to the 40 cells
  of the field's rows.
- The wheat is baked into bread, three to a loaf.

The field grows with every harvest, since each plant gives back more seeds than it took.
Fish and bread are the robot's food; it hunts nothing. Looking for food it fishes first: a
rod (three sticks, two string — from the spiders it fights, the cobwebs it meets underground),
cast along the field's first channel from the headland, else into still water it knows; a bite
is the splash at its own bobber. The catch is cooked in its furnaces. Its harvest fishes too
while it has fewer than six fish. mc26's `week` scenario lives a week this way
(`MC26_DAYS` for more).

## Its map

The robot remembers what it has seen round home, simplified as a person remembers a place:
- **What:** where the woods are, the water, lava, grass for seeds and sand, in areas of 16×16
  blocks, with the ground's height.
- **Learning:** it maps the loaded world within 48 blocks of it every half minute as it goes,
  and keeps the map in its memory, so a robot started again still has it.
- **Using it:** when what it needs is not in sight (logs, water for its bucket, seeds, sand),
  it goes to the nearest area it knows has some, before it searches blind.
- **Scouting:** `scout [radius]` walks to eight places round home (48 blocks off) and back,
  mapping as it goes. It comes home at dusk.

## A village

From day five, the robot goes looking for a village by day:
- **Finding one:** `village [legs]` goes to the village it knows (its map marks a bell, or
  villagers seen), or searches leg after leg as it does for anything (downhill first, turning
  at oceans) until it sees villagers.
- **Trading:** `tradeall` opens the trades of the villagers about (up to eight), and makes
  those it can pay for. First come the ones that give emeralds for what it has much of (coal,
  wheat, sticks), a few times each, then food bought with emeralds. Every offer seen is
  written down: real villagers' trades are what exercise the merchant's packets.

## Smelting

At the end of each night, at the mine, and each evening at home, `smeltall` smelts what it
mined into ingots:
- **What:** what it carries that smelts into ingots (raw iron, gold, copper), from the game's
  smelting recipes.
- **Furnaces:** one for every 8 things, at most 9 at a place. It uses the furnaces within 6
  blocks and makes the rest from cobblestone, setting them into the rock walls round it, so a
  room's floor stays free and its stairs clear.
- **Rounds:** 8 things and their fuel into each furnace, the furnaces working side by side
  (10 seconds a thing), then the ingots taken out, round after round.
- **Then:** the best tools it can now make (an iron pickaxe, a sword).

## Night

**Before dark:** the robot watches the sun. It reckons the light left from the time of day
(dusk at tick 12000) and the way home from how far home is (three blocks a second, and a
margin of half the walk again, at least twenty seconds). With less light left than the way home takes, it stops its outdoor work
(wood, exploring, scouting, the village trip) and goes home. When home is too far to reach
before dark, it builds its shelter where it is while there is still light. With little light
left, it does not set out at all.

Night under the open sky is where a player dies. Near home, `in` takes the robot into its
shelter. More than 48 blocks from home with the way home over fifty seconds (its margin
counted), whatever it is doing, it walls itself in where it stands: cobblestone round its feet and head and over its head. It waits for the morning,
opens the side toward home, and goes on.

## Losing little

- **Banking:** each time it comes `in`, it puts its valuables in the chest at home: diamonds,
  emeralds, gold and copper, raw ores, redstone, lapis, and its iron once its iron tools and
  bucket are made. It keeps its tools, food, torches, coal and blocks. A trip that ends badly
  loses little.
- **After a death:** what it carried lies where it died, for five minutes. It remembers where,
  runs back (`recover`) while its things can still be there, picks up what lies round, and
  goes home. In the scenario a death is counted, and the robot recovers and goes on; a run
  with a death is not a survived one.

## Rules it keeps all along

- **Time:** it reads the time of day from the server's clock: day, dusk, night, dawn. It
  measures how long each lasted.
- **Best tools:** as soon as it carries the iron, it smelts it into an iron pickaxe, then an
  iron sword. With three diamonds it makes a diamond pickaxe. A pickaxe that wears out is
  replaced on the spot. Each block is dug with the fastest tool it carries.
- **Food:** at 14 food or below it eats what it carries: apples, bread and the like.
- **Healing:** it rests before the stairs or a mine until its health is 16 (eating so it can
  heal), and whenever it falls below 10 on the way.
- **Fighting:** it fights back against a hostile mob that comes close or hits it.
  - Against one mob it fights on down to 6 health. Turning its back on a single mob on a
    bridge or in a tunnel is worse.
  - Against two it backs off at 12, against three or more at 16.
  - It backs away from any creeper within 8 blocks and never fights one.
- **Digging ahead:** never the block under its feet (it would fall) nor the one over its head
  (what is above that would fall on it). It digs only beside it and ahead, and leaves ores in
  the floor alone: their hole is a step down it would walk into later. Even climbing, the
  headroom for the next jump is dug as the third block of the step ahead.
- **Lava:**
  - A block with lava behind it is not dug; the stairs or the tunnel go another way.
  - With lava near, it digs nothing that would leave it without a free block to step back to.
  - Lava turning up beside it anyway: it steps back to a free block away from it and puts
    cobblestone where it stood, sealing the lava off.
  - The paths it walks keep a block away from lava where they can.
- **Streams:** its paths go round moving water whenever there is a way round. If it is in a
  stream anyway, it steps out to the nearest dry place.
- **Water and lava:** never dug into. The paths it walks keep out of water where there is a
  way round, and out of streams even more.
- **Falling blocks:** gravel and sand above a dug block are waited for and dug too. If it ever
  finds its eyes inside a block, it digs itself out.
- **Harmful plants:** a berry bush, a cobweb or fire in its way is dug, not walked through.
- **A full pack:** with fewer than six slots free, the rubble (dirt, gravel, granite, diorite,
  andesite, extra cobblestone) goes into a chest: the mine's, or one put down where it is.
- **Getting there:**
  - It walks a far goal in legs of 40 blocks.
  - Where no path leads (a ravine, a pit), it walks to the nearest point it can reach and digs
    stairs from there toward the goal.
  - Fallen on the way (more than three blocks: off an edge, pushed): if the way on from down
    there is short (at most twice the distance and 24 more), on. Else back up to where it fell
    from, as a person does: stairs dug into the rock, a block of dirt or stone put under a
    step with nothing under it; a way round only as long as three times the fall and 16 more
    counts. Then on from there.
  - Its paths keep off trees: a step on a log, wood or leaves (a big mushroom, mangrove roots
    too) costs ten steps on the ground, so it walks under the trees, not over their crowns,
    and over them only where the ground has no way (a hillside, a mountain), a few steps. A
    tree is felled from the ground; a log out of reach from there is cut from a pillar of dirt
    in the felled trunk's place, never from the tree's own crown.
  - It does not tunnel to a resource it cannot walk to; it takes the next one, or another way
    (charcoal instead of coal).
- **Searching:** when nothing in sight gives what it needs, and its map knows of none, it
  explores in rings round home: eight places a ring, more on the wider rings (one every 64
  blocks round, north, north-east, east and on round), the first ring 32 blocks out and each
  next one 32 further. It looks again after each place.
  All four ways get covered, the nearest first, never round in a circle. Places its map
  already knows are passed over, and so are places in water.
- **Stone under soil:** a hill's stone lies under its dirt. With no stone in reach, it digs a
  short stair down through the soil (five steps at most) till stone shows.

## What it knows, and from where

What it knows of kinds of things is the game's data, not lists of its own:
- **Recipes and drops:** go-mc26's recipe and loot packages, generated from the server jar's
  data pack, plus the recipe book the server sends.
- **Food:** every item whose default components include food, the most nourishing first,
  leaving out those whose eating does something else (rotten flesh, a spider eye).
- **Hostile mobs:** the mob category `monster`.
- **Logs, ores, crops:** the tags the server sends (`#minecraft:logs_that_burn`,
  `#minecraft:iron_ores`, `#minecraft:crops`).

Judgement stays in its code, said as such:
- which ores are worth digging;
- the neutral monsters it leaves alone (an enderman, a piglin);
- that a crop is sown and not dug;
- what it puts in a chest as rubble.

## How it is tested

mc26's end-to-end run (`mc26 e2e --version 26.3 --only days`) starts a server with an ordinary
world (seed 26265 unless `MC26_SEED` says otherwise) and puts the robot in its nearest forest
at the first morning. The game's clock runs as it does for a player.

The run fails if:
- the robot dies;
- a step the rest depends on fails (the stairs, the iron pickaxe);
- at the end the server does not show an iron pickaxe and a diamond in the robot's inventory;
- the server log holds a complaint about the robot.

Every run is kept with its events, its output, and the commits and seed it ran with. The
viewer can follow a run live or replay a kept one faster than it ran.
