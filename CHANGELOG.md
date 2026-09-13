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
