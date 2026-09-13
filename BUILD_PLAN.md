# BUILD_PLAN.md — every step, every file, every gate

Feed Claude Code **one step at a time**. Never paste the whole document.
Read `CLAUDE.md` first; rule IDs (R1–R11) refer to its §2.

**Every step follows the same git ritual:**
```bash
git checkout main && git pull
git checkout -b step-NN-name
# ... work ...
go build ./... && go test ./...
git add -A && git commit -m "feat: <what>"
git push -u origin step-NN-name
gh pr create --title "Step NN: <name>" --body "<what changed / gate / measured result>"
# after the gate passes:
gh pr merge --squash --delete-branch
git checkout main && git pull
git tag -f known-good && git push -f origin known-good
```
Do not advance until the gate passes. Never `git checkout .` or `git reset --hard`.

---

## Target layout

```
seds-hackathon-Rigur/
├── CLAUDE.md  BUILD_PLAN.md  OPTIMIZATION_LOOP.md  KICKOFF.md  SETUP.md
├── CREDITS.md  CHANGELOG.md  RUNBOOK.md
├── go.mod                          # module: server + internal. NEVER imports AGPL rules
├── cmd/server/main.go              # HTTP server, 4 endpoints
├── internal/
│   ├── api/          model.go parse.go parse_test.go   # R10/R11 exact paths
│   ├── stage/        stage.go stage_test.go
│   ├── board/        board.go                          # compact state, make/undo
│   ├── rules/        resolve.go rules_test.go          # OUR one-turn resolver
│   ├── legal/        legal.go legal_test.go            # R1-R4
│   ├── fallback/     fallback.go                       # lexicographic, never fails
│   ├── voronoi/      temporal.go cutcells.go *_test.go # TVAE evaluator
│   ├── envelope/     tvae.go risk.go                   # joint actions + CVaR
│   ├── royale/       storm.go storm_test.go            # R8
│   ├── duel/         search.go                         # 1v1 only
│   ├── opponent/     ensemble.go profile.go            # advisory, keyed by NAME
│   ├── config/       params.go                         # all tunables
│   └── decide/       decide.go                         # single entry point
├── config/  qualifying.json  royale.json  duel.json  constrictor.json
├── tools/arena/                    # SEPARATE go.mod — may import AGPL rules
│   ├── go.mod  arena.go  baselines.go  league.go
├── tune/  optimize.py  ab_test.py  hall_of_fame/
├── testdata/ fixtures/  payloads/
└── .github/workflows/  ci.yml  keepwarm.yml
```

---

## STEP 0 — Live skeleton, deployed

**Branch `step-00-skeleton`.**

Create:
- `go.mod` — module `github.com/Rigur-Calypso/seds-hackathon-Rigur`
- `cmd/server/main.go` — four endpoints (handbook §03): `GET /` info JSON; `POST /start`, `POST /move`, `POST /end`. `/move` returns `{"move":"up"}`. **Bind `0.0.0.0`, read `$PORT` (default 8080).** `recover()` middleware so no panic escapes. Structured logging.
- `.github/workflows/ci.yml` — `go vet`, `go test ./...`, and the licence check: `go list -deps ./cmd/server | grep -q BattlesnakeOfficial && exit 1 || true`
- `.github/workflows/keepwarm.yml` — as in `SETUP.md` §5
- `CREDITS.md`, `CHANGELOG.md`, `RUNBOOK.md` (stubs)

**Gate:** Render auto-deploys from `main`. `curl` the URL **from mobile data** returns valid
info JSON. One local CLI game completes:
```bash
battlesnake play -W 11 -H 11 --name me --url http://localhost:8080 -g standard --browser
```

---

## STEP 1 — Parsing, settings, stage

**Branch `step-01-parse`.**

`internal/api/model.go` + `parse.go`. **R10 exact paths** — get the nesting right:

```go
type Ruleset struct {
    Name     string          `json:"name"`
    Settings RulesetSettings `json:"settings"`
}
type RulesetSettings struct {
    FoodSpawnChance     int            `json:"foodSpawnChance"`
    MinimumFood         int            `json:"minimumFood"`
    HazardDamagePerTurn int            `json:"hazardDamagePerTurn"`
    Royale              RoyaleSettings `json:"royale"`    // R10: NESTED
}
type RoyaleSettings struct {
    ShrinkEveryNTurns int `json:"shrinkEveryNTurns"`
}
type Game struct {
    ID      string  `json:"id"`
    Ruleset Ruleset `json:"ruleset"`
    Map     string  `json:"map"`
    Timeout int     `json:"timeout"`   // R10: USE THIS
}
```

