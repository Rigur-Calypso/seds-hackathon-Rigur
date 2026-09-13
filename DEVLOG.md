# DEVLOG — everything that was done, why, and what was measured

This file documents every action taken on the project, every deviation from the planning
documents, and every original idea added beyond them. It is the "separate file" requested by
the owner. Newest material is appended at the end of each section.

---

## 1. Context and timeline

| Time (IST, 13 Sep 2026) | Event |
|---|---|
| 00:38 | Owner's initial commit: handbook, poster, `.gitignore` only |
| 05:59 | Work starts. Hackathon began 22:00 on 12 Sep and runs 12 h → ends 10:00; plan's feature freeze "hour 10:45" ≈ **08:45** |
| 06:00 | Read CLAUDE.md, BUILD_PLAN.md, KICKOFF.md, OPTIMIZATION_LOOP.md, SETUP.md, handbook (10 pages, image-only PDF rendered with PyMuPDF), poster |
| 06:05 | Verified CLAUDE.md §2 against engine source `BattlesnakeOfficial/rules@v1.2.3` in the local module cache (see §2) |
| 06:10–06:30 | Wrote Steps 0–4, 6, 7, 9 packages + tests |
| 06:30 | First full build; vet caught an import cycle in tests (fixed) |
| 06:34 | Four real CLI games (standard, royale 11×11, royale 19×19, constrictor) against the built server: all completed, zero WARN lines |
| 06:40 | Arena module + differential test written; constrictor clash diagnosed and fixed (§4.14) |
| 06:50 | PR #1 merged after green CI; `known-good` tagged (7305594) |
| 06:55–07:25 | Per-stage arena baselines, loss-bucket diagnosis, 9 paired experiments (§7), Copilot review triage (§5.5) |
| 07:25 | TVAE-first depth-gated duel search promoted; slow-CPU emulation and a 763-turn 19×19 CLI game verified; PR #2 |

Because only ~2h45m remained before freeze when work began, the plan's "one step per branch,
human verifies each gate" cadence was compressed (see §3.1). Every gate that can be measured on
a laptop was measured; gates that need a human (phone on mobile data, Render dashboard,
UptimeRobot) are listed in §6.

---

## 2. Verification of CLAUDE.md against the engine source

CLAUDE.md claims every rule was verified against `BattlesnakeOfficial/rules`. I re-read
`standard.go`, `royale.go`, `constrictor.go`, `pipeline.go`, `ruleset.go`, `settings.go`,
`maps/royale.go`, `maps/standard.go`, `client/models.go` and `cli/commands/play.go` (reading
only — nothing copied; our resolver is a clean-room reimplementation).

| Rule | Verdict | Notes |
|---|---|---|
| Pipeline order | ✅ correct | `GameOver → Move → Starve → Hazard → Feed → Eliminate → [royale hazards]`; constrictor adds `RemoveFood → AlwaysGrow` after elimination |
| R1 health ≥ distance | ✅ | |
| R2 equal head-to-head kills both | ✅ | Comparison uses lengths **after** feeding (feeding runs first) |
| R3 heads not bodies | ✅ | But BUILD_PLAN Step 3 wording "bodyBlocked … excluding heads" is **misleading**: *current* heads become necks next turn and ARE blocked. R3 applies to the *new* heads. Implemented correctly; see `internal/legal` doc comment |
| R4 tail vacates unless duplicated | ✅ | Also makes R9 fall out automatically from the payload: a constrictor tail is always duplicated |
| R5 food cancels hazard damage | ✅ | |
| R6 hazard kills inline before feeding | ✅ | |
| R7 longest-first attribution | ✅ but cosmetic | Ordering only affects the `EliminatedBy` field, never who dies |
| R8 royale ring | ✅ with clarification | Engine computes `numShrinks = newTurn / N` with `newTurn = request turn + 1`; the ring only changes when `newTurn % N == 0`. Hazards that damage you on a move are exactly the request's hazards |
| R9 constrictor | ✅ with addition | Hazard damage still applies before health is pinned; growth applies to **every** snake including eliminated ones |
| R10 settings paths | ✅ | `shrinkEveryNTurns` nested under `royale`; `timeout` in `game` |
| R11 latency string | ✅ | Also true for **our own** snake — used for adaptive budgeting (§4.6) |

