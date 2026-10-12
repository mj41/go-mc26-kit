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
second). In a test run, a bot's walk back to its shelter went through a flooded cave with a roof over the
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
   ones neither beaten in both (`label`), a way is dropped when another costs no more and has spent no more
   air, the air counted in buckets of 33 ticks (`airBucket`, three blocks of swimming: inside the quarter the
   budget leaves, a way dropped for it has spent less than 33 ticks more than the one kept). Dry land has one
   label a place, as before: the code is the old search when there is no water.
   `backNearest` (the place nearest the goal a walk can come back from) is bounded by the same air.
5. **The air the walker has.** `Walker` counts it as the game does, a tick with the eyes in water one off, a tick
   out four on, from full at its first tick (`Walker.Air()`); a caller that knows the game's count (the entity
   data) sets `Graph.Air` and the walker takes that. Each search gets a copy of the graph with the air at that
   moment.
6. **A goal under water** (a block to dig on a lake's bed, a drowned item) leaves the body there with what is
   left of its air, and a replan from there is refused if the way up does not fit: it would drown where it
   stands. So a goal whose eyes are under water is accepted only when the way out fits too: the search, on
   reaching it, asks `NearestAir` for the cheapest way (in swim ticks) to a place with the head out, and the
   goal is a goal only if `used + Linger + out ≤ budget`. `Graph.Linger` is the ticks the body stays at the goal
   (the dig); 0 by default. A goal in a pocket of air needs no way out; a goal with a nearer way out than the
   way back (the far end of a passage open to a dry room) is judged by that one. The asking is once a place
   (`exits`), bounded to 5000 places; a goal it does not find air for within the budget is refused.
7. **When only a too-long way exists** the search ends with `NoPathError{Air: true}`: it asks once more with no limit
   on the air — at most 20,000 places (`airAskNodes`: a bot in a big flooded cave replans often, so the second
   search is bounded, and so the answer is "no" past it) — and says `Air` when that finds a way, so a goal that
   is shut in for another reason is not blamed on the air. The error says `no way to the goal: under water too
   long (the air does not last the way)`, the walk ends `failed: …` with it, and `NoPath()` has `Air` set: the
   caller (a bot's handler for a failed walk) can dig a way to air, build, or wait. The player stays on the dry side: the refusal
   comes before the first stroke.
8. **A way that grows longer than the air.** While the air is not full, each tick the walker counts the most air the
   next hundred nodes of the way spend (`airNeed`); when it is more than is left it plans again from where it
   is, which finds the way out to breathe or says `under water too long` (it does not leave the body a way to
   take: a replan that finds none is the caller's — a body under water with no way up has no walk that helps,
   and `NearestAir` says where the air is).

## For a brain that goes up for air

A bot that swims up when its air is low (a rule such as "air below 200 ticks") must not do it
against a plan that counted on the water: a 15-block swim planned at full air needs 165 ticks and leaves 135,
below that threshold from the 9th block on, and a body that goes up then goes into the roof the plan counted on.
Two calls serve it:

- `Walker.AirNeed() (ticks, walking)`: the air the way on of the walk in hand needs before it is done — the most
  the swim spends over what the places to breathe on it give back, as the search counts — and whether a walk with
  a path is in progress. 0 with no water over the head on the way. A brain's need is `air < AirNeed + margin`
  while a walk is in progress, its usual threshold otherwise; `Walker.Air()` is the air the walker counts.
- `Graph.NearestAir(from, air) (Node, ticks, ok)`: the nearest place to breathe from a node, by swim ticks, within
  `air` ticks: a pocket ahead may be nearer than where the body last breathed. A node that breathes is its own
  (0 ticks). It uses the graph's own steps, so a roof, a wall or an `Avoid` is as the walk sees it.

## Cost

The labels multiply the search where there is water; they cost nothing on dry land. A search that finds no way
searches every place reachable (SEARCH.md above: ~64,000 places, ~4 s on a bench's two cores), and over water
those are labels. Measured on a laptop under `limited -cpus 2` (a failed search, a goal shut in stone far side
of a walled field, `maxNodes` 100,000; `reads` are the world's `BlockAt` calls, which are the work and do not
vary with the machine's load — the wall time of the same search varied between 0.5 and 6 s from run to run
here, so the times are the best of the runs and only the ratios are meant):

| world | main: places, reads, time | path-air: labels, reads, time |
| --- | --- | --- |
| dry field 100×100 | 13,732; 8.3 M; 0.5–1.6 s | 13,732; 8.7 M; 0.55–1.0 s |
| lake 60×60, 6 deep | 34,474; 47 M; 3.0–5.0 s | 54,252; 94 M; 7–8 s |
| ocean 100×100, 8 deep (at the limit) | 100,000; 161 M; 10–15 s | 104,469 (100,000 popped); 175 M; 12–15 s |
| flooded cave 60×40, 3 deep, roofed | 5,979; 4.2 M; 0.3–0.5 s | 4,666; 14 M; 1.1–1.3 s |

So over water about 1.6 labels a place (2 without the buckets: 68,836 in the lake), 2× the reads in the lake
(half of that the labels, the rest the ask of item 7). In a roofed cave, where every place is under water and
the air refuses the far ones, the labels fall (the budget prunes: 4,666 against 5,979 places) but the refusal
is asked once more with no limit (item 7): 3× the reads, 1.1–1.3 s against 0.3–0.5 s. The ocean is at the
limit on both: what bounds a failed search is `maxNodes` places popped, as before, so the wall time does not
grow past the limit's; the limit counts labels, so it covers fewer distinct places.

What bounds the rest: the ask in item 7 is at most 20,000 places (and one label a place: with no limit on the
air the air is not counted); the search for air from a goal (item 6) at most 5,000 places once per goal place;
`backNearest` at most `maxNodes` as before. A failed search that found no step to refuse for air asks nothing.
Not done: the ask does not reuse the first search's frontier.

The case that hung a test for over two minutes (the first version of `air_test.go`) was this cost, not a
loop: a sealed room on the testWorld's endless flat ground, five refused cases and two walker tests, each a
failed search that ran to its node limit on the open ground (50,000 / 100,000 places, 5–15 s on a loaded
machine), and each refusal for the air then asked (item 7) and had its `backNearest` made — three searches a
failure, where main made two. The tests wall the world in now (a stone ring), so a failed search has a few
thousand places; a bot's walk to a sealed goal on open ground costs what it did, ×1.2 for the ask, ×1.6 over water.

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
  mouth, keeps some air, ends with the eyes out of the water, and is refused the room 20 blocks on.
- `TestAirGoalUnderWaterNeedsTheWayOut`: a dead end 14 blocks long: a goal 6 in is planned; 12 in is refused with
  `Air` though the way in fits; with the far end open to a dry room it is planned; a body that stays 150 ticks
  (`Linger`) at the 6-in goal is refused; a goal in a pocket of air at the end needs no way out.
- `TestAirNearestAir`: from the middle of a long passage the nearest air is the pocket 4 blocks ahead, not the mouth
  17 back; none within less air than the way to it; a body that breathes is its own.
- `TestWalkerAirNeed`: 0 and not walking before the walk; about the passage's swim while it is on; 0 after.
