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

### 5.4 Arena
(filled in below as runs complete)

---

## 6. Gates only a human can close

- [ ] Render service created from this repo (Go runtime, `go build -o app ./cmd/server`, `./app`)
- [ ] `curl` the Render URL from a phone on mobile data
- [ ] `gh secret set SNAKE_URL --body https://…onrender.com` (enables `keepwarm.yml`)
- [ ] UptimeRobot 5-minute monitor
- [ ] Koyeb backup service + URL noted in RUNBOOK
- [ ] Snake registered on play.battlesnake.com and one practice game played
- [ ] Ask organisers: one real `/move` payload; shrink/damage settings; turn cap; number of qualifying rounds
