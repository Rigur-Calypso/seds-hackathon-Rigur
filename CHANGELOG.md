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