**New rules found that CLAUDE.md does not mention** (all golden-tested):

- **R12 — same-tick eliminations do not block.** Elimination phase 1 (out of health / out of
  bounds) and the earlier hazard stage remove snakes *before* collision checks, which only
  consider snakes still alive. A snake that starves this turn does not block anyone with its
  body. `TestR12_SameTickEliminationsDoNotBlock`.
- **Stacked hazards.** Hazard damage is applied once per hazard *entry* on the square, so
  duplicate hazards stack. We store hazard counts, not booleans. `TestHazardStacksApplyPerEntry`.
- **Royale may be a map, not a ruleset.** The engine exposes `game.map`; a royale game can be
  `ruleset.name = "standard"` with `map = "royale"`. Stage classification checks both.
- **Engine default move on error** is "continue in the direction of travel" (neck → head), which
  is what the docs call "repeat previous move". Used for doomed opponents in the envelope.
- **CLI export `you`** is an arbitrary snake per line (Go map iteration), possibly dead — the
  captured payloads were repaired accordingly.

---

## 3. Deviations from the plan (and why)

### 3.1 Branch cadence
Plan: one branch/PR per step, human verifies each gate before merge. With < 3 h left, Steps 0–9
landed on one branch `step-00-03-core` with a single PR, still squash-merged and tagged
`known-good`. Every automated gate was run before merging (results in §5).

### 3.2 `Decide` signature
Plan: `Decide(ctx, s) (string, Reason)`. Implemented `Engine.Decide(ctx, gs) Decision`, where
`Decision` carries move, reason, stage, profile, candidate scores and depth for the structured
log. The evaluator is injected (`decide.New(profiles, search.Evaluate)`) so the fallback-only
engine and the full engine are tested against the same fixtures, and the server and arena call
exactly the same code.

### 3.3 Package layout additions
- `internal/eval` — scoring separated from the envelope so duel search reuses it.
- `internal/search` — glue: duel search when two snakes live, otherwise TVAE.
- `internal/server` — HTTP layer in a testable package; `cmd/server` only wires it.
- `internal/fixture` — fixture DSL (§4.3).
- `internal/testgen` — seeded random positions for property/metamorphic tests.
- `embed.go` at the module root — config embedded in the binary (§4.9).

### 3.4 Differential testing
Plan: `testdata/fixtures/diff_vs_cli.sh` comparing 50 positions against the CLI. The CLI cannot
start from an arbitrary position, so a shell harness is awkward. Replaced by **two** stronger
checks: (a) `tools/arena/diff_test.go` runs our resolver against the official pipeline on every
turn of 240 random games across standard / royale 11 / royale 19 / constrictor / wrapped /
wrapped_constrictor with a 5-turn shrink so R8 fires constantly (thousands of turns, not 50);
(b) real CLI games against our HTTP server, whose exported request lines are kept as parser
fixtures in `testdata/payloads`.

### 3.5 Arena determinism
Wall-clock deadlines make search depth depend on machine load, which would break paired
comparisons. The arena defaults to `--budget 0`: no deadline, search depth capped by the
profile, so identical seeds give identical games (null test = exactly zero difference).
`--budget MS` switches to wall-clock mode to test latency behaviour.

---

## 4. Original ideas added beyond the plan

1. **R12 + stacked hazards + royale-as-map** found by re-reading the engine (§2).
2. **Evaluator injection** into `decide.Engine` so fallback-only and full engines share fixtures.
3. **Fixture DSL** (`internal/fixture`): one directive per line (`you 90 5,5 5,4 5,3`,
   `snake bob …`, `food …`, `safe 1 1 9 9` for royale rings, `expect up left`, `reject down`).
   Human-writable, diff-friendly, and `fixture.Format` renders any request back into it.
4. **Fatal-board harvesting.** The server remembers the last board per game; on a loss `/end`
   logs `fatal_board` in fixture DSL so a production loss becomes a test by copy-paste. Only the
   compact board is logged, never the raw payload.
