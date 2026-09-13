# OPTIMIZATION_LOOP.md — the loop that runs until it converges

Two nested loops, one statistical gate.

- **Inner loop** — automated. Searches *parameter space*. Runs unattended overnight.
- **Outer loop** — agentic, run by Claude Code. Searches *idea space*: one change, measured, promoted or reverted.

Nothing ships on intuition.

---

## Part 1 — Fitness: per-stage, never blended

**Do not collapse the stages into one scalar.** A blended score can hide a bracket regression
behind a qualifying gain, and the bracket is best-of-1 — a single loss ends the run.

Maintain **three champions**, promoted independently:

| Profile | Arena configuration | Primary metric |
|---|---|---|
| `qualifying` | standard, 11×11, 4 snakes, zoo opponents | mean placement points (10/6/3/1) |
| `royale` | royale, 11×11, 4 snakes, zoo opponents | win rate |
| `duel` | royale, 19×19, 2 snakes | win rate |

Every profile additionally reports, as **hard gates rather than score terms**:
- timeout count — must be **exactly 0**
- p99 decision time at concurrency 4 — must be under budget
- p99 at `--budget 150` — the champion must still win, or it is fragile in exactly the conditions the handbook warns about
- opponent-zoo floor — no regression beyond a small predefined bound against `RandomLegal`, `FoodGreedy`, `SpaceGreedy`, `HeadHunter`, `HazardCoward`

---

## Part 2 — The statistical gate

This is what stops you shipping noise at 4 AM.

Placement points have mean 5.0 and standard deviation 3.39 per game — a very noisy signal.
Games needed to detect a real improvement at 95% confidence, 80% power:

| True effect (pts/game) | Independent | **Paired (common random numbers)** |
|---|---|---|
| 0.15 | 8,015 | 4,008 |
| 0.25 | 2,886 | **1,443** |
| 0.50 | 722 | **361** |
| 0.75 | 321 | **161** |
| 1.00 | 181 | **91** |

Three consequences that shape the code:

1. **Always use common random numbers.** Champion and challenger run on an identical fixed seed set with identical food spawns and identical opponent behaviour. Halves the games needed.
2. **Arena throughput is the binding constraint on the whole process.** At 500 games/minute a 1,443-game test takes three minutes. At 50 games/minute it takes half an hour and the loop dies. Budget real effort on arena speed.
3. **Small improvements are not measurable in this window.** Set the bar at **+0.4 pts/game** (~500 paired games) and reject below it. A change worth less is not worth the regression risk.

**Promotion rule:**
```
PROMOTE if  target-stage effect > threshold
        AND paired t-test p < 0.05
        AND timeout count == 0
        AND p99 within budget at concurrency 4 AND at --budget 150
        AND opponent-zoo floor not regressed
        AND all golden + metamorphic + fixture tests pass
REVERT otherwise (git revert, never git checkout .)
```

**Run the null test first** — champion against an identical copy on paired seeds. It must
report no significant difference. Thirty seconds, and it protects the entire night.

---

## Part 3 — Inner loop (automated parameter search)

`tune/optimize.py` — (μ+λ) evolution strategy with a hall of fame. Chosen over grid search or
Bayesian optimisation because the space is ~20-dimensional, evaluation is noisy and expensive,
gradients do not exist, and it parallelises trivially.

```python
POP, ELITE, GAMES = 16, 4, 400
SEEDS = [42, 5, 725, 1337, 99]      # FIXED. never change mid-run.

def optimize(stage):                 # run separately per stage profile
    population   = [champion(stage)] + [mutate(champion(stage), 0.3) for _ in range(POP-1)]
    hall_of_fame = [champion(stage)]
    gen = 0
    while True:
        # League training: opponents drawn from the hall of fame + scripted zoo.
        # Pure self-play co-evolves a bot that beats YOUR bot and loses to everyone else.
        opponents = sample(hall_of_fame, 2) + [sample(ZOO, 1)]

        scores = parallel_map(lambda w: evaluate(w, stage, opponents, GAMES, SEEDS),
                              population)
        elites = top_k(population, scores, ELITE)
        sigma  = max(0.05, 0.3 * (0.95 ** gen))     # anneal step size

        population = elites + [mutate(crossover(pick(elites), pick(elites)), sigma)
                               for _ in range(POP - ELITE)]

        if passes_gate(elites[0], champion(stage), games=1000):   # Part 2, same gate
            promote(stage, elites[0])
            hall_of_fame.append(elites[0])
            save(f"tune/hall_of_fame/{stage}_gen{gen}.json")
            append_changelog(stage, gen, elites[0], scores[0])
        gen += 1
```

Four details that matter more than the algorithm:

- **Fixed seed list.** Changing `SEEDS` mid-run makes every previous number incomparable.
- **Hall of fame, not pure self-play.** The single most important structural choice here.
- **Elitism.** The champion is always in the population, so the loop cannot get worse.
- **Tune each stage independently.** Sharing parameters is what makes a snake mediocre everywhere.

**The inner loop may only tune documented parameters in `config/*.json`. It must never modify
rules, never edit test baselines, and never touch `internal/rules/`.**

Run it overnight on every spare core while a human sleeps in shifts.

---

## Part 4 — Outer loop (paste into Claude Code)

