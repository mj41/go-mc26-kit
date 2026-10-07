# go-mc26-kit

A client, a server framework, the account flows and the examples on top of
[go-mc26](https://github.com/mj41/go-mc26), the Go library for Minecraft: Java Edition 26.x
generated from Mojang's unobfuscated server jars. The library is the protocol and the data as Go
types; this module is what a program does with them.

| package | what |
|---|---|
| `bot`, `bot/basic`, `bot/world`, `bot/msg`, `bot/playerlist`, `bot/screen` | a client: login, configuration, the event model, chunks, chat (who sent a message, what they typed, its chat type, disguised chat included), tab list, inventories and open menus of every type, clicks predicted as the client predicts them, with the hash of every component of the stacks they change |
| `bot/entities` | the entities the server shows: position (26.3's stepped moves included), rotation, data, equipment, motion, passengers; nearby by distance; where a passenger sits |
| `bot/physics` | the vanilla client's movement: keys to velocity, gravity, drag and block friction, collision with the block shapes and with the entities a player stands on (boats, shulkers), stepping up, sneaking at edges, swimming, climbing, water currents — what the server's movement checks accept; a boat's physics, for the client that steers it |
| `bot/path` | finding the way (A* over the places a player can be: walk, step and jump up, drop, gap jumps, ladders, water) and walking it by keys and looks, replanning when stuck or blocked; goto and follow |
| `bot/act` | the hands: digging as the client does (start, a tick's progress from the tool rules, efficiency, effects and attributes, the arm swing, stop), placing against the nearest face, using a block, eating, attacking when the attack is charged; in screens: crafting from the recipe book, shift-clicking stacks between a container and the inventory, smelting; the prediction sequence; an item's effective components |
| `bot/recipes` | the recipe book the server shows: every unlocked recipe by its display id, with what it makes and what its ingredients take |
| `bot/advancements`, `bot/clock` | the advancements as the client tracks them (each told once it is done, with its title), and the game's clock: the time of day and its phases |
| `bot/ride` | the player as a passenger: seated where its vehicle carries it, a boat it steers moved by its own physics and sent to the server every tick, the server's corrections taken |
| `bot/control` | the client's tick: every 50 ms what the vanilla client sends for its player (keys, movement packets, tick end), the player-loaded notice, teleport and rotation answers |
| `server`, `server/auth`, `server/command` | a server framework: list ping, login, configuration, command graph, player list |
| `yggdrasil`, `microsoft`, `offline` | Mojang session server, Microsoft login, offline uuids |
| `examples/*` | small programs: a bot with a console (`daze`), an auto-fisher, a server-list ping, a region-file dumper, a player-data converter, a pressure test, a Microsoft login, a minimal bot, and `robot`, the bot that plays the way a person with the vanilla client does: it moves, digs, places, crafts, fights, rides, sleeps, farms, takes orders in chat and pursues goals (`get <item>`, `build hut`) on flat and ordinary terrain; mc26's end-to-end run judges it against a vanilla server ([examples/robot/README.md](examples/robot/README.md)) |

## One source, every supported Minecraft version

The library is one branch and one tag per Minecraft version (`v0.<YYN>.<patch>`). This module
has one branch, `main`, and tags of its own: its sources build against every supported library
version, which the workflow checks with a matrix. The version is chosen by the library
dependency, not by this module:

```
go get github.com/mj41/go-mc26@v0.263.1   # the Minecraft version
go get github.com/mj41/go-mc26-kit         # the kit, whichever tag is newest
```

`go.mod` here requires the oldest supported library version; a newer one in your `go.mod`
wins. Where a Minecraft version differs in something this module does (the login finished
packet gained a session id in 26.2), the code follows `version.ProtocolVersion` of the library
it was built with, so there is no per-version branch here.

```
go run ./examples/daze -address host:port -name Daze
go run ./examples/mcping host:port
```

This repository is the source: issues and changes go here. The pipeline of
[mc26](https://github.com/mj41/mc26), which generates the library, checks out this repository
next to itself and, for every Minecraft version it builds, assembles this module against that
build, runs its tests, and runs the bots of `examples/` against a vanilla server of that
version; the bot here is that pipeline's test client. The matrix in `.github/workflows/ci.yml`
lists the library versions this module builds against; a new library release is a line added
there.

## Notes

- Built with Claude Opus and Claude Fable. Carries code from
  [Tnze/go-mc](https://github.com/Tnze/go-mc) (MIT); `COPIED` lists the origin of every file,
  `LICENSE` keeps both copyright lines.
- No API compatibility promise (major version 0).
