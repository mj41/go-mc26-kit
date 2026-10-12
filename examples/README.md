# The examples, and the tests that keep them working

Every example in this module is something a user copies, so every example is run by `go test ./...`:
it joins a server, does what it says it does, and ends. An example that stops working fails the
module's tests, in the same commit that broke it. This page is the design, and the list of what
each example is tested against.

Why: in a review of these docs (2026-10-12), both godoc examples of package `bot` were wrong. One
left out the player that answers keep-alives, so the server dropped the client. The other logged
in with an e-mail and password Mojang has retired, and set event fields that do not exist, inside
a comment the compiler never saw. Nothing failed, because nothing ran them.

## The servers an example runs against

- **`bot/fakeserver`** is a server stand-in in the test's process: login, configuration, a flat
  world round the spawn, and whatever packets the test sends (`Send`). It records what the client
  sends (`Got`, `WaitFor`). `StartMem` serves it in memory for a test in a
  [testing/synctest](https://pkg.go.dev/testing/synctest) bubble. Time in a bubble is virtual,
  so an example's waits and ticks take no time, and a run is the same every time.
  - A new `fakeserver.Listen` serves it on loopback with no `testing.T`, closed with `Close`, for
    godoc examples, which have no `testing.T`. It is net/http's `httptest.NewServer` for
    Minecraft, and it is also what a user tests their own bot against.
  - Two new helpers, `Chat(from, text)` and `Kick(reason)`, tell the player what a player said
    (the player-info entry first, then the message, as a server does) and kick it.
- **`bot/simworld`** is a game: the server's ticks and the client's ticks, stepped together, with
  blocks, mobs, damage and menus. It is for an example that plays, such as walking or digging.
  `simworld.Run` runs one in a bubble.

## How an example becomes a test

- **A program** (`examples/<name>`, package `main`): `main` keeps the flags and the signal handling
  and calls `run(ctx, …, bot.JoinOptions{})`, which is the example's own code. A test in the same
  package calls `run` in a bubble, with the fake server's in-memory dialer in the `JoinOptions`.
  The test drives the server: chat, a kick, a console line. It then checks the program's output
  and what the program sent. The only change the reader of the example sees is
  `JoinServerWithOptions(address, opts)` in place of `JoinServer(address)`.
- **A godoc example** (`Example…` in a package): it runs against `fakeserver.Listen`, which it
  starts in plain view, ends by itself (it leaves the game, or the server kicks it), and has an
  `// Output:` line. `go test` runs it and compares the output. Godoc examples are outside a
  bubble (an `Example` function has no `testing.T`) and run on loopback in real time, a fraction
  of a second each.
- **Not runnable here**: an example that needs a Microsoft account, or the network, is compiled by
  `go vet` and `go test` but has no `// Output:`, and its comment says why. No example keeps code
  in a comment: code the compiler does not see is code that rots.

## The examples

| example | what it does | tested by |
|---|---|---|
| `minimal` | joins offline, prints the chat, answers "hi", leaves on Ctrl-C or a kick | a test of `run` in a bubble: the fake server's chat "hi" from Alice is printed and answered with "hello Alice"; a kick ends `run` with the reason; a cancelled context ends it cleanly |
| `daze` | a bot with a console: chat in, chat out, chunks, inventory, respawn | a test of `run` in a bubble: a console line is sent as chat, a player's message is printed, a kick ends it. mc26's end-to-end run also plays it against a vanilla server |
| `bot`: `ExampleClient_JoinServer_offline` | joins offline | a godoc example against `fakeserver.Listen`, `// Output:` joined, then the kick |
| `bot`: `ExamplePingAndList` | the server-list ping | a godoc example against `fakeserver.Listen`, `// Output:` the server's description |
| `bot`: `ExampleClient_JoinServer_online` | logs in a Microsoft account and joins | compiled, not run: it needs an account |
| `mcping` | pings a server, prints its status | a test of `run` against the fake server's list ping |
| `microsoft` | logs in a Microsoft account | compiled, not run: it needs an account |
| `mcadump` | unpacks a region file into chunk files and packs them back | a test on a region the library's `save` package writes: unpacked and packed again, the sector byte for byte; `-x` gives the chunk's NBT |
| `playerdataconvert` | moves an offline server's players to their online uuids | a test on a save the `save` package writes (a player file, a tamed wolf, advancements, all under the offline uuid), the Mojang lookup given by the test: each under the online uuid after |
| `pressureTest` | logs many clients in, each again after it is out | a test of `run` in a bubble: three clients on a fake server that takes three (`MaxPlayers`), each logged in and at its game start, all out when the context ends; a fourth refused as the server is full |
| `autofish` | fishes: casts, reels in at a bite, casts again, recasts after 45 s with no bite | a test of `run` in a bubble on a fake server that plays the fishing (`Fishing`, bot/fakeserver/FISHING.md): three fish reeled in; and with nothing biting, the 45 s recast reels in and casts again |
| `robot` | plays like a person | its own tests in `simworld` (`simrobot_test.go`), and mc26's end-to-end scenarios |

The kit's tests build against every supported library version (the matrix in
`.github/workflows/ci.yml`), so an example is tested on each of them.

## The order of the work

All done on 2026-10-12: every example in the table runs as a test, but `microsoft` and the online godoc example, which need an account.

1. `fakeserver`: `Listen` and `Close` without a `testing.T`; `Chat` and `Kick`.
2. `bot`'s godoc examples, run with `// Output:`.
3. `minimal`: `run`, and its test.
4. `daze`: `run` (the console an `io.Reader`), and its test.
5. `mcping`: `run`, and its test. Its README, still the upstream project's install lines, is
   rewritten.
6. Later: `mcadump` and `playerdataconvert` on written files; `autofish` and `pressureTest` once
   the servers can play them.
