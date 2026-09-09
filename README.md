# go-mc26-kit

A client, a server framework, the account flows and the examples on top of
[go-mc26](https://github.com/mj41/go-mc26), the Go library for Minecraft: Java Edition 26.x
generated from Mojang's unobfuscated server jars. The library is the protocol and the data as Go
types; this module is what a program does with them.

| package | what |
|---|---|
| `bot`, `bot/basic`, `bot/world`, `bot/msg`, `bot/playerlist`, `bot/screen` | a client: login, configuration, the event model, chunks, chat, tab list, inventories |
| `server`, `server/auth`, `server/command` | a server framework: list ping, login, configuration, command graph, player list |
| `yggdrasil`, `microsoft`, `offline` | Mojang session server, Microsoft login, offline uuids |
| `examples/*` | small programs: a bot with a console (`daze`), an auto-fisher, a server-list ping, a region-file dumper, a player-data converter, a pressure test, a Microsoft login, a minimal bot |

## One source, every supported Minecraft version

The library is one branch and one tag per Minecraft version (`v0.<YYN>.<patch>`). This module
has one branch, `main`, and tags of its own: its sources build against every supported library
version, which the workflow checks with a matrix. The version is chosen by the library
dependency, not by this module:

```
go get github.com/mj41/go-mc26@v0.263.0   # the Minecraft version
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
