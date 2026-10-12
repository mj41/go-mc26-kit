# The robot's events: the format

The robot (`-events <file>`) appends what it does and sees to a file. Anything that wants to
show or study the robot reads that file: the dashboard, a replay, an analysis. The robot does
not know who reads it.

**This file is the contract between the robot and its readers.** The robot side (this
directory) writes to it; a reader codes against it and nothing else. Schema **1**.

## The file

- **JSON lines:** one event per line, UTF-8, `\n` after each. A line is written whole, in one
  write, as the event happens: there is no buffering to wait for.
- **Append-only:** a reader follows it as it grows (like `tail -F`). The last line may be
  incomplete while it is being written; a reader keeps it until its `\n` arrives.
- **Recreated, not rewritten:** a new run may delete the file and create it again under the
  same name, or a link of that name may be pointed at a new run's file. A reader notices that
  the file shrank or is a different file, and starts over from its beginning.
- **Its first line** is a `start` event (below) saying which schema the rest follows.
- **Rates:** `state` twice a second (the robot's `-events-hz` raises it, up to 20 a second, for smooth replays) and `view` every two seconds, `blocks` with a `view` when
  they changed; everything else as it happens. A `view` is about 110 KB, so a run writes about 3 MB a minute, some 200 MB an
  hour.

## The envelope

Each line is an event in a common envelope (what an event hub takes as it is):

```json
{"id": "18dba2aaf35e16a080be05ed", "ts": "2026-10-05T14:42:11.921073824+02:00",
 "source": "mc:Settler", "kind": "telemetry", "name": "state", "data": {…}}
```

| Field | Type | Meaning |
|---|---|---|
| `id` | string | Unique and sortable by time: 16 hex digits of Unix nanoseconds, then 8 random. |
| `ts` | string | When it happened, RFC 3339 with nanoseconds and the local offset. |
| `source` | string | `mc:<robot name>`. Several robots write several files. |
| `kind` | string | `telemetry` (a measurement, the latest supersedes the one before) or `event` (a happening). |
| `name` | string | What the event is; the `data` follows from it (below). |
| `data` | object | The event's fields. |

## Conventions

- **Coordinates** are the game's: x east, z south, y up. A **block position** is the integer
  corner of its cube (`floor` of a point in it). A **point** (a player, a mob) is a float with
  two decimals.
- **Yaw** is the game's, in degrees: 0 south (+z), 90 west, 180 north, 270 or −90 east.
  **Pitch:** 0 level, positive down.
- **Names** of items, blocks and entities are the game's ids. `inv` and `view.entities` give
  them without the `minecraft:` prefix; elsewhere they have it.
- **Colours** are map colours (the colour the game draws a block with on a map), RGB packed
  into an integer: `0xRRGGBB`. Two values are special:
  - `0`: open (air, or anything the map leaves out);
  - `view.unknown` (`0x1000000`): not known, the chunk is not loaded.
- **Grids** are flat arrays, row after row (`row * width + column`).

## Telemetry

### `state`: twice a second

| Field | Type | Meaning |
|---|---|---|
| `x`, `y`, `z` | float | Its feet. |
| `yaw`, `pitch` | float | Where it looks. |
| `health` | float | 0–20 (0 is dead). |
| `food` | int | 0–20. |
| `dim` | string | Dimension id, e.g. `minecraft:overworld`. |
| `doing` | string | The command it is on (`descend 16`), `""` when idle. |
| `day` | int | 1 on the first day; one more each morning. Absent before the clock is known. |
| `tick` | int | Time of day, 0–23999. |
| `phase` | string | `day` (0–11999), `dusk` (–12999), `night` (–22999), `dawn`. |
| `held` | string | The item in its hand, absent if empty. |
| `inv` | object | Item (no prefix) → count, the whole pack (not armour or offhand). |
| `still_s` | float | Seconds since it last moved a block: a robot standing long is stuck or idle. |
| `target` | {`x`, `y`, `z`, `what`, `progress`} | The block its hands are on right now: `what` is `dig` (being dug), `place` (a block being put there) or `use` (a door, a chest). `progress`: a dig's, 0 to 1 (the client's destroy progress, the cracks the game shows), absent for the others. Absent when its hands are idle or on something else (a fight, eating). A dig shorter than half a second may not show. |
| `eye` | float | Its eyes' height over its feet: 1.62 standing, 1.27 sneaking. It does not swim or crawl. |
| `swinging` | bool | `true` while its arm swings (a dig, an attack, a placing: within the last third of a second), else absent. |
| `armor` | {`head`, `chest`, `legs`, `feet`} | What it wears, each an item id (`minecraft:iron_chestplate`); a slot with nothing left out, the whole field absent when it wears nothing. |
| `offhand` | string | The item in its other hand, absent if empty. |
| `biome` | string | The biome at its feet, `minecraft:plains`. Absent when not known. |
| `light` | {`sky`, `block`} | The light at its eyes, 0–15 each, as the server sent it: `sky` the sky's (not dimmed for the time of day; the game does that when drawing), `block` the torches' and lava's. Absent when not known. |
| `id` | int | Its own entity id (whose swings, hurts and moves the `tick` event tells as others'). |
| `hotbar` | [9 × [`item`, `count`] or `null`] | Its hotbar left to right, as the game shows it at the bottom of the screen (the inventory menu's slots 36–44); `null` for an empty slot. |
| `selected` | int | The hotbar slot in its hand, 0–8. |
| `using` | {`item`, `ticks`, `of`} | It is eating or drinking `item`: `ticks` gone of the `of` it takes (the hand at its mouth). Absent otherwise. |

### `screen`: when what is on its screen changed

The screen it has open, as the game would show it to a person playing: a crafting table's, a
furnace's, a chest's — or its inventory while it crafts by hand (a person opens that with E and
the server is never told: it counts as open from a click in it till a second after the last).
Told when it opens, after each tick in which anything on it changed (a click, a slot the server
set, the cursor, a furnace's progress: at most 20 a second), and once when it closes. Each one is
whole: a reader needs only the last.

| Field | Type | Meaning |
|---|---|---|
| `open` | bool | `false` when it was closed: no other field then. |
| `id` | int | The menu's id; `0` its inventory. |
| `type` | string | The menu's type, the game's id (`minecraft:crafting`, `minecraft:furnace`, `minecraft:generic_9x3`, `minecraft:smoker`…); `inventory` for its own. |
| `title` | string | The title as the server sent it, as text (a named chest's name); absent when none. |
| `size` | int | How many slots the menu has, its own then the 36 of the player's inventory (27, then the hotbar's 9) — `inventory`: the inventory menu's 46. |
| `slots` | [[`slot`, `item`, `count`]] | Every slot with something in it, by the menu's slot numbers (the game's: a crafting table's result 0, its grid 1–9; a furnace's input 0, fuel 1, result 2; the inventory menu's result 0, grid 1–4, armour 5–8, 9–35, hotbar 36–44, offhand 45). |
| `cursor` | [`item`, `count`] | What the cursor carries (picked up from a slot), absent when nothing. |
| `data` | [int] | The menu's data values as the server sent them (a furnace's: lit time, lit duration, cooking progress, cooking total time — its flame and arrow), absent when none. A change of these alone is told at most four times a second. |
| `clicks` | [{`slot`, `button`, `mode`} or {`recipe`}] | The clicks it made in this menu since the last `screen`, oldest first: `mode` as the game names it (`pickup`, `quick_move` — a shift-click —, `swap`, `throw`, `quick_craft`, `pickup_all`, `clone`), `button` 0 left, 1 right; `recipe` (an item id): it had the server put a recipe's ingredients into the grid, as a click in the recipe book does. |
| `block` | {`x`, `y`, `z`} | The block it opened the screen from (right-clicked), when it did. |

### `tick`: what each tick brought

What its client was told in one client tick that a watcher draws as the game's client draws
it, so a replay can move everything tick by tick: the client interpolates each entity to the
positions the server sends (26.3: living entities along each packet's steps, over their
ticks — SteppedInterpolationHandler; boats and minecarts over 3 ticks —
LinearInterpolationHandler) and draws each frame between the last two ticks. One line at the
end of each tick that brought anything below (at most 20 a second, none while nothing moves),
within 32 blocks of it; never more than one tick in a line. About 7 KB/s while things move.

| Field | Type | Meaning |
|---|---|---|
| `t` | int | The client's tick count: one more each tick it is in the level. |
| `me` | [x, y, z, yaw, pitch] | Its own feet and look after this tick's move, when changed since the last `tick`. |
| `ents` | [[id, x, y, z, yaw, pitch, head, ticks]] | Entities the server moved or turned this tick: each step of a move's path (26.3's stepped moves have several, in order) with the ticks the client takes to it — `0` for its type's own update interval (a plain move, a teleport); a turn alone is one entry where it is. Absolute, as the client holds them. `head` is its head's yaw. |
| `new` | [{…}] | Entities that came into its tracking (add_entity), each as `view.entities` gives it. |
| `gone` | [id] | Entities removed (remove_entities). |
| `swing` | [id] | Arms that swung (animate: main or off hand), its own too (the server sends a client none of its own). |
| `hurt` | [id] | Entities hurt (hurt_animation, damage_event; once a tick). |
| `died` | [id] | Entities that began to die (entity_event 3). |
| `pickup` | [[item, by, count]] | An item entity picked up by an entity (take_item_entity): the item flies to it. |
| `explode` | [[x, y, z, radius]] | Explosions (their blocks come as `changes`). |
| `motion` | [[id, vx, vy, vz]] | Velocities the server set (set_entity_motion), blocks a tick: what a thrown or dropped thing flies with between its positions. |

### `view`: every two seconds

What is round it, all relative to block position `x`, `y`, `z` (its feet) in a square of
`n = 2*size + 1` blocks (`size` is 32, so `n` is 65).

| Field | Type | Meaning |
|---|---|---|
| `x`, `y`, `z` | int | The block its feet are in: the centre of every grid. |
| `size` | int | How far the view reaches each way (32). |
| `facing` | [int, int] | The way it looks, as a unit step `[dx, dz]`: `[0,1]` south, `[-1,0]` west, … |
| `unknown` | int | The colour value meaning "not loaded" (`0x1000000`). |
| `top`, `depth` | n×n | Its level from above: the first block down from its head (`y+1` down to `y−8`), and its `dy` (int). Row = z from `z−size` (north) to `z+size`; column = x from `x−size` (west). `0` where open all the way. |
| `surface`, `height` | n×n | The land from above: the highest block of each column, from the world's top down to `y−48`, and its height over the robot's feet (int, may be negative). Same rows and columns. |
| `sliceX` | n×(up+down+1) | A slice west → east through its feet: row 0 is `y+up`, the last row `y−down`; column = x from `x−size`. |
| `sliceZ` | n×(up+down+1) | The same, north → south (column = z from `z−size`). |
| `up`, `down` | int | 16 and 24: the slices' rows over and under its feet. |
| `legend` | [{`c`, `name`, `n`}] | Every colour in this view, the commonest block with it (`diorite`), and how many cells of it. Most first. |
| `entities` | [{`id`, `type`, `x`, `y`, `z`, `yaw`, `w`, `h`, `equipment`, `baby`, `variant`, `profession`, `size`, `block`, `pitch`, `chest`, `item`, `count`, `color`, `sheared`}] | Every entity within 32 blocks: mobs, animals, items (`item`), players. `id` is the entity's id: the same entity has the same id from view to view. `yaw` is the way it faces (the game's degrees). `w` and `h` are its type's box (an adult's), absent for a type not known. `equipment`: what the server said it holds and wears (set_equipment), slot → item id, slots `mainhand`, `offhand`, `head`, `chest`, `legs`, `feet`, `body`, `saddle`; empty ones left out, absent with none. `baby`: `true` for a baby (its type's baby flag), else absent. `variant`: what kind of its type it is, the registry or enum name without `minecraft:` — a cat's, a wolf's, a frog's, a pig's, a cow's, a chicken's (`tabby`, `pale`, `warm`…), a llama's and trader llama's (`creamy`, `white`, `brown`, `gray`), a parrot's, an axolotl's, a fox's, a mooshroom's, a rabbit's, a horse's (`colour/markings`: `chestnut/white_dots`), a villager's or zombie villager's land (`plains`, `taiga`…) with its `profession` (`farmer`, `none`…). `size`: a salmon's (`small`, `medium`, `large`). `block`: a falling block's block state, as in `blocks` (`minecraft:gravel`). `pitch`: an arrow's, a trident's, a squid's (degrees, as `state.pitch`). `chest`: `true` on a llama, donkey or mule carrying one. `item` and `count`: for an `item` entity (a dropped item), the item it is and how many. `color`: a sheep's wool colour (`white`, `light_gray`, … the dye names), and `sheared`: `true` when it is shorn. |

### `blocks`: with a `view`, when they changed

Every block state of a box round it, for a reader to build the world in 3D (the dashboard
draws it from the robot's eye). Told with a `view` when the box moved or a block in it changed
since the last `blocks`; a reader keeps the last one it read.

| Field | Type | Meaning |
|---|---|---|
| `x0`, `y0`, `z0` | int | The box's lowest north-west block: its smallest x, y and z. |
| `w`, `h`, `d` | int | Its size along x, y and z: 49, 32 and 49. It reaches 24 blocks each way along x and z from the robot's feet, 16 under them and 15 over. |
| `palette` | [entry] | Each block state in the box once, in no order (below). |
| `cells` | string | One cell per block, its palette index: an unsigned 16-bit little-endian integer, `65535` for a block not known (not loaded). Layer by layer from the bottom (y), each row by row from the north (z), each from the west (x): cell `(y*d + z)*w + x` for the block at `x0+x`, `y0+y`, `z0+z`. The bytes are compressed with deflate (raw, RFC 1951), then written as base64. About 5 to 30 KB. |
| `biomes` | {`palette`, `qx0`, `qy0`, `qz0`, `qw`, `qh`, `qd`, `cells`} | The box's biomes as the game keeps them: one for each cell of 4×4×4 blocks (a "quart"). The quarts covering the box start at `qx0`, `qy0`, `qz0` (`floor(x0/4)`…: quart `qx` spans blocks `qx*4` to `qx*4+3`), `qw`×`qh`×`qd` of them (13 along x and z, 8 or 9 along y, as the box lies on them). `palette`: the biomes' ids without `minecraft:` (`plains`, `forest`). `cells`: one byte per quart, its palette index, `255` not known (not loaded), cell `(qy*qd + qz)*qw + qx`; deflated and in base64, as `cells`. A biome change in the box is a change too: the `blocks` is told again. |

A palette entry:

| Field | Type | Meaning |
|---|---|---|
| `name` | string | The state as the game writes it in a command: its block's id, then its properties in the order the block declares them, `minecraft:oak_stairs[facing=east,half=bottom,shape=straight,waterlogged=false]`. |
| `c` | int | Its map colour, `0` for none (air, glass, a torch). |
| `cube` | bool | `true` for a full cube (its outline is the whole block). Absent otherwise. |
| `boxes` | [[6 floats]] | When not a cube: the boxes a cursor hits (its outline shape, `[minX, minY, minZ, maxX, maxY, maxZ]` in the block, 0–1): two for stairs, one for a slab, a torch. Absent for none: air, water, lava. |
| `fluid` | {`name`, `height`} | Its fluid, when it has one: `minecraft:water`, `minecraft:flowing_lava`; its surface's height in the block (0–1). A waterlogged block has both boxes and a fluid. |

## Events

| Name | Fields | When |
|---|---|---|
| `start` | `schema` (int), `robot`, `server` | The first line of a run. |
| `cmd` | `line`, `from`, `ms`, and `answer` or `error` | A command ended: what it was, where it came from (`console`, `chat:<player>`, `whisper:<player>`), how long it took, its answer or error. Commands sent by a test harness to poll (`time`, `wait`, `pos`) are cmds too. |
| `reflex` | `why` | A danger seen by the reflexes (four times a second, an order running or not): `hurt`, `a warden near`, `a creeper close`, `a mob close`, `in a wall`, `lava beside`, `in a stream`, `badly hurt`, `hungry`. The running order is paused at its next safe point, the danger dealt with (defend, eat, rest), and the order goes on. |
| `stuck` | `doing`, `x`, `y`, `z` | The watchdog found the robot still for 30 s with an order running, not waiting by choice: the order is stopped, the robot gets out toward the order's goal, and the order runs once again. |
| `plan` | `line` | A line of its reasoning: what it is getting, digging, why it turned. Free text for people, not for parsing. |
| `phase` | `phase`, `tick`, `last`, `lasted_s` | The time of day changed: the new phase, and how long the last one lasted (seconds). |
| `hurt` | `from`, `to`, `x`, `y`, `z` | Its health dropped. |
| `ate` | `food`, `level` | It ate an item; its food after. |
| `fight` | `against`, `killed`, `fled` (optional) | A fight ended: the mob type, how many it killed, whether it left instead (a creeper). |
| `retreat` | `health` | It is running from a fight. |
| `still` | `s`, `doing`, `x`, `y`, `z` | It has not moved a block for 30 s (again at 60, 90…). |
| `packets` | `read`, `ids` [{`id`, `name`, `read`, `bytes`, `nohandler`?, `errors`?, `firstError`?}] | What the server sent since the last one (every 20 s), by clientbound packet id: the packets and bytes of each id; `nohandler` when no handler is registered for it; `errors` and `firstError` when a handler returned an error for it (a packet the client could not read). Told only when something was read. |
| `broke` | `item`, `hand` | A tool broke in a hand, as the server says it (entity event 47, the main hand, or 48, the off hand, for the robot's own entity): `item` is what that hand held when the word came (`minecraft:stone_pickaxe`; the server syncs the slot only at the end of the tick), `""` if it is empty already; `hand` is `main` or `off`. A break told, not an item found gone (a death, a drop). |
| `upgraded` | `tool` | It made itself a better tool (`minecraft:iron_pickaxe`). |
| `lava` | `x`, `y`, `z` | Lava turned up beside it (the block given); it stepped back and sealed it off. |
| `advancement` | `id`, `title`, `description`, `frame`, `x`, `y`, `z`, `initial` (optional) | An advancement became done: every one of its requirements met. `id` is the game's (`minecraft:story/mine_stone`). `title` and `description` are in English, as the game shows them ("Stone Age", "Mine Stone with your new Pickaxe"). `frame` is `task`, `goal` or `challenge`. `x`, `y`, `z` are where the robot is. Those already done when it joined come in the first packet with `initial: true`. Each advancement is told once. Recipe advancements (`minecraft:recipes/…`) have no display and are never told. |
| `changes` | `changes`: [{`x`, `y`, `z`, and a palette entry: `name`, `c`, `cube` / `boxes`, `fluid`}] | Each tick the world's blocks changed in the box `blocks` covers (the server's: a placement, a crop grown, sand fallen, a block another player dug; not a whole chunk sent; and the robot's own dig, told when it predicts the break, again if the server's ack undoes it): every change, its block's palette entry as `blocks` gives it and its place, the last of a place in that tick. A reader writes them into the box at once, between two `blocks`. |
| `block_event` | `x`, `y`, `z`, `block`, `type`, `data` | The server told everyone near of a block doing something (ClientboundBlockEvent), as the game's client animates it: `block` is the block's id; a chest's (and an ender chest's, a shulker box's, a barrel's none) `type` 1 with `data` how many have it open now — its lid opens when that goes over 0 and closes at 0; a bell rung, a piston pushing, a note block played have their own types. Any block in sight, not only the robot's. |
| `fished` | `caught`, `cooked` | A fishing ended (`fish`, or for food): how many fish it caught, and how many raw ones it cooked after. |
| `died` | `x`, `y`, `z` | It died there. It respawns a second later; what it carried lies there for five minutes (`recover` goes back for it). |
| `map` | `areas`: [{`x`, `z`, `logs`, `water`, `lava`, `grass`, `sand`, `y`, `seen`, `cold`}] | Every half minute: its whole map, what it has seen round home in areas of 16×16 blocks. `x` and `z` are the area's place divided by 16 (it spans `x*16` to `x*16+15`). The counts are what its surface has (sampled every other column, so about a quarter of the blocks; an area it only saw from afar — the look down from a mountain, every fourth column — has its counts scaled to that, until it is seen nearer). `y` is the ground's height. `seen` is when it was last seen, in Unix seconds. `cold`: `true` when its ground is in the cold (the middle of the temperatures seen under 0.15: a snowy biome, a mountain's height), where water freezes; absent otherwise. Counts that are 0 are left out. |
| `health` | `what`, `ms` (optional), `detail` (optional) | A bot saw something wrong with itself, at that moment: `what` a short name (`slow round`: one of its rounds took long; `pressure`, `lag`, `ended`, or another), `ms` how long when it is a duration, `detail` a line for people (its parts' times, its task). Any bot may write it; this robot writes none yet. A reader shows it with the run's other health lines (a harness's), never guessing one. |

## A bot's mind

What a bot has in mind beside what it does — its tasks, its needs, its memory, the places it
knows, its timers — told in two lines a watcher draws without knowing the bot: the bot
describes its own panels (`mind-schema`), then sends their values (`mind`), each panel one of
a few kinds below. Any bot writing these events may write them; one that writes neither has no
mind to show. This robot writes none yet.

### `mind-schema` (event): the panels, once at the start

| Field | Type | Meaning |
|---|---|---|
| `bot` | string | The bot's name. |
| `version` | string | Its version (a commit, a tag): the panels are per version, so a reader takes them from each run, never from what it knew before. |
| `panels` | [panel] | In the order a reader shows them. |

A panel:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | Its key in `mind` (`backlog`). The same id in two versions is the same thing. |
| `title` | string | What a reader calls it (`the backlog`). |
| `kind` | string | `kv`, `table`, `list`, `tree`, `map`, `track` or `text` (below). |
| `columns` | [{`key`, `title`, `type`, `unit`}] | For a `table`: its columns, in order. `type` is `string`, `int`, `float`, `bool`, `ticks` (game ticks, shown as time) or `pos` (`[x, y, z]`, a place in the world); absent: `string`. `unit` is shown after a number (`s`, `blocks`). |
| `doc` | string | Where the panel is described: a URL, or a repo path with a line. |
| `every` | float | At most how often it is sent, in seconds. |

### `mind` (telemetry): the panels' values

| Field | Type | Meaning |
|---|---|---|
| `round` | int | The bot's own count of its decisions, when it has one. |
| `panels` | {id: value} | The panels that changed since the last `mind`, each whole: a value replaces the one before it (no patches). Every 30 s all of them (a keyframe), so a reader seeking into a run needs only the last keyframe and the lines after it. |

The values, by kind:

| Kind | Value | Meant to be shown as |
|---|---|---|
| `kv` | {key: value} | two columns; a value that is an object as compact JSON |
| `table` | [row], a row {column key: value} | a table of the schema's columns; a row with `"hl": true` stands out (the one on top) |
| `list` | [string] | lines |
| `tree` | {`label`, `info`, `children`: [tree]} | a tree to fold; `info` its details |
| `map` | {`boxes`: [{`id`, `min`, `max`, `kind`, `label`}], `points`: [{`id`, `at`, `kind`, `label`}], `lines`: [{`a`, `b`, `label`}]} | places in the world: `min`, `max`, `at` are `[x, y, z]` (a box's both corners inside it); `a` and `b` a point's or a box's id |
| `track` | a number or a short string | a lane along the run's time: a number as a line, a string as bands |
| `text` | string | as it is |

- **A kind a reader does not know** it shows as compact JSON, never as an error: a bot may send
  a new kind before a reader draws it. A new kind is added here when a bot needs it.
- **Fields missing are fine; fields unknown are ignored**, as everywhere in this file.
- **Size:** a `mind` line stays under 64 KB; a bot with a bigger panel sends it less often
  (`every`), not in parts.

## Compatibility

While the robot and its readers are developed together, compatibility is not kept for every
change: the owner may drop a field nobody reads within schema 1 — noted here.

- **2026-10-12:** `health` (event) added: a bot's own word that something went wrong with it.
- **2026-10-09:** `mind-schema` (event) and `mind` (telemetry) added: a bot's mind, its panels.
- **2026-10-08:** `screen` (telemetry) and `block_event` added, and `state.hotbar`, `selected`, `using`.
- **2026-10-08:** `tick` (telemetry) added, and `state.id`.
- **2026-10-06:** `view.front`, `frontDist`, `frontW`, `frontH` and `frontReach` (what it saw
  ahead, 9×10 rays) removed: no reader used them any more. Kept runs before that day have
  them.

- **Schema 1 holds** while names and fields keep their names, types and meanings.
- **Additive changes** (a new event name, a new field, a new grid) need no new schema.
  Readers must ignore what they do not know.
- **Breaking changes** (renaming, removing, changing a meaning or a unit) raise `schema` in the
  `start` event, and this file says what changed.
- **A missing field** (e.g. `day` before the clock is known, an empty `held`) is normal.

## Kept runs

mc26's end-to-end test keeps every robot run in its own directory, with the events and what was
run (`meta.json`: version, scenario, seed, commits). See mc26's `docs/testing.md`, "kept runs".
A reader that replays a run plays its `events.jsonl` with the gaps between `ts` values,
shortened as it likes.
