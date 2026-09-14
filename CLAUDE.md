# CLAUDE.md — Storming Aurora Battlesnake

Project constitution. Read fully at the start of every session. Everything here was verified
against the official Go engine source (`BattlesnakeOfficial/rules`) or the event handbook.
Do not contradict it from memory.

Repo: `https://github.com/Rigur-Calypso/seds-hackathon-Rigur` (private)
Language: **Go**. Dev machine: Mac M3 (arm64). Hosting: Render free tier.

---

## 1. Objective

| Stage | Ruleset | Board | Snakes | Scoring | Posture |
|---|---|---|---|---|---|
| Qualifying (1–2 rounds) | `standard` | 11×11 | 4 | Placement 10/6/3/1, summed | **Risk-averse** |
| Bracket | `royale` | 11×11 | 4 | Single elim, **best of 1** | **Controlled aggression** |
| Grand final | `royale` | 19×19 | 2 | Win only | **Aggressive, deep search** |
| Side event (optional) | `constrictor` | — | — | Separate leaderboard | Pure area control |

**The decisive arithmetic.** In qualifying a guaranteed 2nd is worth 6; a 50/50 between 1st
(10) and 4th (1) has EV 5.5. Holding 2nd, you need **>56% win probability** to justify risking
4th. With only 1–2 qualifying rounds, one 4th place is very expensive. In the bracket, 2nd is
worth zero and a single loss ends the run — so take concrete advantages, but never speculative
ones.

Stage is read off the game state, never guessed. See §4.

---

## 2. Verified engine rules

From `BattlesnakeOfficial/rules`. Getting any wrong is silently fatal. Never simplify these.
Every one gets a golden test.

**Turn pipeline, in order:**
```
GameOver → Movement → Starvation(-1) → HazardDamage → FeedSnakes → Elimination → [Royale: SpawnHazards]
```

**R1 — Reach condition is `health >= distance`, not `>`.**
Starvation runs before feeding; the out-of-health elimination check runs after. A snake
arriving on food at exactly 0 health survives, because feeding resets it to 100 first.

**R2 — Equal-length head-to-head eliminates BOTH.** Engine test is `len(me) <= len(them)`
loses. Strictly longer to win. Collisions are simultaneous.

**R3 — Heads are not body cells.** `snakeHasBodyCollided` skips index 0 of the other snake.
Head-on-head resolves under R2. Keep heads in a separate layer or you will forbid head-to-heads
you would win.

**R4 — The tail vacates unless duplicated.** Movement prepends a head and pops the tail every
turn; growth appends a *duplicate* of the last segment. So
`tailVacates(s) = len(s.Body) < 2 || s.Body[len-1] != s.Body[len-2]`.
Applies to **every** snake, ours and opponents.

**R5 — Food on a hazard square cancels that square's hazard damage entirely**, and also
restores health to 100. Food inside the storm is therefore *safer* than open storm, not more
dangerous.

**R6 — Hazard damage is applied before feeding and eliminates inline.** Consequence: entering a
hazard square that has **no** food can kill you this turn even if you were one step from food
elsewhere. This does **not** contradict R5 — R5 concerns the square you actually occupy.
Golden-test both cases separately.

**R7 — Multi-collision elimination is attributed longest-first**; collision eliminations are
collected then applied together, so mutual destruction in one tick is possible.

**R8 — Royale hazard is always a rectangular ring.** Hazards are cleared and fully regenerated
every turn from the complete sequence of shrinks:
```
numShrinks = (turn+1) / shrinkEveryNTurns
each shrink removes ONE line from ONE of four sides (minX++, maxX--, minY++, maxY--)
hazard = every cell outside [minX..maxX] × [minY..maxY]
```
The safe region is always an axis-aligned rectangle and only ever shrinks. At a shrink turn
there are exactly **four** possible next rectangles. **The direction is not in the request and
must never be predicted** — model all four outcomes. New hazards apply *after* this turn's
movement/eating/elimination resolution. Always treat the supplied hazard list as truth.

**R9 — Constrictor: no tail ever vacates.** Food cleared each turn, health pinned to 100, every
snake force-grown. R4 never fires. All body cells are permanent obstacles for planning.

**R10 — Settings come from the request, at these exact paths:**
```
game.timeout                                       // ms. USE THIS, never hardcode 500
game.ruleset.name                                  // "standard" | "royale" | "constrictor" | ...
game.ruleset.settings.hazardDamagePerTurn          // ROOT level
game.ruleset.settings.royale.shrinkEveryNTurns     // NESTED under "royale" — not root
game.ruleset.settings.foodSpawnChance              // root
game.ruleset.settings.minimumFood                  // root
```
`shrinkEveryNTurns` being nested is the easiest parser bug to ship. It fails silently as zero
and kills all shrink timing. Log every parsed value once per game in `/start`.

**R11 — `board.snakes[i].latency` is a string in every request** — the opponent's measured
response time. Free signal for spotting opponents near the timeout limit. Also
`snakes[i].length` is supplied directly; `snakes[i].id` is **game-scoped**, while
`snakes[i].name` is the stable cross-game handle.

---

