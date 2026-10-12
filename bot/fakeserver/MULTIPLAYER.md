# More than one player on a fake server

Status: **phase 1 built** 2026-10-12 (players, the shared world, chat, join and leave; tests in
`players_test.go`); phase 2 (movement) open. Before it, a `fakeserver.Server` held one player: one
connection, one list of what it sent, one `joined` and `left` signal. A second client that joins
replaces the first's connection and closes `joined` a second time, which panics. That blocks
`examples/pressureTest` (many clients log in) and every test of bots that meet: two bots that chat,
a bot that sees another arrive, a bot that follows a player.

## What changes

**Per connection, a `Player`**, made when a client reaches the play state:

- its name and uuid (the offline uuid of the name), its entity id (1, 2, 3 … in the order players
  joined: `play.Login.PlayerID`), and its connection;
- what it sent (`Got`, `WaitFor`, `Said`) and its own `Hook`;
- its `joined` and `left` signals, and its position: where it was put (`Spawn`), then where its
  move packets say it went.

**On the server, the players in join order:** `Players()`, `Player(name)`, and
`WaitPlayers(n, timeout)`, the many-player `Joined`. `MaxPlayers` (default 1) is the server's
capacity, set before the first join. A client past it is refused at login with the vanilla
"server is full" reason, not left to break the first player's state.

**Single, shared by all players:** the `World` (its blocks), `Digs`, and `SetBlock`. A block that
changes is told to every player, as a server tells every player in view. `Broadcast(p)` sends one
packet to all (a time of day, a weather change).

**Told to the others, as a vanilla server tells them.** The default is on when `MaxPlayers` > 1,
and a field turns it off.

| event | to the player who did it | to every other player |
|---|---|---|
| joins | the player-info entries of everyone present, and an entity spawn (`AddEntity`, type player) for each of them at their position | its player-info entry (`PlayerInfoUpdate`: add, listed), and its entity spawn |
| sends chat (`ServerboundChat`) | the message back as `PlayerChat`, from itself (unsigned, inline chat type "<name> text", as `Chat` makes it) | the same `PlayerChat` |
| moves | nothing (no correction: the fake server checks no movement) | phase 2 only: its entity moved (`EntityPositionSync`) |
| leaves | — | `PlayerInfoRemove` and `RemoveEntities` |

## Compatibility

The single-player API keeps its meaning, so no test and no simworld game changes. `Send`, `Got`,
`WaitFor`, `Said`, `Kick`, `Joined` and `Hook` on the Server act on the first player, "the
player". `Packet` gains `From`, the name of the player who sent it, so a `Hook` on the server sees
every player's packets and knows whose each is. simworld stays one player for now. Its clock and
its settle step one client; a game of several bots is a design of its own, made when a scenario
needs it. The bubble already sees every client's goroutines, so `synctest.Wait` serves several
clients as it serves one.

## Two clients in one bubble

```go
synctest.Test(t, func(t *testing.T) {
	s := fakeserver.StartMem(t)
	s.MaxPlayers = 2
	a := fakeserver.Join(t, s, "Alice") // the kit's bot, or a program's run with s.Dialer
	b := fakeserver.Join(t, s, "Bob")
	if err := s.WaitPlayers(2, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	synctest.Wait()                    // both in, each told of the other
	// … Alice says hi in chat: Bob's msg.PlayerChat gets it, from Alice
	said, _ := s.Player("Alice").Said(ctx, 1)
	_ = said
	_, _ = a, b
})
```

`pressureTest` becomes `run(ctx, address, n, opts)`. Its test logs in 3 clients in a bubble and
checks each one's game start and the server's 3 players; the program keeps its default of 1,023
against a real server.

## Choices, and the recommendation

1. **One fake server per bot, nothing shared.** It is free today, but bots that do not see each
   other test nothing about meeting, and pressureTest's point, many logins to one server, is
   lost.
2. **Players, the shared world, chat and presence (join, leave): phase 1.** What a vanilla server
   tells other players of a player arriving, talking and leaving. It is enough for pressureTest,
   for bots that chat, and for a bot that sees a player arrive and leave.
3. **Phase 1 and movement (phase 2).** Each move packet re-sent to the others as an entity
   position, so that a bot can follow another. It means a moves-to-entity translation per
   packet kind, and with no physics on the server, a client's claim is what the others see.

**Recommended: 2, phase 1 now; phase 2 when a scenario needs a bot to follow another.** The
single-player API stays as it is, so the existing tests are the regression check. The new
behaviour gets tests of its own: two clients chat, a third is refused at `MaxPlayers`, a leave is
seen by the others, and pressureTest with three clients.
