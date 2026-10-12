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

# Breath: the air a walk through water costs

## The defect

The graph plans a swim through any number of cells with the head under water. A body's air is 300 ticks (15 s),
one lost a tick while the eyes are in water, four given back a tick out of it; at none it drowns (2 health a
second). A bot's walk "back up to the shelter" (bench run #110) went through a flooded cave with a roof over the
water: at 1.84 blocks a second (11 ticks a block) the 15 s are 27 blocks one way; the run was longer, with no
air on it, and the bot sank and drowned.

## Design

1. **The state in the search, not a per-node rule.** Whether a run of flooded cells is too long depends on the
   run, which no rule about one node sees (every node of a long passage is the same as the nodes of a short
   one). A distance-to-air field (a node is allowed when air is within so many cells) was the other choice: it
   is a rule about one node and keeps a way back, but a passage with a shaft to the surface every few blocks
   passes it for any length while the path itself never goes to the shaft and drowns. So the search carries
   the air spent: a way to a place is `(cost, used)`, `used` in ticks.
2. **`used` along an edge** (`airUse`). A step that starts or ends with the eyes in water (`submerged`: the cell
   of the eyes, `floor + 1.62`, is water; a body swimming at the surface, head out, breathes) adds its swim time
   — 11 ticks a block, a diagonal as long as it is (the walker's swim measured 10.3 along a passage, 5.5 up a
   shaft: the 11 has the margin). A step between two places with the head out takes four ticks of `used` off
   for each tick it lasts, to none: a pocket of air on the way, a bank, is a place to breathe. A step that would
   take `used` over the **budget** is not an edge.
3. **The budget** is three quarters of the air the walk starts with (`Graph.Air`, in ticks; unset: full, 300). The
   quarter is the margin for a swim that goes slower than counted, a current or a mob's push, a plan made
   again from the middle of the water; being in proportion, a body with little air left may still swim the few
   blocks up to breathe (40 ticks of air: 30 ticks, two blocks). The start is the walk's: a plan from the
   middle of the water is made with the air at that moment.
4. **The labels.** A place is reached by several ways that differ in cost and air spent; the search keeps the
   ones neither beaten in both (`label`), a way is dropped when another costs no more and has spent no more.
   Dry land has one label a place, as before: the code is the old search when there is no water.
   `backNearest` (the place nearest the goal a walk can come back from) is bounded by the same air.
5. **The air the walker has.** `Walker` counts it as the game does, a tick with the eyes in water one off, a tick
   out four on, from full at its first tick (`Walker.Air()`); a caller that knows the game's count (the entity
   data) sets `Graph.Air` and the walker takes that. Each search gets a copy of the graph with the air at that
   moment.
6. **When only a too-long way exists** the search ends with `NoPathError{Air: true}`: it asks once more with no limit
   on the air and says `Air` when that finds a way, so a goal that is shut in for another reason is not blamed on
   the air. The error says `no way to the goal: under water too long (the air does not last the way)`, the
   walk ends `failed: …` with it, and `NoPath()` has `Air` set: the caller (a bot's walkFailed) can dig a way
   to air, build, or wait. The player stays on the dry side: the refusal comes before the first stroke.
7. **A way that grows longer than the air.** While the air is below half, each tick the walker counts the most
   air the way on spends (`airNeed`); when it is more than is left it plans again from where it is, which
   finds the way out to breathe or says `under water too long` (it does not leave the body a way to take: a
   replan that finds none is the caller's — a body under water with no way up has no walk that helps).

## Not modelled

Respiration, water breathing, a conduit; bubble columns; a roof so low that the eyes are under a solid cell
(no body stands there). The swim time is one speed: a current, a slope, depth strider are not in it.

## Tests

- `TestAirLongPassageIsRefused` (the graph): a sealed room with a flooded passage the only way in. 8 and 18
  blocks with full air are planned; 40 is refused with `Air` and the reason in the message; with the air at
  150 the 18 is refused, the 8 planned; with 60 even the 8.
- `TestAirNoWayThatIsNotTheAir`: the passage shut with stone says no more than "no way".
- `TestAirPocketIsBreathed`: 15 flooded, 6 with the head out, 15 flooded is planned; 36 flooded is not.
- `TestWalkThroughAFloodedPassage` (walker, physics): 12 blocks swum, air to spare (≥100); 40 refused before the
  first stroke, the player on dry ground with all its air.
- `TestWalkUnderWaterWithLittleAir`: a body 8 blocks into a passage with 150 ticks of air walks out by the near
  mouth, keeps some air, and is refused the room 20 blocks on.
