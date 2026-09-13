# DEVLOG — everything that was done, why, and what was measured

This file documents every action taken on the project, every deviation from the planning
documents, and every original idea added beyond them. It is the "separate file" requested by
the owner. Sections are in topic order; the timeline in §1 is the chronological index.

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
| 06:52–07:08 | Per-stage arena baselines, loss-bucket diagnosis, 9 paired experiments (§7), Copilot review triage (§5.5) |
| 07:10 | TVAE-first depth-gated duel search promoted; slow-CPU emulation and a 763-turn 19×19 CLI game verified |
| 07:12 | PR #2 merged after green CI; `known-good` moved to 9e3f3a3. Handover: NEXT_STEPS.md lists the owner-only tasks |
| 07:13 | PR #3 (timeline correction) merged; `known-good` a6410b1 |
| ~07:50 | Owner deployed on Render at https://seds-hackathon-rigur.onrender.com; live verification begins (§8) |
| 07:59 | PR #4 merged: 50 ms compute cap from live measurements + `X-Snake-Decision` header (live `aa762f4`) |
| 08:02 | PR #5 merged: grand-final duel search capped at depth 4 (live `b9a95ac`) |
| 08:07 | PR #6 merged: live verification docs (live `82cb73e`) |
| 08:10–08:35 | Owner registered the snake and played a practice game; hackathon extended by 1 h. Loss diagnostics and improvement proposals (§9); proposals PR #7 opened |
| 08:40 | Owner approved P1, P2, P3 and shared a Codex review; triage and plan (§10) |
| 08:41–08:45 | Codex-found defect fixed test-first (improve-004, §10.2); PR #7 merged |

Because only ~2h45m remained before freeze when work began, the plan's "one step per branch,
human verifies each gate" cadence was compressed (see §3.1). Every gate that can be measured on
a laptop was measured; gates that need a human are listed in §6.

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
`known-good`. Every automated gate was run before merging (results in §5). From improve-001 on,
every change has its own branch and PR.

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
19. **Compute cap from live measurements** (§8): budgets swept per request through `game.timeout`
    against the live service, no redeploy needed; `X-Snake-Decision` header for observability.
20. **Decision-level loss diagnostics** (§9.1): every loss classified by whether the fatal move
    was avoidable and how many turns earlier the last real choice was — which showed that
    lookahead at danger points, not the final move, is where points are lost.

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

