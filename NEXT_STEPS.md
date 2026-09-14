# NEXT STEPS — after the v2 engine (post-hackathon)

Written 14 Sep 2026. The hackathon is over; the target is Battlesnake **standard 11×11 (4 snakes)**
and **11×11 duels**. `main` runs the v2 engine (DEVLOG §11). Royale and constrictor still work but are
not tuned.

## A. Check the deploy (≈5 min, after every merge to `main`)

```bash
URL=https://seds-hackathon-rigur.onrender.com
curl -s "$URL" | jq .            # "version" = first 7 characters of the latest main commit
curl -s -D - -o /dev/null -X POST -H 'Content-Type: application/json' \
  --data @testdata/payloads/standard_cli_turn.json "$URL/move" | grep X-Snake-Decision
```

Expect `reason=search` with `depth` ≥ 2 and `nodes` > 0. `reason=tvae` means the v1 engine answered
(only expected with more than eight snakes).

## B. Compete

1. play.battlesnake.com → your snake → make sure the URL is the Render URL.
2. Join the **Standard** and **Duels** arenas.
3. Keep-warm stays on (UptimeRobot 5-minute monitor + `keepwarm.yml`): Render free sleeps after 15 minutes.

## C. Turn every real loss into a test

Render → Logs → the `end` line of a lost game has `fatal_board`. Save it as
`testdata/fixtures/<name>.txt`, add `expect <move>` or `reject <move>`, run
`go test ./internal/decide/ -run TestFixtures`. Never delete a fixture. `tools/arena --trace SEED`
replays an arena game decision by decision.

## D. Hosting is the strength limit

Render's free tier is about a tenth of a CPU. v2 plays better the more positions it searches per move
(10 000 vs 2 000 nodes: +0.54 pts in 4-snake self-play, DEVLOG §11.8). Any free host that gives a full
core — or `cloudflared tunnel` from the Mac during a session (RUNBOOK §5) — makes the same code
stronger. A GPU does not help: the engine is CPU search, not a neural network.

## E. Engine work, in expected-value order

1. **Self-coiling in long 4-snake games** — the largest loss bucket in self-play (37 of 70). Fatal boards
   show zigzag coils inside our own territory. Ideas: a coil/"compactness" penalty, tail-reachability in
   the leaf, or a survival check against *adversarial* (not predicted) opponents — the predicted version
   (`endgameAlways`) traded self-collisions for head-to-heads and was rejected.
2. **Food drive** — `foodDeficitScale` 3 won in 4-snake self-play (+1.17) but leaned negative vs v1
   and the zoo, so it stays off (DEVLOG §11.9). A version that only applies in duels, or only when an
   opponent is two or more longer, is the next thing to try.
3. **Speed** — the fill is ~90 % of search time; a bitboard flood, or skipping articulation in inner
   leaves if self-play shows no loss, would buy depth on Render.
4. **Duels** — head-to-head (25) and self-collision (22) losses in duel self-play, sealed 4–10+ turns
   earlier: deeper search and a duel-specific profile (`engine` weights for two snakes).
5. Transposition-table reuse between turns of the same game; Lazy SMP if hosting ever has several cores.

Every change: own branch and PR, behind a flag, paired arena gate (`promotionGate` must pass) on
4-snake and duels, vs v1 and v2 self-play and the zoo; documented in DEVLOG and CHANGELOG.
