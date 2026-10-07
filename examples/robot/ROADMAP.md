# The robots to come

This robot (`examples/robot`) is the first of three kinds. Each later kind knows less, and so
plays more like a person. The game's rules stay the same for all three: physics, digging,
fighting and the server's corrections. What changes is what the robot may know and how long
it remembers it.

## 1. The server's-data robot (now)

It uses everything the server sends a client:
- every block of every loaded chunk, through walls and under the ground;
- every entity's data;
- the registries, the recipes and the light.

It remembers all of it exactly, for as long as it likes (`memory.go`: home, stairs, fields,
water, a map of 16×16 areas). It finds ore through the rock and water 150 blocks off. See
`DESIGN.md`.

Its job: to show that the protocol library (go-mc26) and the client kit work, and to find
their bugs. It plays a whole week of survival through every system: physics, digging,
crafting, smelting, farming, fighting, trading and the advancements. And it shows the game
from a robot's eye in the events (`EVENTS.md`) for a viewer.

## 2. The client-like robot (next)

It may use only what a real client would put on the screen:
- what is in its field of view and in line of sight, and lit enough to see;
- the sounds a player would hear;
- the chat and the screens (inventory, a furnace, a villager's trades);
- the HUD: health, food, the hotbar, the time of day as the sky shows it.

It may not see ore inside the rock, a cave behind a wall, or a mob behind a hill. It finds out
the way a player does: by looking round, going there and digging. The protocol data still
arrives, since the client gets it, but the robot reads it through a "what would be drawn"
filter: a view frustum, occlusion by opaque blocks, and the light.

What it will need:
- a renderer-like visibility pass over the chunk data (opaque blocks, light, the field of
  view);
- a memory built only from what it saw;
- search by looking round (turning its head), not by asking the world.

## 3. The human-like player (after that)

It plays like a person who has been playing for a few weeks:
- **No infinite memory.** A human-like brain: what it saw fades unless it matters or is
  repeated. The way home is known; a cave seen once three days ago is half forgotten.
- **One sheet of paper and a pen.** A small notebook it chooses to write in (the home's
  coordinates, the water's, a note of where the iron was), with a limited size. It reads the
  sheet back when its memory fails. The paper is a real item in its inventory: lost when it
  dies.
- **Attention.** It notices what a person notices: movement, sound, the unusual.
- **Mistakes.** It sometimes misjudges a jump, forgets to eat or gets lost, and recovers
  as a person does.

## Parameters: Westworld-style settings

In the series *Westworld*, a host's personality is a set of attributes shown on a tablet. A
technician moves a slider (bulk apperception, candour, vivacity, aggression, …) and the
host behaves differently. The later robots should have such a panel too: the same brain, many
characters.

To do: watch the series again and write down every attribute shown on the tablets, with what
it does to the host. Then map each to the robot's decisions. Possible first sliders, from
this robot's rules:
- **courage:** the health at which it retreats, and whether it fights an archer or walls itself
  in;
- **curiosity:** how far it explores, and how often it looks into a cave;
- **patience:** how long it waits for the morning or for a mob to leave;
- **caution:** how close to lava, water or a drop it will dig;
- **diligence:** how many chores it does before resting;
- **memory:** for the human-like player, how fast its memory fades and how much it writes on
  the paper.

The settings belong in a file (`-character` or similar) and in the events' `start`, so a
kept run says who played.
