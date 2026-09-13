# KICKOFF.md — exactly what to paste into Claude Code

Finish `SETUP.md` first. Then `cd ~/Desktop/seds-hackathon && claude`.

---

## Message 1 — orientation (paste verbatim)

```
Read CLAUDE.md, BUILD_PLAN.md and OPTIMIZATION_LOOP.md in this repository in full
before doing anything. Read docs/handbook.pdf too.

Then, without writing any code yet, report back:

1. A one-paragraph summary of the objective and why the three tournament stages
   need different risk postures.
2. The eleven engine rules R1-R11, in your own words, flagging which two are the
   easiest to get wrong in a parser.
3. Anything in those documents that is ambiguous, contradictory, or that you
   believe is factually wrong. Be direct — I would rather find an error now than
   at 4am.
4. The exact git ritual you will follow for every step.

Do not start Step 0 until I confirm.
```

Read the reply properly. If it misstates a rule, correct it now — that misunderstanding will
otherwise be compiled into the resolver.

---

## Message 2 — begin

```
Confirmed. Begin STEP 0 from BUILD_PLAN.md.

Constraints:
- Go only. Mac M3 (arm64) locally, Render free tier (native Go runtime, not
  Docker) for deployment.
- Bind 0.0.0.0 and read $PORT from the environment.
- recover() middleware so no panic can escape a handler.
- Follow the git ritual: branch step-00-skeleton, commit, push, open a PR with
  gh, and stop. I will verify the deploy before you merge.

Do not proceed past Step 0's acceptance gate.
```

---

## Message 3 onward — one per step

```
Step 0's gate passed: [paste evidence — curl output from mobile data, the CLI
game result].

Merge the PR, tag known-good, then begin STEP N from BUILD_PLAN.md.
Stop at its acceptance gate and open a PR.
```

**Never let it run two steps in one go.** The gates are the whole point.

---

## After Step 12 — start the loop

Paste **Part 4 of `OPTIMIZATION_LOOP.md`** verbatim. It will run until it converges or you call
freeze.

---

## Prompts for specific situations

**When a step's gate fails:**
```
The gate failed: [exact output].
Do not change the gate. Diagnose the root cause, state it in one sentence,
then propose the smallest fix. Wait for my approval before implementing.
```

**When it wants to build something not in the plan:**
```
That is not in BUILD_PLAN.md. Either justify it against the acceptance gate for
the current step, or defer it to the optimisation loop's priority queue.
```

**When you are short on time:**
```
Time check: [X] hours remain before code freeze. Given BUILD_PLAN.md, tell me
which remaining steps you would cut and why, ranked by expected cost to our
placement. Do not implement anything yet.
```

**Before the practice window:**
```
The unscored practice window opens in 15 minutes. Deploy the known-good tag.
Verify: keep-warm active, URL reachable from outside, opponent logging enabled,
every fatal payload saved to testdata/fixtures/.
During the window we collect data only — no code changes. Confirm you understand.
```

**At feature freeze:**
```
Feature freeze. Deploy the known-good tag and confirm it is live and awake.
From now on: operations and proven rule-defect fixes only. No strategy changes,
no refactors, no "small improvements". If you believe something must change,
state the defect and the evidence first and wait for approval.
```

---

## Guard rails — watch for these

Claude Code is good at this work but has predictable failure modes. Intervene if you see:

- **Skipping a gate** because the code "looks right". The gates exist because reasoning about a snake is unreliable and measuring it is not.
- **`git checkout .` or `git reset --hard`.** Forbidden. It destroys unrelated work.
- **Hardcoding 500 ms** instead of reading `game.timeout` (R10).
- **Parsing `shrinkEveryNTurns` at the settings root.** It is nested under `royale`. Fails silently as zero.
- **Merging the body and head occupancy grids** (R3). Breaks head-to-heads you would win.
- **Detaching a goroutine** for search. Cooperative deadlines only — a detached search starves your concurrent games.
- **`cmd/server` importing `BattlesnakeOfficial/rules`.** AGPL boundary. CI catches it; do not let it be disabled.
- **Assigning equal-arrival Voronoi cells by snake index.** Systematic bias. They are contested.
- **Blending the three stage fitnesses into one number.** Hides a bracket regression behind a qualifying gain.
- **Deleting a fixture** because it now fails. The fixture is the specification.
- **Changing `SEEDS` mid-tuning-run.** Makes every prior measurement incomparable.

---

## Time-boxed fallback plan

If you fall behind, this is the cut order. Each line below is still a functioning, deployable
snake.

| If you reach hour... | Ship at least |
|---|---|
| 3 | Steps 0–3: deployed, exact legal moves, lexicographic fallback. **This alone is competitive** — a searchless 463-line rule chain reached semi-finals at a comparable event |
| 5 | + Step 4 (temporal Voronoi) and Step 5 (arena) |
| 7 | + Step 6 (TVAE, CVaR risk) |
| 8.5 | + Step 7 (Royale) — **do not skip this; two of three stages are Royale** |
| 9.5 | + Step 8 (resilience) — **never skip this** |
| 10.5 | + Steps 9–11 if green |

**Steps 3 and 8 are the two you must never cut.** Step 3 stops avoidable deaths; Step 8 stops
forfeits. Everything else is upside.