Also parse `snakes[i].latency` and `snakes[i].length` (R11). Log all parsed settings once in
`/start`. Sane defaults if absent: timeout 500, hazard damage 14, shrink 25.

`internal/stage/stage.go` — exactly as `CLAUDE.md` §4.

`internal/opponent/profile.go` — `Registry` keyed by `game.id`, mutex-guarded, entries deleted
in `/end`. Profiles keyed by `snake.name`, persisted to `profiles.json`.

**Gate:** `testdata/payloads/` has one fixture per ruleset (standard, royale 11×11, royale
19×19, constrictor). Each parses, classifies correctly, and reports correct settings. Two
concurrent games do not cross-contaminate state.

---

## STEP 2 — The exact one-turn resolver

**Branch `step-02-resolver`.** The foundation everything else stands on.

`internal/rules/resolve.go` — `Resolve(state, moves) State` implementing the exact pipeline in
`CLAUDE.md` §2, including R8 Royale hazard regeneration after elimination.

`internal/rules/rules_test.go` — golden tests, one per rule:
- R1: health 3, food at distance 3 → survives. Health 3, distance 4 → dies
- R2: equal-length head-on → both eliminated. Longer → only the shorter dies
- R3: move onto a shorter snake's head → we live, they die
- R4: snake that just ate → its tail is blocked. Snake that did not → its tail is free. **Test both our own tail and an opponent's**
- R5: head onto a hazard square **containing food** → no hazard damage, health 100
- R6: head onto a hazard square with **no** food at low health → eliminated
- R7: three-way collision → attributed to the longest
- R8: hazard ring geometry at successive shrink counts
- R9: constrictor → no tail vacates, food list empty, all grow

`testdata/fixtures/diff_vs_cli.sh` — differential harness: generate N random positions, resolve
with our code and with the official CLI, assert identical outcomes.

**Gate:** all golden tests pass. Differential harness agrees with the official CLI on 50
random positions across standard and royale.

---

## STEP 3 — Legal moves and the guaranteed fallback

**Branch `step-03-safety`.**

`internal/legal/legal.go` — three occupancy layers, **never merged**:
1. `bodyBlocked` — all segments except each snake's tail-if-vacating (R4), excluding heads (R3)
2. `headCells` — resolved by R2, not by body collision
3. `hazard` — costly, **not** blocked

`internal/fallback/fallback.go` — lexicographic comparator (pattern from TheApX/hungry), no
weights, no numbers to tune:
never the neck → never out of bounds → never a body cell → never a losing head-to-head →
prefer more immediate free neighbours → prefer fewer steps from food (one BFS outward from all
food) → prefer toward the centre of the safe rectangle.
**Must run under 1 ms and must never return an error.**

`internal/decide/decide.go` — the single entry point:
```go
func Decide(ctx context.Context, s *api.GameState) (string, Reason) {
    stage := stage.Classify(s)
    legal := legal.Moves(s)
    if len(legal) == 0 { return "up", ReasonNoLegal }
    if len(legal) == 1 { return legal[0], ReasonForced }
    fb := fallback.Best(s, legal, stage)        // ALWAYS first
    move, err := evaluate(ctx, s, legal, stage) // stub for now
    if err != nil || ctx.Err() != nil { return fb, ReasonFallback }
    return move, ReasonEvaluated
}
```
**Both the server and the arena call `Decide`. Never duplicate decision logic.**

Write 20–30 compact fixtures in `testdata/fixtures/` with asserted correct moves.

**Gate:** every fixture asserts its move. Solo game survives past turn 200. p99 decision time
under 20 ms. `diff_vs_cli.sh` confirms our legal set matches the engine on 20 positions.

---

## STEP 4 — Temporal Voronoi evaluator

**Branch `step-04-voronoi`.** The heart of the snake.

`internal/voronoi/temporal.go` — one multi-source BFS returning everything:

