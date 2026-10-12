# Determinism of a game in simworld: knowing when the bot is done

A game is the server's tick and the client's tick, stepped from one loop. It is deterministic when
what the client does between two ticks does not depend on the machine's speed or on which of its
goroutines the scheduler ran first. Today that holds for 19 runs in 20 (`make simdet`: 3 of 20
creeper runs and 1 of 20 stage runs differ).

## What can differ between two runs

1. **When is the client done?** After each tick the game waits till the client's goroutines have
   "nothing to run" (`settle`). It reads the goroutines' states from `runtime.Stack` and calls the
   client quiet when two snapshots in a row show nothing running or runnable. That is a wall-time
   heuristic on a moment: a path search that is runnable between two snapshots, a goroutine just
   woken by something not yet scheduled, a syscall that returns later — each makes a quiet that
   isn't. It has been patched three times (the brain's table count, `Summon` not syncing, a
   syscall counted as still working) and what is left is the part a heuristic can't do.
2. **In what order do goroutines that are woken together run?** A tick's end wakes every job
   waiting on the clock at once; two of them touching the body's shared state race. The stepped
   clock already fires its *timers* one at a time (a settle after each), but not the waiters of a tick.
3. **Waits on the wall clock** in the client: `time.After(5s)` in the body's job end, a read
   deadline, a `time.Sleep`. They fire, or not, by the machine's speed.
4. **Real sockets.** The bot and the game talk over loopback TCP: when a packet is read is up to
   the kernel and the netpoller.

## Two ways to know "done"

**A. The client declares it** (the bot offers a counter: a job reports "parked on the game's clock").
Every blocking point of every client goroutine must be annotated — a channel, a mutex, a
callback, the controller, the walker, the brain's round, the events writer, any library the client
calls. One missed point is a silent nondeterminism that shows 1 run in 20, which is where we are.
It also asks every client — the robot, a test's own bot — to carry the annotations, and its
library API would grow a notion of "game" that a real server has no use for.

**B. The runtime tells.** Go 1.25's `testing/synctest` runs a test in a *bubble*: every goroutine it
starts is in it, `time` in it is virtual, and `synctest.Wait()` returns when every goroutine of the
bubble is **durably blocked** — on a channel, mutex, `sync.Cond`, `WaitGroup`, a `select`, a
virtual-time sleep or timer, all made in the bubble — and so can only be woken by another goroutine
of the bubble or by time passing. It is exactly "the client has nothing to run", observed
by the runtime, not guessed from stack text, with no annotation by the client. What it does not
count as durable: a goroutine blocked on a real file or socket (I checked: a loopback TCP read
holds `Wait` for ever), and one in a syscall — those run or may run.

B is the design. A client says nothing; the library's API changes by one optional field.

## The design

**1. A transport inside the bubble.** The bot and the game join over an in-memory connection:
`simworld/memnet` — a `net.Conn` pair with unbounded buffers (not `net.Pipe`, which is unbuffered:
the server sends a batch of chunks while the client writes a tick end, and two unbuffered writers
deadlock), made of a mutex and a `sync.Cond` or channels, so a goroutine reading from it is durably
blocked. The client joins with a `JoinOptions.MCDialer` of simworld's — the kit already has that
interface (`mcnet.MCDialer`) — and the server accepts from `&mcnet.Listener{Listener: memListener}`.
No library API changes; a real server's path is untouched. Deadlines (`SetDeadline`) are supported
with the bubble's virtual timers, so the keep-alive deadline works as it does on a real socket
and the `KeepAliveTimeout = -1` special case could go.

**2. `settle` is `synctest.Wait()`** in a bubble, and the stack heuristic otherwise — the fallback
for a game outside a bubble (a test that has not moved yet, an old caller): `settle` probes whether the
caller is in a bubble (a `Wait` outside one panics; recovered once, cached) and chooses. A client that
"never says" is a client the runtime sees anyway; one that never blocks (spins) holds `Wait` — the test
times out, as a hung client does today, and `-timeout` names the goroutine.

**3. A game runs in a bubble: `simworld.Run(t, func(t *testing.T))`** is `synctest.Test` plus what the game
needs (a cleanup that closes the client and the server before the bubble ends: goroutines left
blocked when it ends are a deadlock panic, which is the leak check a game wants). `Start` and
`NewServer` work in or out of a bubble; a test moves by wrapping its body in `Run`, one at a time.
Virtual time also makes `Stepped` optional: the controller can tick on the bubble's clock; the stepped
clock stays (it is what ticks the client from `Step`) and its virtual `time.Now` is the bubble's.

**4. Order of simultaneous wakes.** `Stepped` wakes the waiters of a tick (WaitTicks) one at a time, in the order they
began to wait, with a `Wait` between, as it does its timers: one registry of waiters on the clock instead of a
broadcast. Where two goroutines are woken by something else (a channel the client itself closes), the order is
the scheduler's; for that a game may set `GOMAXPROCS(1)` for the length of the bubble (`Options.Serial`): with one
P the run queue is FIFO and `runnext` is deterministic, so the same wakes run in the same order — except where
the runtime preempts a goroutine that has run 10 ms (a path search): the one place left, named and measured by `simdet`.

**5. Wall waits** in the client (the body's `time.After(5s)`, a `time.Sleep`) become virtual in a bubble and
need no change: they fire when everything else is blocked, in order, and never by machine speed. Outside a bubble
they stay as they are.

## What is not decided here (for the bot session's review)

- The events writer's file syscalls: `Wait` does not wait for a goroutine in a syscall. The sim body
  writes events to a file; a buffered or in-memory writer for tests takes the syscall out. (Or `Wait` and
  then a settle of the heuristic for syscalls only.)
- A client goroutine blocked on `os.Stdin`, a real file, or a real network (the robot's console reads
  stdin) is not durable: the test must not start it (the harness of `simrobot_test.go` does not).
- `GOMAXPROCS(1)` is global; games run one at a time per process (they do: `go test` runs a package's
  tests in order unless `t.Parallel`).

## Plan and test

1. `memnet` (conn, listener, dialer) and a join through it in `fakeserver`, a test that a bot joins and ticks.
2. `settle` with `synctest.Wait`, `simworld.Run`; the kit's simworld tests moved in to it; each must pass as before.
3. A downstream bot's test harness (its own `simBody`) and the robot's `simRobot` through `Run`: their stages 20 runs each.
4. `Stepped`'s waiter registry and `Options.Serial`.
5. The test is `make simdet` (20 runs) with no run differing, creeper included; the number measured before
   (3/20, 1/20) and after is in the commit.

## Result (2026-10-12)

Built as steps 1–3 (memnet, `StartMem`/`Join`, `Run`, `settle` = `Wait` in a bubble; a bot built on
the kit joins its test harness with `Game.Join` and runs each game of its two determinism tests in `Run`):

| test (20 runs each) | before (the stack heuristic) | in a bubble |
|---|---|---|
| creeper by the tree (`TestSimMobGamesAreDeterministic`) | 3 runs differing | 20 identical, 23 s |
| the seven stages (`TestSimStagesAreDeterministic`) | 1 run in 20 differing | 20 identical each, 148 s all |

A game takes about 1.2 s of wall time in a bubble (the creeper's) — the loop no longer sleeps or spins to
decide the bot is done. Points 4 (waiters woken one at a time, `GOMAXPROCS(1)`) were not needed for these
runs and are not built; the first run that differs in a bubble is the case for them. Answers from that bot's
review: its sim body starts no goroutine that blocks on a file, stdin or the network; its events
writer is a short syscall in the caller's goroutine; a respawn that waited with no context was the one leak, since fixed there.