5. **Tolerant parsing.** Latency accepted as string, number, null or garbage; explicit
   `hazardDamagePerTurn: 0` kept while an absent field defaults to 14 (pointer presence check);
   zero-length snakes dropped; missing board size inferred; boards > 64 rejected as hostile.
6. **Latency-adaptive budget.** The engine reports *our* previous response latency in
   `you.latency`. Overhead = that − our own compute time (EWMA per game). Budget margin =
   max(`networkMarginMs`, overhead + pad), then scaled by 2/(inflight+1) under concurrency.
   Directly addresses "latency is part of the algorithm".
7. **cgroup-aware GOMAXPROCS.** Render's free tier is a fraction of a CPU. Go would schedule on
   all host cores and burn the CFS quota in parallel, stalling the process for the rest of the
   100 ms period — straight onto move latency. `server.TuneRuntime` reads `/sys/fs/cgroup/cpu.max`
   and caps GOMAXPROCS (Go only does this itself from go 1.25 in go.mod; we pin 1.21 for host
   compatibility).
8. **Fallback extensions** (still weight-free, still < 1 ms): "not trapped" (bounded flood fill ≥
   length) and "not in hazard" keys inserted before free-neighbour count.
9. **Embedded config** via `embed.FS`, strict decoding (`DisallowUnknownFields`) so a typo in a
   profile fails CI instead of silently using a default; decoded over `Defaults()` so profiles
   list only what they change.
10. **Tie-breaking toward the fallback.** Safe moves are reordered so the fallback's choice is
    first; exact score ties resolve to the lexicographic safe choice.
11. **Contested tiles to the longer snake** (from the locality-limited MaxN paper in the owner's
    resource list): Voronoi `Attack` cells (unique strictly-longest arriver) are weighted by
    `attackCellWeight` (0.85) instead of the generic contested weight (0.5).
