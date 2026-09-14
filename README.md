# Rigur Battlesnake

A [Battlesnake](https://docs.battlesnake.com) written in Go. It plays every official ruleset —
standard, royale, constrictor and wrapped — on any board up to 25×25, and never returns anything
but one of the four moves.

Built for the SEDS Celestia hackathon, then reworked into a deep-search engine ("v2"). The rules it
relies on are verified against the official engine source and written down in
[`CLAUDE.md`](CLAUDE.md) §2; the full development record is in [`DEVLOG.md`](DEVLOG.md).

## How it decides a move

```
request ─► parse + validate (settings and deadline from the request, R10)
        ─► fallback move (lexicographic safety chain, < 1 ms, always computed first)
        ─► engine "v2": deep search on the fast state        (default)
             └─ position does not fit (> 8 snakes)? ─► engine "v1": TVAE + duel search
        ─► move, structured log line, X-Snake-Decision header
```

A panic, a parse error or a missed deadline returns the fallback move; the server never answers 5xx.

### v2 engine

| Piece | Package | What it does |
|---|---|---|
| Fast state | `internal/sim` | One position in flat arrays; bodies are ring buffers. `Make`/`Unmake` apply and revert one simultaneous turn in place in the engine's exact order (movement → starvation → hazard → feeding → eliminations → constrictor growth) in ~65 ns with no allocation. Incremental 64-bit hash. |
| Temporal Voronoi fill | `internal/sim/fill.go` | One lockstep BFS from every head: time-aware tail release, health carried through storms and food, contested cells never assigned by index, cut-cell (articulation) analysis. ~2 µs on 11×11. |
| Search | `internal/brain` | Iterative-deepening alpha-beta over whole turns. Our move, then the joint reply of the opponents close enough to matter (the others play one predicted move), every joint action resolved with the exact rules. Transposition table, history and killer ordering, extensions near equal-or-longer heads, node budget and cooperative deadline. |
| Evaluation | `internal/brain/eval.go` | Territory share, trapped and sealable space, exits, hunger (storm-aware on hazard boards), growth, length lead, health, royale centre and shrink safety. Deaths and wins are ordered terminal bands: a loss that takes an opponent along, or comes later, is worth more. |
| Sealed endgames | `internal/brain/endgame.go` | When no opponent can ever reach our space, a survival search (Warnsdorff ordering) keeps only the moves that last longest. |

Correctness is checked by differential tests: `sim.Make` against our clean-room resolver
(`internal/rules`, 10 000+ random turns) and against the official AGPL engine
(`tools/arena`, every ruleset). The v2 leaf evaluator is tested to equal v1's heuristic term for
term, so v2 differs from v1 by search, not by accident.

## Strength

Measured in the in-process arena with the official rules, paired seeds (common random numbers),
2 000 search nodes per move (about what Render's free tier allows). Points are the qualifying
placement table 10/6/3/1.

| Stage | Opponents | v1 (previous bot) | v2 | Difference |
|---|---|---|---|---|
| Qualifying, 4 snakes 11×11 | three v1 bots, 200 games | 4.82 pts, 22.5 % wins | **8.49 pts, 70.0 % wins** | +3.67 pts, p < 10⁻²⁵ |
| Qualifying, 4 snakes 11×11 | scripted zoo, 500 games | 8.77 pts, 84 % wins | **9.55 pts, 94 % wins** | +0.78 pts, p < 10⁻⁶ |
| Bracket royale, 4 snakes 11×11 | three v1 bots, 200 games | 4.86 pts, 22.5 % wins | **8.21 pts, 60.5 % wins** | +3.35 pts, p < 10⁻²⁴ |
| Constrictor, 4 snakes 11×11 | three v1 bots, 100 games | 4.72 pts, 3 % wins | **7.21 pts, 55 % wins** | +2.49 pts, p < 10⁻⁷ |
| Final, 1v1 royale 19×19 | v1 with its depth-4 duel search, 100 games | 7.86 pts, 45 % wins | **8.38 pts, 58 % wins** | +0.52 pts, p = 0.026 |

More results, including the 19×19 final and every experiment that was rejected, are in
[`DEVLOG.md`](DEVLOG.md) §11.

## Run it

```bash
go run ./cmd/server                       # listens on :8080 ($PORT on hosts)
go vet ./... && go test ./...             # unit, golden, property, fuzz-seed and fixture tests
(cd tools/arena && go test ./...)         # differential tests against the official rules
```

Play a local game with the official CLI (see [`RUNBOOK.md`](RUNBOOK.md) §8):

```bash
battlesnake play -W 11 -H 11 -n me -u http://localhost:8080 -n me2 -u http://localhost:8080 -g standard --browser
```

Measure a change in the arena (see [`RUNBOOK.md`](RUNBOOK.md) §9):

```bash
cd tools/arena
jq '. + {"searchExtensions": 3}' ../../config/qualifying.json > /tmp/cand.json
go run . --opponents v2 --nodes 2000 --games 200 --profile-b /tmp/cand.json
```

## Configuration

Profiles live in `config/{qualifying,royale,duel,constrictor}.json` and are embedded in the binary;
each lists only what differs from `internal/config.Defaults()`. The stage is read from the request
(`internal/stage`). Unknown keys are rejected, so a typo cannot silently fall back to a default.

| Key | Meaning |
|---|---|
| `engine` | `"v2"` (deep search) or `"v1"` (TVAE envelope + duel search) |
| `cpuCapMs` | compute cap per move; the deadline is `min(timeout − network margin, cpuCapMs)` |
| `searchNodes` | node cap per move, 0 = deadline only |
| `searchMaxAdv`, `searchAdvSlack` | how many nearby opponents choose adversarial replies, and how near they must be |
| `searchExtensions` | extra turns allowed on a path when an equal-or-longer head is within two cells |
| `searchShrinkPessimistic` | future royale shrinks hazard the union of all four possible rings |
| `searchEndgame`, `endgameHorizon`, `endgameNodes` | sealed-region survival filter |
| `hazardHunger` | storm-aware hunger on hazard boards |
| `w*` weights | evaluation terms, shared by both engines |

## Deploying

Render's free web service with the native Go runtime (`go build -o app ./cmd/server`, start `./app`),
auto-deploying `main`. Setup, keep-warm and failover are in [`SETUP.md`](SETUP.md) and
[`RUNBOOK.md`](RUNBOOK.md). Check what the live snake is doing:

```bash
curl -s -D - -o /dev/null -X POST -H 'Content-Type: application/json' \
  --data @testdata/payloads/royale19_cli_turn.json https://YOUR-SNAKE.onrender.com/move | grep X-Snake-Decision
```

Search strength scales with CPU: Render's free tier is about a tenth of a core. Any host with a
full core gives the search several times more nodes per move.

## Licence boundary

`tools/arena` imports the official rules package (AGPL-3.0) as a local test oracle. The deployed
server never imports it; CI checks `go list -deps ./cmd/server`. See [`CREDITS.md`](CREDITS.md).