```go
type Result struct {
    Guaranteed  [4]int   // strictly earlier than any contender
    Contested   [4]int   // equal arrival or unresolved head-to-head
    Attack      [4]int   // simultaneous arrival while strictly longer (R2)
    WeightedArea[4]int   // weighted by health-on-arrival
    FoodDist    [4]int   // -1 if unreachable
    SafeExits   [4]int
    CutCells    [4]int
    Trapped     [4]bool  // Guaranteed[i] < length[i]
}
```

Requirements, in priority order:
1. **Time-aware occupancy.** `freeAt[cell]` = that segment's index from the tail. Enterable at BFS distance `d` iff `freeAt[cell] <= d`. For our own body, `freeAt[cell] + foodEatenEnRoute <= d` (R4 + snork).
2. **Health carried through.** −1 per step, −(hazardDamage+1) on hazard, reset to 100 on food (R5). Refuse the cell if health would reach 0.
3. **Lockstep multi-source.** All heads at distance 0. **Equal-arrival cells become `Contested`, never assigned by index.** Where we are strictly longer, they also count as `Attack`.
4. **Bounded** at `params.SafeCavernRatio * ourLength`.
5. **Constrictor (R9):** `freeAt[cell] = MaxInt` for all body cells.

`internal/voronoi/cutcells.go` — articulation points via Tarjan on the free-cell graph. Space
reachable only through a single narrow entrance is discounted: an opponent can seal it.

`internal/voronoi/metamorphic_test.go` — rotate/reflect boards and permute opponent order;
outputs must transform correspondingly.

**Gate:** hand-built tests for tail-chase, just-eaten tail, hazard cost, food reset, contested
cells. Metamorphic tests pass. Beats random-legal in >95% of 200 arena games (Step 5 may land
first if you prefer; they are independent).

---

## STEP 5 — Arena and baselines

**Branch `step-05-arena`.** Build before tuning anything.

`tools/arena/go.mod` — **separate module**, may import `BattlesnakeOfficial/rules` (AGPL,
§8). This gives exact production rules *and* in-process speed, eliminating simulator divergence
entirely.

`tools/arena/arena.go` — in-process loop, **no HTTP**, calls `decide.Decide` directly:
- `--rules standard|royale|constrictor`, `--width/--height`, `--snakes N`
- `--seed S --games N`, fully deterministic
- `--profile-a --profile-b` to pit two configurations
- **Placement points (10/6/3/1) as qualifying fitness; win rate for royale and duel — reported separately, never blended**
- `--budget MS` to simulate reduced compute
- `--concurrency N` to simulate parallel games
- Parallel across cores; machine-readable JSON output: mean points, win rate, mean survival turns, timeout count, p50/p95/p99 decision time

`tools/arena/baselines.go` — opponent zoo, so we never train only against ourselves:
`RandomLegal`, `FoodGreedy`, `SpaceGreedy`, `HeadHunter`, `HazardCoward`, `LastChampion`.

**Gate:** 500 four-snake games in under 60 seconds on the M3. Null test (champion vs identical
copy, paired seeds) reports **no significant difference** — if it does not, the arena is not
deterministic and every later number is fiction.

---

## STEP 6 — TVAE and stage risk

**Branch `step-06-tvae`.**

`internal/envelope/tvae.go` — for each of our legal moves, enumerate every joint opponent
action (~27 per candidate, ~81 total), resolve each exactly (Step 2), evaluate each surviving
outcome (Step 4).

`internal/envelope/risk.go` — aggregation per `CLAUDE.md` §6:
- Qualifying: **CVaR-25** — mean of the worst 25% of outcomes
- Bracket: expected value, credit only for *forced* eliminations
- Final: maximin
- Constrictor: guaranteed area and mobility only

`internal/opponent/ensemble.go` — four cheap policies generate *likely* actions for weighting,
but **all legal actions stay in the envelope**. Profiles are advisory priors only.

`internal/config/params.go` + `config/*.json` — three profiles (`qualifying`, `royale`, `duel`)
plus a constrictor override. **Zero magic numbers anywhere else.**

**Gate:** at most 256 first-ply outcomes per turn. p99 decision time under the request-derived
budget at `--concurrency 4`. Beats `FoodGreedy` in >80% of 300 games; mean placement points
above 7.0 against three greedy opponents.

---

## STEP 7 — Royale layer

**Branch `step-07-royale`.** Two of three stages are Royale; most teams will arrive untested.

