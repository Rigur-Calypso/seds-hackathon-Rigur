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
