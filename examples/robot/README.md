# robot

A bot that plays the way a person with the vanilla client does. It ticks 20 times a second and
sends what the client sends: keys, movement, clicks with their predictions and the tick end. A
server cannot tell it from a person by its packets. Each thing it learns to do becomes a
command.

```
go run ./examples/robot -address localhost:25565 -name Robot
```

How it decides and why — every rule and the run that taught it — is in
[DESIGN.md](DESIGN.md); the course it plays in [SCENARIO.md](SCENARIO.md); what it reports in
[EVENTS.md](EVENTS.md); the robots to come (a client-like one that sees only what a player
sees, a human-like one with a fading memory and a sheet of paper) in [ROADMAP.md](ROADMAP.md).

It reads commands on stdin, one per line, and answers each on one line that starts with
`robot: `. That lets a person or a test drive it. Players can also command it in chat:
`robot get oak_log 3` in public chat, or `/msg Robot pos` as a whisper. It answers the same
way it was asked. The commands are listed in the package comment of `robot.go`
(`go doc ./examples/robot`).

## What it can do

| | |
|---|---|
| move | walk, sprint, jump, sneak at edges, swim, climb, fall, ride a water current — the client's own physics (`bot/physics`), so the server's movement checks accept every move; it stands on boats and shulkers as on blocks |
| find its way | A* over the places a player can be (walk, step and jump up, drop, gap jumps, ladders, water), walked by keys and looks, replanned when blocked; `goto`, `follow`, `come`; it explores when nothing it needs is in sight |
| see | blocks of every chunk and update, entities with their data and equipment, the inventory and every open menu, chat (player, whisper, system, disguised) |
| hands | dig in the ticks the game's tool rules give, place against the face it sees best, use blocks and items, eat, fight with charged attacks |
| screens | craft from the recipe book, move stacks with shift-clicks, smelt, trade with villagers, write signs; every click carries the right hash of every component of the stacks it changes, so the server sends nothing back |
| ride | minecarts (the server moves them) and boats, whose physics it runs itself, as the steering client must; it sits where the server seats it |
| live | sleep in a bed through the night, farm wheat (till, sow, bone meal, harvest), cross into the Nether through a portal it builds, and the End |
| plan | `get <item> [n]`: from the inventory, crafted (ingredients first, a crafting table placed when needed), or dug from the blocks whose loot table drops it; a recipe the book does not have yet comes from the game's recipes (`data/recipe` of go-mc26), whose ingredients unlock it on the server; `build hut` |

## How it is tested

The [mc26](https://github.com/mj41/mc26) pipeline builds go-mc26 for every supported Minecraft
version, assembles this kit against that build, and runs the robot against a vanilla server of
the same version. The server judges every step: positions, blocks, inventories, NBT, the time
of day. The server's log must also hold no complaint (`moved wrongly`, `moved too quickly`, a
kick). There are two scenarios:

- `robot`, on a flat world:
  - an obstacle course;
  - a maze;
  - digging, placing, crafting, smelting, a fight;
  - the goals (`get wooden_pickaxe`, `get cobblestone 25`, `build hut`);
  - 25 item stacks with every kind of component, clicked with no slot sent back;
  - one of every entity type;
  - the Nether and the End;
  - orders in chat;
  - farming, a bed;
  - a minecart, a boat with no move corrected by the server.
- `survival`, on ordinary terrain (its own server, a fixed seed, generated anew every run). The
  robot starts with nothing in a forest. It must get a wooden pickaxe and 25 cobblestone and
  build a hut. Its view of the terrain around it is judged block by block.

Each scenario was written to catch bugs, and they did. Among the ones found and fixed:

- A list of texts with plain strings in it lost those strings when decoded.
- The library's teleport answer carried no position on 26.3.
- The planner knew only the recipe book. A new player's book is all but empty.
- The robot dug through rock it could not see, which left its drops shut in.
- It sank beside the boat it had just stepped off.

## Its first days

[SCENARIO.md](SCENARIO.md) is the course the robot follows in a new survival world, in a normal
biome with trees and stone: a shelter dug into a hill on day 1, stairs and mines for iron at
night, a field on day 2, diamonds on night 2. It also lists the rules it keeps throughout:
tools, food, fights, water and lava, getting unstuck.

## Watching it

With `-events <file>` the robot appends what it does to a file of JSON lines: its state twice a
second, its view of the world every two seconds (and every block of a box round it, when they
changed), each command, plan line, hurt, meal and change
of the time of day. It does not know who reads them. [EVENTS.md](EVENTS.md) is the format, the
contract with whatever reads it: a viewer (a program of its own, not in this repository) can
follow the file and draw the world in 3D from the robot's eye, the land from above, its
inventory and its last events.

`mc26 e2e` keeps every robot run in its runs directory (`MC26_RUNS`), `<date>_<version>_<scenario>_<robot>/`: the
events, the robot's output and a `meta.json` of what ran (the world's seed, the mc26 and kit
commits). A viewer can show the newest live, and play any of them again, faster than it ran.
The same seed again (`MC26_SEED`) is the same world.

## Not yet

- **A week:** the `week` scenario has run to day 9, but each run still has deaths; a survived
  week is the goal. Food (a watered field early enough) and archers are what kill it most.
- **Water far from home:** a world with no water near its home (the test seed's nearest river
  is 185 blocks off) makes the field slow: the search finds it, the walks are long.
- **Getting stuck:** it can still stand somewhere a long time; the harness stops it after three
  minutes, and a relog fixes the server putting it back.
- **Physics:** a few rare cases, listed in `bot/physics`' package comment.