`internal/royale/storm.go`:
```go
func SafeRect(s *api.GameState) Rect          // reconstruct from the hazard list. R8
func PossibleNext(r Rect) [4]Rect             // R8: exactly four outcomes
func ShrinkRobust(p Point, r Rect) bool       // safe under ALL four
func TurnsToShrink(turn, shrinkEveryN int) int
func HazardBudget(health, hazardDamage int) int // ~6 turns at full health, 14 dmg
```

Evaluator additions, gated on stage:
- **Centre pull** toward the safe-rectangle centroid, ramped by turn
- **Shrink robustness** bonus when `TurnsToShrink <= 2` and the cell survives all four outcomes
- **Hazard as a weapon:** bonus for cells we can reach within `HazardBudget(ourHealth)` that an opponent cannot reach within `HazardBudget(theirHealth)`. At full health we get ~6 turns of traversal; an opponent at 40 health gets ~2
- **Food in hazard is cheap (R5):** zero hazard cost on food cells
- **Storm frontier value:** in bracket play prefer a *forced* opponent boundary loss over passive centrality

**Gate:** fixtures cover hazard, food-in-hazard, imminent shrink, 11×11 and 19×19. Survives
past turn 150 in >70% of solo royale games. Beats the Step 6 build in royale 11×11.

---

## STEP 8 — Resilience

**Branch `step-08-resilience`.** Explicitly its own step. This is where forfeits are prevented.

Tests: malformed payload, missing fields, zero-length snake, empty food list, 1×1 board,
forced timeout, `--concurrency 8`, process restart mid-game, external reachability.

Add adaptive budgeting: track in-flight requests atomically, scale the deadline down under
contention, degrade policies before depth.

Write `RUNBOOK.md`: how to redeploy, how to switch to Koyeb, how to revert to `known-good`, who
owns keep-warm. Rehearse each once.

**Gate:** zero 5xx across every adversarial test. Fallback always returns a legal move. p99
comfortably under budget at concurrency 4. Restart and revert both rehearsed end to end.

---

## STEP 9 — Duel search

**Branch `step-09-duel`.** Only if every earlier gate is green. The grand final is 1v1.

`internal/duel/search.go` — exact simultaneous first ply (Step 6), then iterative deepening
maximin with alpha-beta. **Cooperative deadline via `ctx` — never a detached goroutine.**
PV move ordering: carry the previous iteration's ordering forward. Transposition table keyed by
complete state. Depth target 6–8 (branching ~9).

**Gate:** never exceeds its deadline in stress tests. Wins on hand-built duel fixtures. Beats
the Step 7 build in 1v1 royale 19×19 by a measurable margin.

---

## STEP 10 — Offensive pressure

**Branch `step-10-pressure`.**

- Forced-elimination detection: after each candidate, is some opponent's guaranteed area below their length?
- Death ranking (bookworm): if we die anyway, prefer head-to-head over starvation over self-collision
- Placement-aware terminal utility: qualifying uses placement points; bracket and final use advancement

**Gate:** opponent-elimination rate rises with no drop in our own survival rate.

---

## STEP 11 — Constrictor (optional, ~30 min, separate leaderboard)

**Branch `step-11-constrictor`.** Gate everything on `Stage::Constrictor`. Disable food,
health, and tail-vacate logic. R9: all bodies permanent. Score = guaranteed area + mobility.

**Gate:** survives markedly longer than the standard-tuned profile on a constrictor board.

---

## STEP 12 — Practice, tune, freeze

**Branch `step-12-tuning`.** Hand over to `OPTIMIZATION_LOOP.md`.

During the practice window: run the known-good build, **collect data, do not experiment**.
Save every fatal-board payload as a fixture. Classify each loss.

Final checklist:
- [ ] Deployed, awake, registered; UptimeRobot + Actions keepwarm both live
- [ ] URL verified from mobile data
- [ ] Koyeb backup deployed and its URL noted
- [ ] Zero timeouts in a 500-game run at concurrency 4 and at reduced budget
- [ ] All four stages classify correctly and load distinct profiles
- [ ] `go list -deps ./cmd/server | grep BattlesnakeOfficial` returns nothing
- [ ] `known-good` tag pushed; revert rehearsed
- [ ] All golden, metamorphic, and fixture tests green
- [ ] `CREDITS.md` complete; `RUNBOOK.md` rehearsed

**Feature freeze at hour 10:45.**
