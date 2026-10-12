# The walker's search: not under the lock its readers take

## The defect

`Walker.Think` — the control tick's walking — held the walker's mutex through `plan`, the A* search. A search
that finds no way searches every place reachable: ~64,000 places, ~4 s on a bench's two cores. For those 4 s
every caller of the walker waited on the mutex: `Status()` (a bot's observer asks it every round), `Goto`,
`Follow`, `Stop`, `NoPath`; and the control tick itself, which sent no input, was inside `Think`. A bot whose
shelter walk failed every round saw rounds of 7.6–8.6 s, half of it the observer's look and half the mind's, and
the tick loop stalled the same time (survival run 20261012-011308 on the bench).

## Design

1. **The search runs on a goroutine of its own, outside the lock.** `Think` that needs a path (none, or the
   next nodes are no places to stand) starts one search with a copy of what it needs — the start node, the goal,
   the node limit — and returns at once, the player standing (no input; sneak held as the walker's `Sneak` says).
   It holds the mutex only for its own bookkeeping, microseconds.
2. **The result is published, not shared.** The search goroutine takes the mutex when it ends and leaves its
   result (the path, or the error with its `NoPathError`) tagged with the walk it was for (a generation number and
   the goal). The next `Think` takes the result: a path becomes the walk's path, an error ends the walk
   (`failed: …`, `NoPath()` set) — as before, one tick later than the plan was asked.
3. **A new `Goto`, `Follow` or `Stop` supersedes a running search** (a `Goto` for the goal already being walked keeps it:
   a caller that asks each round would otherwise never see a search end). Each bumps the generation and cancels the
   search's context; `Graph.FindContext` looks at it every 256 places and returns. A result of an older generation
   is dropped, never applied. A `Follow` whose target moved to another block asks for a new search the same way
   (the running one is cancelled; one for a goal no longer asked is never applied).
4. **`Status()` never waits on a search.** It takes the mutex, which no search holds.
5. **Not repeated while it cannot change.** A follow whose target is unreachable does not search again for 20
   ticks (a second); a `Goto` ends at the first failure and is the caller's to re-issue. A cache of "no way" per
   goal, invalidated by block changes near the frontier, is not built: the searches are no longer on the tick and
   the callers' own re-asking (the bot's, once per round) is the loop to change; measure again if a bot re-asks a
   failed goal every tick.

`Graph` is read by the search goroutine while the tick goroutine reads it (`climbing`, `inWater`): it holds no
state of its own besides the `Avoid` and `StillWater` the caller set, and reads the world through `World.BlockAt`,
which takes the world's read lock. An `Avoid` that takes a lock must not be one the tick goroutine holds while it
waits for the walker.

## A stepped game searches inline

A game a test steps from its loop (`control.Stepped`, `State.Stepped`) must be the same each time: a search on a
goroutine of its own would read the world while the server's packets change it, and the story would depend on
which came first. `Think` there makes the search itself and takes its result at once — the old behaviour, exactly:
the search takes no game time and nothing changes in the world while it runs. The background search is the real
clock's (and the unit tests of this package').

## Cost: one tick (a real clock)

A walk's first step comes one tick after `Goto` (the search ends before the next tick in a bubble — the runtime
waits for it — and within some ticks on a real clock), not in the same `Think`. In a bubble the order is
deterministic.

## Tests (lowest rung)

- `TestStatusDoesNotWaitOnASearch`: a walker asked for a goal while the world's reads are held (a blocked
  `BlockAt`), `Think` returns and `Status()` answers within a bounded virtual time.
- `TestGotoSupersedesASearch`: a second `Goto` while a search is held; releasing it applies the second walk's
  path and never the first's, and the cancelled search ends within a bounded number of reads.
- `TestNoPathIsPublishedByTheSearch`: an unreachable goal ends the walk `failed: …` with `NoPath()` set.