## 3. Hard runtime constraints

- **Deadline from `game.timeout`**, not a constant. Budget `min(timeout − networkMargin, cpuCap)`.
- **Cooperative deadlines only. Never detach a goroutine that keeps computing after the response is sent.** Under concurrent games that CPU starves the other games. Every inner loop checks `ctx.Done()` / a monotonic deadline and returns before the handler does.
- **Concurrent games.** All per-game state keyed by `game.id`, mutex-guarded. Never a global "current game". Delete the entry in `/end`.
- **Under contention, degrade search first** — fewer opponent policies, then shallower depth. Never sacrifice the fallback path.
- **The fallback move is computed before any search** and returned on any parse, timer, or evaluation failure. Never 5xx. `recover()` in middleware so a panic cannot escape a handler.
- **Cold starts kill.** See `SETUP.md` §5.

---

## 4. Stage classification

`internal/stage/stage.go`, computed once per request:

```go
type Stage int
const ( Qualifying Stage = iota; Bracket; Final; Constrictor )

func Classify(g *GameState) Stage {
    switch {
    case strings.Contains(g.RulesetName, "constrictor"): return Constrictor
    case g.RulesetName == "royale" && g.Width >= 19:      return Final
    case g.RulesetName == "royale":                       return Bracket
    default:                                              return Qualifying
    }
}
```

Independently: `aliveCount == 2` enables duel logic regardless of stage — branching drops from
~81 to ~9, so depth 6–8 becomes reachable.

Start with **three** policy profiles (`qualifying`, `royale`, `duel`) plus a constrictor
override. Split further only when measurements demand it.

---

## 5. Decision pipeline

```
parse + validate (settings & deadline derived from the request)
  → legal action mask + guaranteed fallback (computed FIRST, ~1ms)
  → TVAE: exact simultaneous first-ply resolution
  → temporal Voronoi evaluation of each outcome
  → stage risk aggregator (CVaR / expected / maximin)
  → optional duel search (only when exactly 2 snakes live)
  → move + structured decision log
```

Never 5xx, never panic, always return one of the four move strings.

### 5.1 Engine v2 (post-event rework, DEVLOG §11)

The profile key `engine` selects the evaluator stage above. `"v2"` replaces TVAE + duel search with a
deep search on a fast state; TVAE remains the path for positions v2 cannot hold (> 8 snakes).

```
parse + validate → fallback (FIRST) → sim.State (make/unmake, exact rules R1–R12)
  → brain.Search: iterative-deepening alpha-beta over whole turns
       our move → joint reply of nearby opponents (paranoid), far opponents predicted
       leaves: temporal Voronoi heuristic (same terms as internal/eval)
  → move + structured decision log (depth, nodes)
```

Every rule in §2 still applies inside `internal/sim`, cited by ID. Any change to `sim.Make` re-runs
`TestMakeMatchesResolver` and the official differential in `tools/arena` — non-negotiable. The
stage risk postures of §6 are expressed through the evaluation weights and search flags per profile;
the search itself is paranoid at every stage.

---

## 6. TVAE — Temporal Voronoi Action Envelope

The core evaluator. Rather than flood-filling from the current position and hoping opponents
cooperate, resolve the **actual simultaneous first ply**.

For each of our legal moves, enumerate every joint action of the other live snakes. With four
snakes and ~3 legal moves each that is ~27 combinations per candidate, ~81 total — trivial.
Resolve each with the exact one-turn resolver (§2), then evaluate the resulting board.

For each surviving outcome build a **temporal Voronoi map**:

- time-aware reachable cells, including tail-release time (R4)
- health on arrival, including hazard cost and food reset (R5)
- **guaranteed** cells — we arrive strictly earlier than any opponent who could contest
- **contested** cells — equal arrival time or unresolved head-to-head risk
- **attack** cells — we can arrive simultaneously while strictly longer (R2)
- safe-exit count and cut-cell count

**Never assign an equal-arrival cell to a snake by index.** That is a systematic bias. Score
guaranteed area, discounted contested area, food feasibility, escape redundancy, and attack
opportunity as separate terms.

### Opponent model: ensemble, not database

Generate opponent actions from four cheap policies — survival-space, food-seeking, aggression,
legal-baseline — but **retain all legal actions in the adversarial envelope**. Never remove an
immediate legal threat because a profile says the opponent is passive.

Observed behaviour may later *reweight* the ensemble. Key observations by `snake.name`
(R11: `id` is game-scoped and useless across games). Treat them as **advisory only** — with
1–2 qualifying rounds there is very little data, so they adjust priors, never prune threats.

### Risk aggregation by stage

| Stage | Objective over the outcome envelope |
|---|---|
| Qualifying | **CVaR-25**: mean of the worst 25% of outcomes. A move with a forced-4th path loses to a safe alternative even if its average looks better |
| Bracket | Expected advancement, lower risk penalty, strong credit for *forced* eliminations and storm traps. Not speculative aggression |
| Final | Maximin / alpha-beta after the exact simultaneous first ply |
| Constrictor | Guaranteed area and mobility only; no food, starvation, or tail-vacate assumptions |

---

