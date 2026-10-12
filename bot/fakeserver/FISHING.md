# Fishing on the fake server: as much as examples/autofish needs

Status: **built** 2026-10-12 (`fishing.go`; `examples/autofish/autofish_test.go`). Found on the way:
the fake server sent no keep-alives, so a test longer than the client's 20 s timeout lost its
connection; `Server.KeepAlive` sends them, as a vanilla server does every 15 s.

## What autofish does, and what it was found doing

`examples/autofish` casts with `UseItem` and waits for one packet, the splash sound
(`entity.fishing_bobber.splash`) of a bite. It then reels in with `UseItem`, casts again, and recasts
after 45 s with no splash. It never looks at the bobber entity or at what it caught.

It compares the sound against the registry id **609**, an id from an older Minecraft: the splash is 638
in 26.1, 640 in 26.2 and 644 in 26.3. So on every supported version the bite is never recognised, and
the bot only recasts every 45 s. mc26's end-to-end run only checks that it logs in, so nothing noticed.
The fix is to look the sound up by name (`soundid`), not by number, and a test of the example is what
keeps it fixed.

## The model

The fishing is in a fake-server field, `Fishing *Fishing`; nil, as by default, means no fishing. The
player holds a rod, and nothing checks it: the fake server keeps no inventory.

- **Cast**: a `UseItem` with no bobber out. The server spawns the bobber (`AddEntity`, type
  `minecraft:fishing_bobber`, its data the player's entity id) a few blocks in front of the player, and
  starts the wait for a bite.
- **Bite**: after `BiteAfter` (default 5 s, the low end of the game's 5 to 30 s), the server sends
  the splash sound at the bobber (`Sound`, the holder's id the registry id + 1). The fish stays on for
  `Hooked` (default 1 s, the game's 20 to 40 ticks).
- **Reel**: a `UseItem` with the bobber out. The bobber is removed (`RemoveEntities`). Within `Hooked`
  of a bite, that is a **catch**: counted (`Caught()`) and told as the game tells a pickup, the slot's new
  stack (`ContainerSetSlot` of a cod). Before the bite, or after `Hooked`, it catches nothing.

The waits are timers on the clock: in a synctest bubble, virtual time, so a test's fishing takes no time
and runs the same each time. No server tick loop is added. Of the game's own fishing, nothing else is
modelled: no open water check, no lure or luck, no loot table (always a cod), no particles, no
bobber movement. About 80 lines in `bot/fakeserver`, beside `Chat` and `Kick`.

## The test, and autofish's change

- **autofish:** it becomes `run(ctx, address, name, opts)`, as minimal did. Its watchdog ends with the
  context instead of panicking on a write error, and the splash is matched by name.
- **The test:** in a bubble, the fake server with `Fishing` on, `run` going. It checks that after three
  bites `Caught()` is 3 and the output has three reels ("gra~"); then the context ends it. Breaking the
  match on purpose, back to 609, must fail the test.

## Recommendation

**Build it.** It is a small model (well under a page, about 80 lines), and the test it allows has
already found the reason to exist: an example that cannot fish on any version we support. The
alternative, autofish compiled only with the reason, would leave that example broken.