Re-measured at 08:44 on `main` + improve-004: qualifying 500 games **8.768 pts, 84.0 %** (identical);
bracket 300 games **8.827 pts, 84.7 %** (was 8.84 / 85.0 % before PR #2 — see §10.3).

### 5.5 Copilot review of PR #1 (automated) — triage

| Finding | Verdict | Action |
|---|---|---|
| CI licence check passes silently if `go list` fails (no `pipefail`) | Valid | Query into a variable first so failure is fatal |
| `MinBudgetMs` floor can exceed a tiny `game.timeout` | Valid | Floor capped at half the timeout; test for `timeout=1` and `30` |
| TVAE keeps the old ring on shrink turns | Partly valid (shrink-robustness term already uses the union of all four outcomes) | Added `envelopeShrinkPessimistic`. **The first A/B ("no effect") was invalid — the option was never passed to the resolver (§10.2).** Corrected re-run: −0.07 pts/game, not promoted; four-world storm planned instead |
| Arena uses stale state when `Execute` reports game over | Not a bug: the engine's game-over stage runs before movement, and the loop breaks at ≤ 1 alive before calling `Execute` | None |

### 5.6 Verification of the promoted change (improve-001)

- Root tests incl. new `internal/search` tests (duel used only at min depth; a deadline that cuts duel search keeps the TVAE move) — green.
- Differential test still 2 877 turns identical; null test still exactly zero.
- Slow-CPU emulation: 19×19 1v1 at `--budget 20` wall-clock, 60 games vs zoo → 100 % wins, **0 timeouts**, p99 21.5 ms.
- Real CLI game, royale 19×19, two copies of the snake, 763 turns: **0 WARN**; 1 047 moves `duel` depth 6, 349 depth 5, 1 depth 4, 2 moves where search did not reach depth 4 and the TVAE move was played instead of the fallback; latency p50 83 ms, p99 221 ms (budget cap).

---

## 6. Gates only a human can close

- [x] Render service created from this repo — live at https://seds-hackathon-rigur.onrender.com (~07:50)
- [ ] `curl` the Render URL from a phone on mobile data (not yet confirmed)
- [x] `gh secret set SNAKE_URL` — set 07:27; manual keepwarm run succeeded (scheduled runs not yet observed)
- [ ] UptimeRobot 5-minute monitor (not yet confirmed)
- [ ] Koyeb backup service + URL noted in RUNBOOK (not yet confirmed)
- [x] Snake registered on play.battlesnake.com and one practice game played (~08:10)
- [ ] Ask organisers: one real `/move` payload; shrink/damage settings; turn cap; number of qualifying rounds; engine location

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
| 4 | `envelopeShrinkPessimistic` (review finding) | bracket | 300 | ~~0.000 — all 300 games identical~~ **invalid: the option never reached the resolver** (§10.2). Corrected re-run: −0.07 pts, −1.3 pp; storm deaths 10→12 | 0.47 | **Reject** (too passive) |
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
On a slow CPU it degrades to TVAE instead of to shallow, measurably worse search. (A side effect
on proven shallow wins was found later — §10.3.)

---

## 8. Live deployment verification (Render free tier)

Owner deployed at **https://seds-hackathon-rigur.onrender.com** (~07:50). All measurements below
were taken from the dev Mac in India against the live service.

### 8.1 Deployment identity
`GET /` → 200 in 130–260 ms, `"version":"a6410b1"` = `known-good` = `origin/main`. Garbage body to
`/move` → 200 `{"move":"up"}`.

### 8.2 Latency of the live snake
| Scenario | Samples | Total round trip |
|---|---|---|
| `GET /` (no compute) | 3 | 134–260 ms (network ≈ 115 ms) |
| `/move` 4-snake 11×11 royale (TVAE) | 10 | 117–257 ms |
| **Real CLI game, 4 snakes all on the live URL (4 concurrent requests/turn)** | 1 093 | **p50 82, p90 93, p99 137, max 215 ms; 0 failed** |
| 4 concurrent 11×11 requests | 4 | 109–240 ms |
| `/move` 19×19 1v1 (duel search), budget 220 ms | 9 | **376–516 ms** ❌ |
| same, budget 120 ms | 4 | 290–492 ms ❌ |
| same, budget 70 ms | 4 | 185–388 ms |
| same, budgets 25 / 35 / 40 / 50 / 60 ms | 8 each | p50 185 / 203 / 186 / 183 / 193 ms; max 281 / 456 / 277 / 264 / 278 ms |
| `/move` 11×11 1v1 (duel search), budgets 25 / 50 / 80 / 120 / 220 ms | 8 each | p50 125 / 124 / 122 / 122 / 125 ms; max 134 / 134 / 254 / 135 / 137 ms — search finishes depth 4 before any cap binds |

(Budgets set per request through `game.timeout`, since budget = timeout − 180 ms margin, so no
redeploy was needed to sweep them.)

**Finding.** TVAE (all 4-snake play) is safe on the live instance. Duel search runs until its
deadline, and on Render's fractional CPU quota a long burn triggers throttling stalls of up to
~300 ms beyond the budget. At the shipped 220 ms cap a 19×19 1v1 move already reached 516 ms from
India, which is a timeout, before counting the (unknown) distance to the tournament engine. At
≤ 60 ms budgets the median halves (~185 ms). The occasional ~450 ms outlier appears at all
budgets, including requests with almost no compute, so it is proxy/network noise, not search.

**Change (improve-002).** `cpuCapMs: 50` in all four shipped profiles, with a test that fails if a
profile goes above 60. TVAE needs well under 1 ms, so only the optional duel search is shortened;
the `duelMinDepth` gate (§7) ensures a too-shallow search plays the TVAE move instead. The arena's
deterministic mode ignores wall-clock caps, so the §5 baselines are unchanged.

**Observability.** `/move` now returns an `X-Snake-Decision` header
(`reason=… depth=… budget_ms=… compute_ms=… inflight=…`). Clients ignore unknown headers; it lets
anyone check the live snake's behaviour with `curl -D -` without Render dashboard access.

### 8.3 Post-deploy verification of improve-002 (live `aa762f4`, deployed 18 s after merge)

Six requests per payload at the default `timeout: 500`, read from the new header:

| Payload | Total round trip | `X-Snake-Decision` |
|---|---|---|
| 4-snake 11×11 royale | 110–119 ms | `reason=tvae`, compute 0 ms |
| 1v1 11×11 royale | 118–131 ms | `reason=duel depth=4`, compute 6–12 ms |
| 1v1 19×19 royale | 254–377 ms (was 376–516 ms) | `reason=duel depth=4`, **compute 56–137 ms against a 50 ms budget** |

19×19 always completes depth 4, which is the depth the arena showed beats TVAE (§7 #9). The
overshoot above the 50 ms budget comes from starting depth 5 with the leftover budget: that extra
burn exhausts the CPU quota and the process is throttled until the next quota period. Depth 5+
was never shown to help (the arena's 19×19 measurements used depth 3 and 4).

**Change (improve-003).** `config/duel.json` `duelMaxDepth` 6 → 4, so the grand-final search
stops as soon as the proven-useful depth completes. 11×11 profiles were already at 4.

### 8.4 Post-deploy verification of improve-003 (live `b9a95ac`, deployed 19 s after merge)

| Scenario (live, from India) | Result |
|---|---|
| 8 × `/move` 19×19 1v1 at `timeout: 500` | total **140–272 ms** (was 254–377 after improve-002, 376–516 before); 7 × `duel depth=4` with compute 13–98 ms, 1 × `tvae depth=3` (search too shallow → TVAE move, as designed) |
| **Real CLI game, royale 19×19, both snakes on the live URL** (455 turns, 2 concurrent requests per turn) | 907 latency samples: **p50 190, p90 214, p99 307, max 414 ms; 0 failed requests**; 2 samples ≥ 400 ms |
| Earlier real CLI game, standard 11×11, 4 snakes on the live URL | 1 093 samples: p50 82, p99 137, max 215 ms; 0 failed |

Latency history for the grand-final scenario (19×19 1v1, measured from India):
220 ms cap, depth 6 → up to 516 ms · 50 ms cap, depth 6 → up to 377 ms · 50 ms cap, depth 4 →
p99 307 ms over a full game. The remaining tail (0.2 % of moves ≥ 400 ms) is network plus
occasional throttling; if the tournament engine is farther away than India→Singapore, watch the
`overhead_ms` field — the adaptive margin shrinks the budget automatically after the first moves
of each game.

Tuning paused here: every further change needs a deploy plus live re-measurement.

---

## 9. Improvement research (08:10–08:35) — proposals only, bot unchanged

Owner registered the snake and played a practice game; the hackathon was extended by 1 h and the
owner asked for a proposal document (additions only) to approve before any change.

### 9.1 Loss diagnostics
A scratch harness (outside the repo; copies the arena game loop and zoo) replayed games with the
official rules and, for every decision of our snake, recorded safe / non-losing-head-to-head /
viable (not a dead end) option counts and the evaluator's best score. Per loss it records whether a
viable option existed at the fatal decision and the turns since the last decision with ≥ 2 viable
options.

| Run | Games | Losses | Fatal move avoidable | Last real choice ≤ 3 turns | Evaluator saw it coming |
|---|---|---|---|---|---|
| qualifying vs zoo | 500 | 84 | 0 | 59 | 0 |
| royale vs zoo | 300 | 46 | 0 | 35 | 0 |
| qualifying vs 3 champions | 200 | 137 | 0 | 80 | 3 |
| constrictor, standard map | 100 | 52 | 0 | 3 | 0 |
| constrictor, `hz_scatter` | 100 | 49 | 0 | 2 | 0 |
| constrictor, `hz_scatter`, storm penalties off | 100 | 49 | 0 | 2 | 0 |
| royale 19×19 1v1 vs champion (depth 4) | 100 | 0 | — | — | — (every game reached the 400-turn cap) |

Loss causes: qualifying head-to-head 45, starvation 19, body 13, self 7; royale head-to-head 31,
storm 9, starvation 5, body 1; self-play head-to-head 83, self 33, body 19, starvation 2;
constrictor self 38, body 13, head-to-head 1.

### 9.2 Hosting facts checked on the web
- Hugging Face Spaces: new free accounts can no longer run Docker/compute Spaces on free CPU
  (change rolled out June–August 2026, per the HF community forum thread "Official Community
  Complaint: Revert Free CPU Basic Spaces…"). Dropped.
- GitHub Codespaces: 120 core-hours/month on Free plans, public port forwarding allowed, idle
  timeout default 30 min and reset by terminal output (GitHub Docs). Kept as a research option.

### 9.3 Output
`docs/IMPROVEMENT_PROPOSALS.md` (11 proposals, 3 tiers, gates, estimates, recommended order) and an
approval page (claude.ai artifact with decisions stored for Claude). Merged as PR #7 at 08:42 after
the owner's approval.

---

## 10. Codex review (08:40) — triage, fixes and implementation plan

The owner approved P1, P2 and P3 and asked to take "all the good and important" items from a
Codex review of the codebase. Each item was checked against the code and the measurements.

### 10.1 Triage

| Codex item | Verdict | Reason / plan |
|---|---|---|
| `EnvelopeShrinkPessimistic` computed but never passed to the resolver | **Valid defect** | Fixed test-first (improve-004, §10.2) |
| Selective two-turn Threat Graph: "can opponents force every exit contested or lethal next turn?" | **Take — merged with P1** | §9.1: 70–76 % of losses sealed 1–3 turns before death, unseen by the evaluator |
| Four-world royale storm combined by stage risk posture | **Take** | The pessimistic union measured −0.07 pts (too passive, §7 #4); worlds enter the envelope as equal-weight branches so CVaR / mean / min apply per stage |
| Exact tournament-points utility (10/6/3/1, simultaneous eliminations) | **Take** | Terminal utility for qualifying from exact tied-placement points; arena already scores this way |
| Parameter-driven arena (food, hazard, shrink, map, turn cap, tie-break) + grid tuning | **Take** | Flags first; grid tuning after the features land |
| Opponent adaptation (reweight after enough observations, never prune threats) | **Take — merged with P2** | |
| Threat-preserving joint-action pruning | **Take** (with P2) | Collapsing an opponent must keep moves that can reach our next exits |
| Food-race certificates (arrival, length at arrival, escape after eating) | **Take — merged with P3** | Starvation is 23 % of qualifying losses |
| Duel transposition table + move ordering | **Defer** | Codex conditions it on a benchmark; after the above |
| Controlled mixed play in equal 1v1 positions | **Skip for this event** | No opponent models our tie-breaks; determinism keeps paired tests valid |
| Final posture chosen by score state | **Skip for now** | No score-state data yet; revisit with real games |
| Latency circuit breaker | **Take** | Live tail: 0.2 % of 19×19 moves ≥ 400 ms (§8.4) |
| Validation: tactical regression fixtures, organiser payload goldens, fuzz tests, bootstrap CIs, adversarial zoo | **Take** all but organiser payloads | Organiser payloads need a real request from the organisers (§6) |

### 10.2 improve-004 — the defect, test first
`envelope.Evaluate` built `opt` from `EnvelopeShrinkPessimistic` but resolved every outcome with a
hard-coded `ShrinkKeep`. `TestEnvelopeShrinkPessimisticReachesResolver` (royale, turn 24, shrink 25,
our head on the edge) asserts that the pessimistic ring lowers every candidate's score. It **failed
on the old code** (both 0.2366) and passes after the one-line fix. All root and arena tests green.

Consequences for the record: §7 #4 and the §5.5 Copilot triage row were based on the broken flag and
are corrected above. Re-run with the flag really applied — royale 11×11, 300 paired games:
−0.07 pts/game (p = 0.47), −1.3 pp wins (p = 0.32), storm deaths 10 → 12, starvation 5 → 8;
royale 19×19 1v1, 100 paired games: identical (duel search decides nearly every move). Not
promoted; default stays off.

### 10.3 Observation — a side effect of `duelMinDepth`
Re-measuring baselines at 08:44: qualifying is identical to before PR #2 (8.768 pts, 84.0 %) but the
bracket moved from 8.84 pts / 85.0 % to 8.827 / 84.7 % (about one game in 300). The arena is
deterministic, so behaviour changed. Hypothesis: when duel search proves a win before depth 4 it
stops early, and the `duelMinDepth` gate then discards the proven winning move for the TVAE move.
Planned fix: a proven win is always played; a proven loss still defers to TVAE (which picks the
most robust line). To be confirmed with a paired bracket run.

### 10.4 Implementation order

1. ✅ improve-004 — storm-option defect, test first
2. ✅ improve-005 — arena: parameters (food spawn, minimum food, hazard damage, shrink cadence, map,
   turn cap, tie-break), bootstrap confidence intervals, `--diagnose`, adversarial zoo (pincer,
   food-bait, edge-herder, storm-trapper), `--duel-depth` also lowers `duelMinDepth`; fuzz tests
3. improve-006 — proven duel wins bypass the depth gate (§10.3)
4. improve-007 — **P1 Threat Graph** (selective second ply)
5. improve-008 — **P3 food-race certificates**
6. improve-009 — **P2 opponent adaptation** + threat-preserving pruning
7. improve-010 — four-world royale storm
8. improve-011 — exact tournament-points terminal utility
9. improve-012 — latency circuit breaker
10. Robust tuning across a parameter grid; duel transposition-table benchmark

Every behaviour change ships behind a config flag that defaults off, is enabled only after a paired
arena gate with bootstrap confidence intervals, and is checked live through `X-Snake-Decision`.

### 10.5 improve-005 — arena and validation upgrades (no bot behaviour change)

What was added:
- **Every game setting is a flag** — `--food-spawn`, `--min-food`, `--hazard-damage`, `--shrink`,
  `--map`, `--max-turns`, `--tie-break length|draw` — and the same values reach both the official
  engine and the request our bot parses (`TestConfigReachesEngineAndRequest`,
  `TestShrinkCadenceChangesTheGame`).
- **`--grid "shrink=15,25;food-spawn=10,25"`** runs a comparison in every combination and reports
  the worst cell, so a change that helps on average but hurts in a plausible event setting is
  visible. Placement scoring moved into `placements()` with a tie-break test.
- **Bootstrap confidence intervals** (95 %, fixed seed, reproducible) next to every paired t-test.
- **`--diagnose`** (the decision-level classification from §9.1) and `--examples N` are in the repo.
- **`--duel-depth`** also lowers `duelMinDepth`; before, `--duel-depth 3` silently disabled duel search.
- **Adversarial zoo**: `pincer`, `foodbait`, `edgeherder`, `stormtrapper` (`--opponents
  adversarial` or `full`); the original `zoo` rotation is untouched so earlier baselines compare.
- **Fuzz tests** `FuzzParse` (normalisation invariants) and `FuzzDecide` (valid move, no panic,
  bounded time) with seed corpora from real payloads and fixtures, run in CI; **property tests**:
  a duplicated tail never vacates, and a safe uncontested move never dies by wall, self-collision,
  starvation or hazard.

Measured:

| Check | Result |
|---|---|
| Default qualifying, 500 games | 8.768 pts / 84.0 % — identical to before, so the parameterisation changes nothing by default |
| `--diagnose` qualifying, 500 games | 80 losses; fatal move avoidable 0; last real choice 1–3 turns before death 57 (71 %), 4–10 turns 21, > 10 turns 2; evaluator saw it coming 0 |
| Adversarial zoo, qualifying, 300 games | 9.22 pts / 90.3 % |
| Adversarial zoo, royale, 300 games | 8.88 pts / 85.3 % |
| Grid smoke, identical A and B, royale shrink 15 vs 25, 40 games each | difference 0, CI [0, 0] in both cells; baseline 8.15 pts at shrink 15 vs 9.05 at 25 |

Notes:
- The scratch harness in §9.1 (turn cap 500, copied game loop) reported 84 losses with 59 sealed
  within three turns; the in-repo `--diagnose` on the real arena loop (cap 600) reports 80 and 57.
  Same conclusion; the in-repo numbers are authoritative from here on.
- The scripted adversaries are **not harder** than the original zoo in aggregate. The strongest
  opponent available remains our own engine, so P1–P3 are gated on both the zoo and self-play
  (`--opponents champion`) pools.
- The shrink-15 grid cell costs 0.9 pts/game against shrink 25: the event's royale settings matter,
  which is why robust tuning (step 10) runs across a grid rather than one assumed cadence.
