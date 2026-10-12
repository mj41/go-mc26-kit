# mcping

Asks a Minecraft: Java Edition server for its entry in the server list, as the multiplayer
screen does, and prints it: the version, the description, the round trip, the players.

```
go run ./examples/mcping localhost:25565
go run ./examples/mcping -f icon.png localhost      # also saves the server's icon
```

The port defaults to 25565. A server of any version answers; the version it reports is its own.
