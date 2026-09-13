# CREDITS

Every non-obvious idea below was **reimplemented from the idea — no code was copied**.
Several sources carry no licence (all rights reserved) and two are GPL; reading an idea
and writing our own implementation is the only thing we did with any of them.

## Rules oracle

| Source | Licence | How it is used |
|---|---|---|
| [BattlesnakeOfficial/rules](https://github.com/BattlesnakeOfficial/rules) v1.2.3 | AGPL-3.0 | **Imported only by `tools/arena`** (separate Go module, never deployed) as the exact production ruleset and differential oracle. Read (not copied) to verify CLAUDE.md §2. `cmd/server` never links it — enforced in CI with `go list -deps`. |
| Official Battlesnake CLI (`battlesnake play`) | AGPL-3.0 | Local games and captured real request payloads (`testdata/payloads`). Not linked. |

## Strategy ideas

| Idea | Source | Where |
|---|---|---|
| Time-aware occupancy (`freeAt`), health carried through the fill, food-on-path delays own tail | L4r0x/snork (MIT) | `internal/voronoi/temporal.go` |
| Size advantage decaying with turn; `sqrt(health)` shape | snork | `internal/eval/score.go` |
| Area as a ratio, never a count | coreyja, m-schier, bookworm | `internal/eval/score.go` |
| Terminal scores as ordered bands; losses ordered by opponents alive | coreyja/battlesnake-rs | `internal/eval/score.go` |
| Death ranking: head-to-head > starvation > body > self | bookworm | `internal/eval/score.go` |
| Locality masking of distant opponents | m-schier (GPL — idea only), bookworm | `internal/envelope/tvae.go` |
| Contested tiles go to the longer snake ("diamond flooder") | locality-limited MaxN paper (dl.gi.de) | `AttackCellWeight` in `internal/eval/score.go` |
| One BFS outward from all food; lexicographic fallback chain | TheApX/battlesnake-hungry (MIT) | `internal/legal/legal.go`, `internal/fallback` |
| Alpha-beta + dual flood fill for duels | smallsco/robosnake (MIT) | `internal/duel/search.go` |
| Minimax / paranoid variants for Battlesnake | coreyja, "Minimax in Battlesnake" | `internal/duel/search.go` |
| ASCII board states for unit tests | mike-anderson/snek-spec (idea) | `internal/fixture` DSL |
| Regression tests from real game states | jfgodoy/battlesnake-tester (idea) | fatal-board logging in `/end` |
| CVaR risk aggregation, TVAE envelope, metamorphic tests, cut-cell penalty | project planning review | `internal/envelope`, `*_test.go`, `internal/voronoi/cutcells.go` |

## Not referenced

`JaxHodg/battlesnake` — its README claims minimax; the code contains none (CLAUDE.md §7).
