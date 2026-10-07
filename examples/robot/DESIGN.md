# How the robot decides, and why

This is the robot's design as it is now: what it knows, the rules it follows and the reasons
for them. Most rules came from a run where the robot died, got stuck or did something no
player would do. The run is named where it helps. Read this before changing the robot: a rule
that looks odd is usually the fix for one of those runs.

`SCENARIO.md` describes the course it plays (day 1, night 1, …). `EVENTS.md` is the format of
what it reports. `ROADMAP.md` covers the kinds of robot still to come.

## The kind of robot this is

This robot is **omniscient within the protocol**. It uses everything the server sends a
client, not only what a player on that client would see:
- every block of every loaded chunk, behind walls and under the ground;
- every entity's data (a villager's profession, a mob's equipment);
- the registries, the recipe book and the tags;
- the light the server computed.

It remembers what it learns exactly, in a file that outlives a restart (`-memory`, see
`memory.go`): its home, its stairs, its fields, the water it found, the search's rings, and a
map of 16×16 areas.

It still plays by the game's rules. It moves with the client's physics, digs at the client's
speed and fights with the client's cooldown, and the server corrects it when it doesn't. It
uses no commands. What it knows is a game client's, not a player's. `ROADMAP.md` says how the
later robots will know less.

## Data: from the game, not from lists

Where the generated library has the data, the robot uses it. Recipes, loot tables, block
behaviour (collision, hardness, tools), entity types, item components and entity data
layouts all come from the game jar through go-mc26. Recipes the server has unlocked, and
tags, come from the server at runtime. Only judgement stays hand-written:
- which mobs are neutral;
- which ores are worth a tunnel;
- what counts as junk;
- which foods it eats (any with a food component and no bad effect; rotten flesh only when
  starving).

## Commands and the harness

The robot is driven by commands: from its console (stdin, which is how the test harness talks
to it) or from chat. The harness in mc26 (`gen/internal/e2e/days.go`) plays the scenario as a
list of commands per time of day: `out`, `water`, `get logs +16`, `farm`, `harvest`, `scout`,
`in`, `down`, `descend`, `mine`, `smeltall`, `village`, `tradeall`.

A command that stops on purpose answers `stopped: …` or `staying in: …`, not an error. A
command stopped for the dark answers `stopped: too late in the day`. Errors are for failures.
With `MC26_HOLD=1` an error holds the run so the robot can be fixed and resumed. A deliberate
stop must never hold it.

## Time of day

- **Dusk** (`dusk.go`): the light left is compared with the way home (distance ÷ 3 blocks a
  second, plus a minute). Outdoor work stops when the light is shorter than the way home.
  The robot then goes home if it can get there, or walls itself in where it is. Within 16
  blocks of home it always goes in: the minute of margin once made its own door "too far".
- **After dark:** no trees, no search legs, no scouting, no village trip. If it is out it
  heads home or hides; if it is in, it stays in. The check is the same from inside the room
  (`dayWork`), not just out under the sky: a scout retried at night once walked out of the
  door.
- **Under the sky** is judged by the sky light at its head (13 or more). Leaves are no roof,
  so it doesn't think it's under one while beneath a tree.
- **Night commands retried by day** (after a hold) stop at once: by day, with a home, the mine
  is tonight's work.
- **Waiting** for the morning (walled in) is logged as `robot: waiting for the morning`. The
  harness then moves the clock on (`time add`, which keeps the day count), unless
  `MC26_REALTIME=1`.

## Home

- **The room's plan** (`shelter.go`): it's dug into a hill, 3×3. Along its right wall stand
  the table (cell 2), the chest (3) and the furnace (4). Torches are on the left wall. A
  3-block tunnel leads to the stairs.
- **Furniture:** near home, a table, chest or furnace is the room's own, at its place in the
  plan, and is put back there if it's gone. It is never put down on the path outside.
- **The door** is shut from inside. Coming back from outside to a shut door, it goes to the
  front and opens it. A door that won't shut (something stands in it) is tried again, then
  left.
- **Going in from far away** (after a death, a long trip) is done in legs over the land. Its
  stairs are only followed when it is near them.

## The mine

- **Digging:** it digs ahead only, never under itself or over its head. Lava gets a free block
  to step back to and is sealed with stone. A step that would open onto water or lava turns
  away.
- **Bridges:** a cave under the next step is bridged under the rock only. Where the stairs come
  out of the hill into the open air, they turn back into the rock. On the land it never
  builds a block over the air: no bridges, no floors over a drop. It walks round, digs its
  way, or builds a stair of blocks up.
- **The stairs** are walked step by step, never by one long path search to the mine's end:
  that search once found a cheaper way over the land, at night. A step that turns
  diagonally gets its corner opened. Goals under ground never get legs over the land.
- **Ores** are dug from beside, never from the floor under it.
- **Tools:** a worn-out pickaxe is replaced with a stone one first, from the cobblestone it
  carries. With no pickaxe at all, it makes a wooden one first. This is done once, not round
  again: making a pickaxe needs stone, which needs a pickaxe. With no room in rock it can't
  dig, the table goes on a stair for the craft.
- **Unfit for the mine:** with no pickaxe, or below 10 health with nothing to eat, it doesn't
  go down. It goes home from wherever it is instead.

## Fighting

`defend` runs at the start of every `goto`, in every walk loop, at each step of climbing and
digging out, and between digs. Every death in the first runs came from a loop that didn't
call it: a path that failed at once, a long hand-dig, a stream reflex.

- **Order:** a creeper within 8 is fled, after a hit if it is within 3.5. If it has just been
  hit while a creeper is still more than 4 blocks off, the other mob, the one that hit it,
  comes first.
