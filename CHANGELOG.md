# CHANGELOG

Per-stage results are reported **separately** (qualifying / royale / duel), never blended.
Every entry: what changed, gate, measured result. Reverted experiments are logged too.

## step-00-03-core — skeleton, parser, resolver, legal, fallback, TVAE, royale, duel, resilience

- Server: 4 endpoints, `0.0.0.0:$PORT`, recover middleware, JSON logs, request-derived budget.
- Parser: R10 nested `royale.shrinkEveryNTurns`, `game.timeout`, R11 latency (string or number).
- Resolver: engine pipeline with golden tests R1–R9 plus R12 (same-tick eliminations don't block)
  and stacked-hazard damage.
- Legal: three layers (blocked / head threat / hazard); lexicographic fallback < 1 ms.
- Evaluator: temporal Voronoi + cut cells, TVAE envelope with CVaR/mean/min blend, royale layer,
  1v1 iterative-deepening alpha-beta.
- Measured results: see DEVLOG.md §5.

## improve-001 — review fixes, per-stage baselines, experiment log, duel min depth

Baselines (arena, zoo, paired seeds 42,5,725,1337,99, reported separately):
- qualifying 500 games: 8.77 pts/game, 84.0 % first, 0 timeouts, p99 13.3 ms
- royale 11×11 300 games: 8.84 pts/game, 85.0 % wins, 0 timeouts, p99 42.3 ms
- duel 19×19 100 games: 93 % wins, 0 timeouts; solo royale survival 434 turns
- wall-clock 150 ms budget at concurrency 4: 0 timeouts

Experiments (full table in DEVLOG §7):
- REJECTED growth drive +0.156 pts p=0.35 · wNoSafeExit 1.5 −0.048 p=0.41 · ensUniform 1.5 −0.076 p=0.41
- NO EFFECT envelopeShrinkPessimistic (300 identical games) — option kept, default off
- REJECTED disabling duel search: qualifying −0.072 p=0.0065, bracket −0.213 p=0.0003
- MEASURED 19×19 head-to-head: TVAE beats depth-3 duel search 58.5 %, loses to depth-4 search 45 %
- PROMOTED duelMinDepth=4: TVAE first, duel move used only if depth ≥ 4 completed

Fixes: CI licence check fails closed; budget floor never exceeds half of game.timeout; Decide
keeps a completed evaluator move when the deadline expires during optional deeper search.

## improve-002 — Render CPU cap from live measurements, decision header

Live service https://seds-hackathon-rigur.onrender.com (version a6410b1), measured from India:
- 4-snake CLI game on the live URL: 1 093 moves, p50 82 ms, p99 137 ms, max 215 ms, 0 failures
- 19×19 1v1 duel search at the 220 ms cap: 376–516 ms total (throttling stalls) → timeout risk
- 19×19 at 25–60 ms budgets: p50 ~185 ms; 11×11 1v1 at any budget: p50 ~124 ms (depth 4 finishes early)

Change: `cpuCapMs` 220 → 50 in all profiles (test enforces ≤ 60); `X-Snake-Decision` response
header (reason, depth, budget, compute time). Arena baselines unchanged (deterministic mode).

## improve-003 — grand-final search stops at the proven depth

Live headers after improve-002: 19×19 1v1 always reaches `duel depth=4`, but compute was 56–137 ms
against a 50 ms budget because starting depth 5 exhausts the CPU quota (throttling). Depth 4 is
the arena-proven useful depth (beats TVAE 55–45 head-to-head); deeper was never shown to help.
Change: `config/duel.json` `duelMaxDepth` 6 → 4.

Measured after deploy (live b9a95ac): 19×19 1v1 `/move` 140–272 ms; real 19×19 CLI game on the
live URL (455 turns): p50 190, p99 307, max 414 ms, 0 failed requests.

## improve-004 — fix: `envelopeShrinkPessimistic` never reached the resolver

Found in the owner's Codex review: `envelope.Evaluate` built the shrink option but always resolved
with `ShrinkKeep`. Regression test `TestEnvelopeShrinkPessimisticReachesResolver` written first —
it failed on the old code (identical scores 0.2366) and passes after the one-line fix.

The earlier "no effect, 300 identical games" result (improve-001) was an artefact of this bug and
is withdrawn. Corrected re-run with the flag really applied:
- royale 11×11, 300 paired games: −0.07 pts/game (p = 0.47), −1.3 pp wins (p = 0.32); storm deaths
  10 → 12, starvation 5 → 8 → **not promoted** (union of four rings is too passive); default stays off
- royale 19×19 1v1, 100 paired games: identical (duel search decides almost every 1v1 move)
Replacement planned: four explicit shrink worlds combined by stage risk posture.

## improve-005 — arena and validation upgrades (no bot behaviour change)

- Arena settings as flags: `--food-spawn --min-food --hazard-damage --shrink --map --max-turns
  --tie-break length|draw`, passed to both the official engine and the request the bot parses.
- `--grid` comparisons across settings with the worst cell reported; placement scoring extracted
  and tested; paired results carry 95 % bootstrap confidence intervals.
- `--diagnose` / `--examples` decision-level loss classification in the repo; `--duel-depth` also
  lowers `duelMinDepth`.
- Adversarial opponents `pincer`, `foodbait`, `edgeherder`, `stormtrapper` (`--opponents
  adversarial|full`); the original `zoo` rotation is unchanged.
- Fuzz tests `FuzzParse`, `FuzzDecide`; property tests for duplicated tails and safe moves.

Measured: default qualifying 500 games 8.768 pts / 84.0 % (identical); `--diagnose` 80 losses,
0 avoidable, 57 sealed 1–3 turns earlier; adversarial zoo qualifying 9.22 pts / 90.3 %, royale
8.88 pts / 85.3 % (not harder than the zoo — self-play remains the strongest test).

## improve-006 — proven duel wins bypass the depth gate (test first)

`duelMinDepth` discarded a win that duel search had already proven below depth 4 (search stops once
the result is certain). Regression test `TestProvenWinBypassesDepthGate` failed on the old code
(`reason=tvae depth=1`) and passes after the fix. Added `arena --results FILE` (per-game JSON lines)
to compare two binaries game by game.

Before/after on identical seeds — bracket 300, qualifying 500, 19×19 1v1 100 games: points and wins
identical in all 900; 36 games changed, 33 of them ended sooner (19×19: 182 → 138 turns on average);
no outcome or death-cause changes. The hypothesis that this caused the 8.84 → 8.827 bracket dip is
falsified; the likely remaining cause (proven shallow losses now play the TVAE move) is worth about
one game in 300 and was not pursued.