## 7. Design provenance

Every non-obvious choice comes from a snake that placed, or from verified engine source.
**All reimplemented from the idea — no code copied.** Several sources carry no licence at all
(all rights reserved) and two are GPL. See `CREDITS.md`.

| Decision | Source | Why |
|---|---|---|
| Time-aware occupancy (`freeAt[cell]`, not blocked/free) | snork (MIT) | Tail-chasing falls out of the fill |
| Health carried through the fill; cell owned only if health > 0 | snork | Merges starvation into area control |
| Food-on-path delays own-tail vacate | snork | Fixes a bug robosnake admits it never fixed |
| Bounded fill, stop at `k × length` | calvinl4, robosnake (MIT) | We only care whether a cavern exceeds us |
| Area as a **ratio**, never a count | coreyja, m-schier, bookworm (4 independent) | Scale-free; survives 11×11 → 19×19 |
| Terminal scores as an ordered type, not magic numbers | coreyja | `Lose < Tie < Scored < Win` for free |
| `Lose` ordered by (fewer opponents alive, then later) | coreyja | Placement-aware terminal utility |
| Hunger as lexicographic override, not a weighted term | coreyja | Removes the hardest tuning problem |
| Locality masking beyond `depth × 2`; alpha-beta at 2 live | m-schier (GPL — idea only) | Branching 81 → ~9 |
| Distant snakes forced to one move | bookworm | Simpler variant of the same |
| Death ranked: head-to-head > starved > self-collision | bookworm | If dying, take someone along |
| Kill reward weighted heavily | bookworm, snek-two, coreyja | Converts directly to placement |
| Size advantage decaying with turn | snork | Grow early, control space late |
| `sqrt(health)`, `spaceAdv^3` shapes | snork (HPO-tuned) | Not linear |
| One BFS outward from all food | TheApX/hungry (MIT) | Scores all four moves in one pass |
| Future-uncertainty discount `0.87^k` | calvinl4 | Death 8 plies out ≈ 0.43 of death now |
| Common random numbers in tuning | snork HPO | Halves games needed for significance |
| Cut-cell penalty (articulation points) | — | Space behind a narrow entrance is easily sealed |
| Metamorphic tests (rotate/reflect) | — | Catches coordinate and index-bias bugs fixtures miss |

**Anti-pattern:** `calvinl4` won RBC but only runs minimax when `numSnakes === 2`, falling back
to greedy otherwise. Do not copy that — qualifying is entirely 4-snake.

**Do not reference:** `JaxHodg/battlesnake`. Its README claims minimax and 4th place; the code
contains no minimax and does not import.

---

## 8. Licence boundary — mandatory

`BattlesnakeOfficial/rules` is **AGPL-3.0**. AGPL obligations trigger on distribution and on
network service.

- `tools/arena` **may** import it — local test tooling, never served, never distributed.
- `cmd/server` **must never** import it, directly or transitively.
- Keep the arena in its own Go module (`tools/arena/go.mod`) so the server binary provably cannot link it.
- CI check: `go list -deps ./cmd/server | grep BattlesnakeOfficial` must return **nothing**.

The handbook says you may be asked to show source. Clean-room reimplementation from documented
behaviour survives that conversation; pasted AGPL code does not.

---

## 9. Coding standards

- Go 1.21+. Idiomatic, clear, commented. Small packages with explicit interfaces.
- Every rule interaction carries a comment citing its ID: `// R4: tail vacates unless duplicated`.
- **Zero magic numbers in eval or search.** All tunables live in `internal/config`, loaded from `config/*.json`.
- Per-request objects immutable after parsing.
- No `math/rand` global. Seeded `*rand.Rand` only, so arena runs are reproducible.
- Structured logs only: game ID, turn, stage, chosen move, candidate scores, elapsed ms, fallback reason. Never log full payloads in production.
- Decision logic is a pure function so the arena can call it directly with no HTTP.

---

## 10. Git workflow — strict

- **Never `git checkout .`**, `git reset --hard`, or any command that discards unrelated work.
- One branch per step: `step-NN-short-name`. One PR per step. Squash-merge to `main`.
- Every PR body states: what changed, which gate it passes, and the measured result.
- After each merge: `git tag -f known-good && git push -f origin known-good`.
- Reverting a tested change means `git revert <sha>`, never wiping the tree.
- Commit prefixes: `feat:`, `fix:`, `test:`, `perf:`, `chore:`.

---

## 11. Testing rules

- Every rule R1–R11 gets a golden test before the code depending on it.
- **Differential tests against the official CLI** after every resolver change. Non-negotiable.
- **Metamorphic tests:** rotate/reflect a board and permute opponent order; the chosen move must transform correspondingly.
- Every real loss becomes a fixture in `testdata/fixtures/` with the correct move asserted. **Never delete a fixture.**
- Performance gate: p50/p95/**p99** decision time at 1 and 4 concurrent games, and again at a reduced timeout.

---

## 12. Discipline

- One change at a time, measured, promoted or reverted.
- Never refactor and change behaviour in the same commit.
- **Feature freeze at hour 10:45.** After that: operations and proven rule-defect fixes only.