- **Archers** (skeleton, stray, bogged, pillager, witch): running away gets it shot in the
  back. Against an archer it walls itself in at any health, and waits until none is within
  16. A second skeleton death came from running down a straight tunnel.
- **A mob that keeps up** (retreated from 3 times in 30 s) means it walls itself in, anywhere.
- **Hurt in a tunnel** (under 8 health, under rock): it walls itself in instead of running
  down the tunnel, where the zombie follows.
- **No running off while walling in:** a fight inside the wall-in once left the walls half
  built.
- **Walls** are made of cobblestone or dirt; when those run out, logs and planks. After a
  death it had only logs. The night's hideout keeps 8 blocks over for a later wall-in.
- **Long digs:** if a hostile is within 6 and hits it during a dig (stone by hand), it lets
  go of the dig (`act.Hands.CancelDig`) and fights.
- **Heal reflex:** below 8 health it rests to 14, at most once a minute.

## Food

It never kills friendly animals. Food is wheat (bread), apples from leaves, and what it can
make from what it sees: mushroom stew, berries. When starving it eats rotten flesh from the
zombies it kills, and when hungry after a fight it collects their drops. Food is the hardest
part of a week: without bread its health stays low, and low health is when mobs kill it.

- **Hurt with nothing to eat** (under 10): no scouting, no village trip, no search legs, logs
  only within 48 of home, and remembered places measured from home only within 48.
- **Plants by hand:** grass, flowers and wheat are broken with the hand (an empty slot, or one
  with no tool), never the pickaxe.

## The field and its water

The field is the food. Its water is what makes the wheat grow fast enough.

- **Finding water on day 1** (`water.go`, the `water` command), in this order:
  1. still water in sight: a source under the sky with more water beside it and dry land at
     its edge;
  2. a spring (a lone source) if there is no pond or river;
  3. ice melted into water: an ice block broken over a block leaves a source (snow never
     does);
  4. where its map has seen water;
  5. **downhill**: the lowest open ground 32 blocks off in 8 directions, leg after leg,
     because rivers lie at valley bottoms;
  6. the search's rings round home, a place every 64 blocks round each ring, all day, looking
     round after every leg, a failed one too (water is what stops a walk).

  The place is remembered (`water` in memory). Still water seen under ground is remembered
  too (`under`), for buckets only.
- **With a bucket** (iron: after the iron pickaxe, two buckets before the iron sword): the
  field by home (`farm.go`).
  - **Layout:** a 2×2 pool at the west end, two channels of 10 from it, and four rows of
    farmland along them (40 plants), a headland across the east end.
  - **Digging:** everything is dug one deep, from beside, never standing on a cell to be dug.
    Then every floor, the pool's outer sides, the channel's end and its sides (dirt) are
    shut.
  - **The pool:** two buckets, fetched from the water found (never from its own half-made
    pool), are poured into opposite corners. The other two fill themselves, which makes an
    endless source.
  - **The channel:** filled from the pool, a bucket at a time.
  - **Pouring and filling** are done standing right beside the cell, aimed down into it.
    Poured from afar, the water landed outside the pool and ran away.
  - **Sealing:** its own field's water is never sealed, and the stream reflex is off while it
    is being made. The reflex once sealed the new pool.
- **Without a bucket:** the field is made round the water found. It goes there first, since
  its land isn't loaded from afar. It lowers the bank (the grass a block over the water is dug
  off) so the dirt at the water's level is tilled and kept wet.
- **The harvest** tends both fields. It cuts only ripe wheat (age 7), fetches seeds from grass
  within 32 of the field, and bakes bread. It builds the field by home once a bucket can be
  had.
- **Water on the land is never sealed.** Sealing is only for water running down its stairs
  or into a mine. It once sealed the spring it was looking for.

## Getting stuck, and getting out

- **Still for 3 minutes** on one command: the harness holds the run.
- **Put back by the server tick after tick** (more than 40 corrections in 10 s): its world and
  the server's disagree. It ends itself (`robot: relog`) and the harness starts it again at
  once. Seen after a pickaxe broke mid-dig (more than 1,100 corrections).
- **Shut in** (fewer than 20 places reachable):
  - at night under the sky, it waits in its hideout until morning;
  - by day, or in rock, it digs toward home in rounds, only where there is a floor, by hand
    if no pickaxe is left. A hideout once opened its home-facing side onto a ravine.
- **Covered over** (sky light 0 out on the land): it climbs to the sky over it, or walks to the
  nearest open place within 16.
- **Exploring** never starts from under a roof. Two failed legs end a call, which stops the
  rings racing outward. Out-of-sight ring points are walked toward, not skipped.

## Death

It respawns after a second. If it can be back within 5 minutes, it goes for its things, but
not where mobs still stand. It once walked back into the skeleton that had killed it. After
a death it has nothing: no mine (no pickaxe), and no exploring or long trips while hurt with
nothing to eat.

## Testing it

- **mc26 e2e** (`docs/testing.md` in mc26):
  - `robot` (steps), `days`, `week`;
  - `field` (pool and channel from water fetched 30 off);
  - `fieldwild` (no bucket: a field round a pond);
  - `farmstart` (the first day handed over: shelter, real water found, a field);
  - `village`.
- **Kept runs** are in mc26's runs directory (`MC26_RUNS`): one directory per run (events,
  meta, robot log) and the index `runs.jsonl`.
- **Watching:** a viewer reads the events. Replays come from kept runs.
- **Resuming:** with `MC26_HOLD=1` a held run is fixed and resumed: re-assemble the kit
  (`mc26 kit --version <v>`), then `touch temp/e2e/<v>/<scenario>.resume`. To load a new build
  into a running run, stop the robot process. The run holds; then resume it.