12. **Toroidal distance** (`State.Dist`) and wrapped `Step`: wrapped boards work end to end
    (differential-tested against the engine's wrapped rulesets).
13. **Cut-cell robustness defined precisely:** iterative Tarjan over our reachable region; for
    each articulation cell an opponent can reach or stand next to, the pocket it separates is
    sealable; `Robust = reach − (largest sealable pocket + 1)`.
14. **Two-ply head-to-head awareness** (`ExitsUncontested`, `wNoSafeExit`). Diagnosed from a real
    CLI constrictor game: four identical snakes marched to the centre; at turn 2 every move
    scored ≈ −0.55 with no sign that the next turn would leave every exit contested by an
    equal-length head; at turn 3 every move was a loss and all four died head-to-head. The fill
    now counts exits that no equal-or-longer head can also reach next turn, and the scorer
    penalises having none.
15. **Graceful shutdown + warm-up.** SIGTERM drains in-flight moves during redeploys; one dummy
    decision at start so the first real `/move` pays no first-use cost.
16. **Metamorphic tests at three levels**: resolver, Voronoi (rotation, reflection, opponent
    permutation), and TVAE candidate scores.
17. **TVAE-first, depth-gated duel search (`duelMinDepth`).** Measured that paranoid duel search
    is worse than TVAE at depth 3 and better at depth 4 on 19×19 (§7 #8–#9). Since Render's
    fractional CPU may not reach depth 4, TVAE always runs first and the duel move is only used
    when depth ≥ 4 completed; `Decide` keeps a completed evaluator move even if the deadline
    expired during the optional deeper search.
18. **Loss-bucket diagnosis from fatal boards** (`arena --dump-losses`) and **automated review
    triage** (§5.5): every reviewer finding was checked against the code and either fixed, A/B
    tested, or rejected with a reason.

---

## 5. Measured results

### 5.1 Unit / golden / metamorphic / fixture tests
`go vet ./...` clean; `go test ./...` green: R1–R9 + R12 + stacked hazards golden tests, parser
tests (nested shrink, root-level shrink ignored, defaults, latency forms, garbage, 4 captured CLI
payloads), stage table, legal layers, fallback (< 1 ms on 19×19 with 4 × length-34 snakes;
no avoidable suicide on 300 random positions), Voronoi hand tests + metamorphic (80 positions ×
3 transforms), envelope (≤ 256 outcomes, CVaR, metamorphic), 14 decision fixtures × 2 engines,
server adversarial payloads (15 cases × 3 endpoints, zero non-200), 8 concurrent games × 25
turns, deadline test at 5/40/150 ms budgets.

### 5.2 Micro-benchmarks (Apple M3, 10 cores)
| Operation | Time | Allocs |
|---|---|---|
| Resolve one turn, 4 snakes | 170 ns | 4 |
| Temporal Voronoi + cut cells, 11×11, 4 snakes | 2.3 µs | 0 |
| TVAE full decision, 4 snakes | 24 µs | 56 |
| Fallback, 19×19, 4 long snakes | 4.5 µs | 7 |

### 5.3 Real CLI games (official `battlesnake play` → our HTTP server)
Four simultaneous games (standard 4p, royale 11×11 4p, royale 19×19 2p, constrictor 4p), all
our snake: 3 502 moves served, **0 WARN**, reasons: duel 1 793, tvae 1 217, fallback 253 (search
completed but safe-only moves), forced 236, no_legal 3. Latency p50 1 ms, p95 148 ms, p99 222 ms
(duel iterative deepening uses the full 220 ms cap by design).

### 5.4 Arena baselines (deterministic, official rules in-process, zoo opponents, M3)

| Stage configuration | Games | Result | Timeouts | p99 decision | Wall |
|---|---|---|---|---|---|
| Qualifying: standard 11×11, 4 snakes | 500 | **8.77 pts/game, 84.0 % first** | 0 | 13.3 ms | 18.3 s (gate < 60 s ✅) |
| Bracket: royale 11×11, 4 snakes | 300 | **8.84 pts/game, 85.0 % wins** | 0 | 42.3 ms | 22.1 s |
| Final: royale 19×19, 1v1 (duel depth 3) | 100 | 93 % wins (never died; losses are length at the turn cap) | 0 | 13 ms | 10.8 s |
| Solo royale 11×11 | 100 | mean survival 434 turns (gate: > 150) | 0 | 2.3 ms | 1.7 s |
| Constrictor 11×11, 4 snakes (side event) | 100 | 7.33 pts/game, 52 % wins; self-collision is the top loss bucket | 0 | 25.7 ms | 0.6 s |
| Qualifying, wall-clock `--budget 150 --concurrency 4` | 100 | 8.90 pts/game, 86 % | **0** | 10.1 ms (max 35) | 7.0 s |

Loss buckets (DIAGNOSE step): qualifying 80 losses → head-to-head 46 (57 %), starvation 17,
body 10, self 7. Royale 45 losses → head-to-head 30 (67 %), hazard 9, starvation 5, self 1.
Reading the head-to-head fatal boards (`--dump-losses`): three of four are "sandwiches" — our
snake running down a corridor between two longer snakes into a wall until every exit is a
losing head-to-head; the fourth is a snake still length 4 at turn 123.

Null test (`TestNullTestDeterministic`): paired difference exactly 0 ✅.

### 5.5 Copilot review of PR #1 (automated) — triage

| Finding | Verdict | Action |
|---|---|---|
| CI licence check passes silently if `go list` fails (no `pipefail`) | Valid | Query into a variable first so failure is fatal |
| `MinBudgetMs` floor can exceed a tiny `game.timeout` | Valid | Floor capped at half the timeout; test for `timeout=1` and `30` |
| TVAE keeps the old ring on shrink turns | Partly valid (shrink-robustness term already uses the union of all four outcomes) | Added `envelopeShrinkPessimistic` and A/B-tested it (#4 below): zero effect, default off |
| Arena uses stale state when `Execute` reports game over | Not a bug: the engine's game-over stage runs before movement, and the loop breaks at ≤ 1 alive before calling `Execute` | None |

---

## 7. Optimisation loop log

Gate (OPTIMIZATION_LOOP Part 2): paired seeds `42,5,725,1337,99`, common random numbers, one
change per experiment against the shipped champion, promote only on a significant effect (p < 0.05)
of meaningful size with zero timeouts.

| # | Hypothesis (one change) | Stage | Games | Effect (B − A) | p | Decision |
|---|---|---|---|---|---|---|
| 1 | Stronger growth drive (`wFood` .35→.7, `lengthLead` 2→4, `foodDecayTurns` 250→400) cuts head-to-head + starvation losses | qualifying | 500 paired | +0.156 pts, +1.8 pp; h2h 46→37, starve 17→12, self 7→11 | 0.35 | **Reject** (not significant, < +0.4) |
| 2 | `wNoSafeExit` 0.6→1.5 avoids sandwiches | qualifying | 500 | −0.048 pts | 0.41 | **Reject** |
| 3 | `ensUniform` 0.5→1.5 so opponents' risky moves weigh more in the CVaR tail | qualifying | 500 | −0.076 pts; h2h 46→57 | 0.41 | **Reject** |
| 4 | `envelopeShrinkPessimistic` (review finding) | bracket | 300 | 0.000 — all 300 games identical | 1.0 | **No effect**; option kept, default off |
| 5 | Duel search off in qualifying (TVAE handles 1v1 endgames) | qualifying | 500 | −0.072 pts, −1.8 pp | **0.0065** | **Reject** — duel search helps on 11×11 |
| 6 | Duel search off in the bracket | bracket | 300 | −0.213 pts, −5.3 pp | **0.0003** | **Reject** — duel search clearly helps on 11×11 |
| 7 | Duel search off for the 19×19 final (vs zoo, duel depth 3) | final | 100 | TVAE-only 98 % vs 93 % | 0.058 | Inconclusive → head-to-head |
| 8 | Head-to-head 19×19: TVAE-only (seat 0) vs duel search **depth 3** | final | 200 | TVAE-only wins **58.5 %** | ≈ 0.02 | Depth-3 paranoid search is worse than TVAE |
| 9 | Head-to-head 19×19: TVAE-only vs duel search **depth 4** | final | 200 | TVAE-only wins **45 %** | ≈ 0.16 | Depth-4 search is better than TVAE |

**Promoted from #5–#9 — `duelMinDepth` (original idea, §4.17).** The value of duel search flips
between depth 3 and 4. Depth 4 on 19×19 costs ~18 ms on the M3; on Render's ≈0.1 CPU the
iterative deepening will often stop at depth 3 — exactly the regime where it loses to TVAE.
`search.Evaluate` now runs TVAE first (always completes, < 1 ms), then duel search in the remaining
time, and plays the duel move only if depth ≥ `duelMinDepth` = 4. Otherwise it plays the TVAE move.
`decide.Decide` was changed to trust a completed evaluator result even if the deadline passed a
moment later (previously a finished TVAE move would have been discarded for the fallback).
In the deterministic arena this is identical to the measured champion (depth 4 always
completes), so all stage baselines above still apply; on a slow CPU it degrades to TVAE instead of
to shallow, measurably worse search.

### 5.6 Verification of the promoted change (improve-001)

- Root tests incl. new `internal/search` tests (duel used only at min depth; a deadline that cuts duel search keeps the TVAE move) — green.
- Differential test still 2 877 turns identical; null test still exactly zero.
- Slow-CPU emulation: 19×19 1v1 at `--budget 20` wall-clock, 60 games vs zoo → 100 % wins, **0 timeouts**, p99 21.5 ms.
- Real CLI game, royale 19×19, two copies of the snake, 763 turns: **0 WARN**; 1 047 moves `duel` depth 6, 349 depth 5, 1 depth 4, 2 moves where search did not reach depth 4 and the TVAE move was played instead of the fallback; latency p50 83 ms, p99 221 ms (budget cap).

---

## 6. Gates only a human can close

- [ ] Render service created from this repo (Go runtime, `go build -o app ./cmd/server`, `./app`)
- [ ] `curl` the Render URL from a phone on mobile data
- [ ] `gh secret set SNAKE_URL --body https://…onrender.com` (enables `keepwarm.yml`)
- [ ] UptimeRobot 5-minute monitor
- [ ] Koyeb backup service + URL noted in RUNBOOK
- [ ] Snake registered on play.battlesnake.com and one practice game played
- [ ] Ask organisers: one real `/move` payload; shrink/damage settings; turn cap; number of qualifying rounds