```
You are improving a Battlesnake in a live competitive hackathon.
Read CLAUDE.md fully before your first action. Obey it exactly.
Run this cycle repeatedly until a STOP condition triggers.

=== CYCLE ===

1. BASELINE
   Run the null test:
     cd tools/arena && go run . --profile-a ../../config/qualifying.json \
        --profile-b ../../config/qualifying.json --games 500 --paired \
        --seeds 42,5,725,1337,99
   It MUST report no significant difference. If it does not, the arena is
   non-deterministic: stop and fix that first — every later number is fiction.

   Then run the full per-stage report (qualifying / royale / duel, reported
   SEPARATELY, never blended) and record it in CHANGELOG.md.

2. DIAGNOSE
   Run 200 games per stage and collect every loss. Extract the board state at
   the fatal turn. Cluster deaths by cause:
     trapped/self-collision | head-to-head lost | starvation |
     hazard | timeout | other
   Report the distribution per stage. The largest bucket is the target.
   Save the three most representative losses as fixtures in testdata/fixtures/.

3. HYPOTHESISE
   Propose EXACTLY ONE change addressing the largest bucket, as a falsifiable
   prediction, e.g.:
     "Trapped deaths are 41% of qualifying losses. Raising SafeCavernRatio
      from 1.8 to 2.4 should cut them below 30% and gain >0.4 pts/game."
   Check CLAUDE.md §7 first — a proven technique from a snake that placed
   beats an original idea, and several are not yet implemented.

4. IMPLEMENT
   git checkout main && git pull
   git checkout -b improve-NNN-short-name
   Make ONE change. Never refactor and change behaviour together.
   Add a golden test if it touches any rule in CLAUDE.md §2.

5. TEST
   Run the full gate from OPTIMIZATION_LOOP.md Part 2, on the TARGET STAGE,
   with 500 paired games on the fixed seed set.

   If it passes:
     go build ./... && go test ./...
     git add -A && git commit -m "perf: <change> (+X.XX pts/game, p=0.0YY)"
     git push -u origin improve-NNN-short-name
     gh pr create --title "..." --body "<hypothesis / measured effect / p-value / game count>"
     gh pr merge --squash --delete-branch
     git checkout main && git pull
     git tag -f known-good && git push -f origin known-good

   If it fails:
     Record the failure in CHANGELOG.md, then abandon the branch:
       git checkout main && git branch -D improve-NNN-short-name
     NEVER use git checkout . or git reset --hard.

6. RECORD
   Append to CHANGELOG.md whether promoted or reverted: hypothesis, measured
   effect with p-value and game count, decision, one-line reason.
   A reverted change is as valuable to log as a promoted one — it stops the
   loop retrying the same idea.

7. LOOP
   Return to step 2.

=== STOP ===
  - three consecutive cycles fail to promote, OR
  - the largest death bucket is under 20% of losses (deaths are now diffuse,
    so no single high-value target remains), OR
  - a human declares feature freeze.

=== HARD RULES ===
  - One change per cycle. Two means you cannot attribute the result.
  - Never promote on a "looks better" judgement. Only the Part 2 gate.
  - Never delete a fixture.
  - Never touch internal/rules/ without re-running the CLI differential tests.
  - Never let cmd/server import BattlesnakeOfficial/rules (CLAUDE.md §8).
  - If a cycle exceeds 20 minutes, cut the game count, not the gate.
```

---

## Part 5 — Priority queue for the outer loop

Work down this list before inventing anything. Each entry is proven in a snake that placed, or
follows from verified engine source.

| # | Change | Source | Targets |
|---|---|---|---|
| 1 | Time-aware occupancy (`freeAt`, not blocked) | snork | trapped deaths |
| 2 | Health carried through the fill | snork | starvation |
| 3 | Bounded fill at `k × length` | calvinl4, robosnake | timeouts, enables depth |
| 4 | Lexicographic hunger override | coreyja | starvation |
| 5 | Cut-cell / articulation penalty | — | trapped deaths |
| 6 | Locality masking beyond `depth × 2` | m-schier | depth |
| 7 | PV move ordering | coreyja | depth |
| 8 | Royale centre pull + four-way shrink robustness | engine source R8 | hazard deaths |
| 9 | Hazard-as-weapon positioning | engine source R5/R6 | bracket win rate |
| 10 | Forced-elimination detection | bookworm, snork | kill rate |
| 11 | Size advantage decaying with turn | snork | late game |
| 12 | Compactness penalty | rare | 19×19 self-collision |
| 13 | Ensemble reweighting from observed behaviour | — | over-caution |
| 14 | Death ranking (h2h > starve > self) | bookworm | placement points |
| 15 | Bitboard representation | — | depth, 3–5× throughput |

**Item 15 last, deliberately.** It is the most enjoyable item and the least likely to change
placement. Optimising a wrong evaluator just reaches the wrong answer faster. The recurring
finding across every post-mortem read for this project is that the heuristic, not the search
speed, was the bottleneck.

---

## Part 6 — Four ways the loop will mislead you

**Self-play overfitting.** The population co-evolves to beat itself and collapses against a
different style. Mitigated by the hall of fame plus the zoo, but check periodically: does the
champion still beat `FoodGreedy` and `HeadHunter` by the margin it did ten generations ago?

**Arena/engine divergence.** Largely solved by having `tools/arena` import the official AGPL
rules directly (CLAUDE.md §8) — but our `internal/rules` resolver is still used inside TVAE.
Run the CLI differential tests after **every** change to it, without exception.

**Optimising the wrong stage.** Always read the per-stage breakdown. The bracket is best-of-1,
so a bracket regression is worse than an equivalent qualifying gain.

**Latency blindness.** The arena has zero network latency; real games have 100–200 ms of it and
concurrent games contend for CPU. A configuration optimal at 350 ms may time out at 150 ms.
Re-run the sweep with `--budget 150` periodically and confirm the ranking holds.
