# Improvement proposals — awaiting approval

Status: **PROPOSED, nothing implemented.** Written 13 Sep 2026, 08:30 IST, against live `known-good`
`82cb73e`. Every proposal is an **addition**: new code behind a config flag that ships **off**,
measured in the arena, checked live with the `X-Snake-Decision` header, then switched on with a
one-line config PR. Switching it off again is the rollback.

---

## 1. Where the snake actually loses (measured before writing this)

A diagnostic harness replayed arena games with the official rules and recorded every decision our
snake made. For each loss it asked two questions: was the fatal move itself a mistake, and how many
turns before death did the snake last have a real choice (≥ 2 moves that were safe, not a losing
head-to-head, and not into a dead end)?

| | Qualifying 11×11, 4 snakes | Royale 11×11, 4 snakes | Qualifying vs 3 copies of itself | Constrictor 11×11 |
|---|---|---|---|---|
| Games / losses | 500 / 84 | 300 / 46 | 200 / 137 | 100 / 52 |
| Fatal move was a mistake (a safe option existed) | **0** | **0** | **0** | **0** |
| Last real choice 1–3 turns before death | **59 (70 %)** | **35 (76 %)** | 80 (58 %) | 3 (6 %) |
| Last real choice 4–10 turns before | 24 | 11 | 40 | 28 |
| Last real choice > 10 turns before | 1 | 0 | 17 | 21 |
| Evaluator already saw the loss at that last choice | 0 | 0 | 3 | 0 |
| Loss causes | head-to-head 45 · starvation 19 · body 13 · self 7 | head-to-head 31 · storm 9 · starvation 5 · body 1 | head-to-head 83 · self 33 · body 19 · starvation 2 | self 38 · body 13 · head-to-head 1 |

**What this says**

1. The snake never blunders on the last move. Every death was already sealed by an earlier decision.
2. In the main tournament the sealing decision is usually **1–3 turns** earlier, and the evaluator
   never saw it coming. One-turn lookahead is the ceiling now: the top lever is *seeing 2–3 turns
   ahead when it matters*.
3. Starvation is **23 %** of qualifying losses — a second, cheaper target.
4. Constrictor losses are **strategic** (last real choice usually ≥ 6 turns earlier): more tactical
   lookahead will not fix them; a space-filling planner will.
5. Against copies of itself (the closest thing we have to strong opponents) it wins 16 % of 4-snake
   games and dies in 69 %: the zoo flatters the bot, so stronger test opponents matter too.

Live constraint that shapes every proposal: Render's free tier is ~0.1 CPU. Measured live, a 4-snake
move is p99 137 ms and a 19×19 1v1 move p99 307 ms. Anything added must fit in a few milliseconds of
CPU per move or degrade to today's move.

---

## 2. Proposals

Each has: the problem it targets, what gets added, how it is proven (the gate), an honest effort
estimate, and the risk. Impact numbers are **estimates to be tested**, not promises. For scale: in
the current qualifying arena a lost game averages ~2.3 points and a win 10, so every 100 losses
avoided in 500 games is worth roughly +1 point per game.

### Tier 1 — targets the measured losses directly

#### P1 · Danger-triggered second look ("TVAE-2")
- **Problem:** 70–76 % of main-tournament losses were sealed 1–3 turns before death, invisible to one-turn lookahead.
- **Addition:** after today's TVAE finishes (it always runs first), *only when a candidate move looks
  dangerous* — few viable exits, every exit contested by an equal-or-longer head, or reachable area
  below twice our length — resolve a second turn for that move: our best reply against each nearby
  opponent's two most likely replies. Hard cap on evaluations (~1 000, a few ms of CPU on Render) and
  the existing deadline; if it cannot finish, the TVAE move stands. New file
  `internal/envelope/deepen.go`, flags `deepenEnabled`, `deepenMaxEvals`.
- **Gate:** paired arena — qualifying 500 games and royale 300 games, ≥ +0.3 pts/game or ≥ +3 pp wins
  with p < 0.05, zero timeouts; then live p99 latency unchanged via the header.
- **Estimate:** avoid a third to a half of the 1–3-turn losses ⇒ roughly **+0.2 to +0.5 pts/game** in
  qualifying and +3 to +6 pp bracket wins against the zoo; more against stronger fields.
- **Effort:** ~3 h. **Risk:** CPU on Render — bounded by the evaluation cap and the TVAE-first rule.

#### P2 · Learn each opponent during the game
- **Problem:** the opponent model mixes four fixed policies with the same weights for every snake.
  Real opponents have styles; one wrong weight hides a threat in the CVaR tail or wastes lookahead.
- **Addition:** every turn, infer each opponent's actual last move from its head position, score which
  policy predicted it, and update that snake's policy weights (per game, with a light prior carried
  by snake **name** across games — R11). If an opponent's reported latency is near the timeout, raise
  the weight of "continue straight", which is what the engine plays when a snake times out. All
  legal actions stay in the envelope; only weights move. New file `internal/opponent/learner.go`,
  flags `learnOpponents`, `timeoutPrior`.
- **Gate:** paired arena vs zoo (stable styles, so it should learn) and vs copies of itself (must not regress).
- **Estimate:** +0.1 to +0.3 pts/game on its own; also makes P1 cheaper and sharper.
- **Effort:** ~2 h. **Risk:** low — weights only, never prunes a legal threat.

#### P3 · Only count food we can actually win
- **Problem:** starvation is 19 of 84 qualifying losses. Hunger urgency uses the distance to the
  nearest food we can *reach*, even when an equal or longer snake gets there at the same time or first.
- **Addition:** the Voronoi fill already knows arrival order; expose "distance to food we reach
  strictly first, or tie while longer" and drive hunger urgency from it. Flag `winnableFoodHunger`.
- **Gate:** paired qualifying 500: starvation deaths down, pts/game up (p < 0.05), no rise in head-to-head deaths.
- **Estimate:** halve starvation losses ⇒ about **+0.1 pts/game**. Cheap.
- **Effort:** ~1 h. **Risk:** low.

#### P4 · Space-filling planner for sealed regions
- **Problem:** constrictor (side event) wins 48 % of arena games; 73 % of its losses are
  self-collision with the real choice ≥ 6 turns earlier. The same situation — our region sealed off
  from every opponent — decides many late royale and 1v1 games.
- **Addition:** when no cell of our reachable region can be reached by any opponent, stop estimating
  and plan: a time-capped longest-path search (depth-first, most-constrained-neighbour first) picks
  the move that survives the most turns; with regions still touching, keep today's evaluator. New
  package `internal/endgame`, flag `fillPlanner`.
- **Gate:** constrictor arena win rate from 48 % to ≥ 60 %; paired royale and duel must not regress.
- **Effort:** ~3 h. **Risk:** CPU in long constrictor games — capped and falls back.

### Tier 2 — supporting additions

#### P5 · Tournament-day monitor endpoints
- **Problem:** during the tournament the only window into the snake is the Render log view.
- **Addition:** `GET /stats` (games, wins/losses, loss causes, latency p50/p99, network overhead,
  current version) and `GET /fatal` (last 20 losing boards in fixture format, ready to paste into
  tests). The engine never calls them; nothing about opponents beyond board positions. Flag `monitorEndpoints`.
- **Gate:** server tests; open both from a phone.
- **Effort:** ~1 h. **Risk:** near zero.

#### P6 · Storm forecast in the fill (royale)
- **Problem:** the storm killed 9 of 46 royale losses. The fill charges hazard only on today's ring,
  but a path 10 turns long crosses future shrinks.
- **Addition:** at each arrival time, treat a cell as hazard if the ring could have reached it by then
  under any shrink sequence (the union of possible rings — R8, never predicting a direction). Flag `stormForecast`.
- **Gate:** paired royale 300: storm deaths down, wins up, duel 19×19 not worse.
- **Effort:** ~1.5 h. **Risk:** may make the snake too centre-hugging — the gate decides.

#### P7 · Tougher test opponents + automatic tuning
- **Problem:** we beat the scripted zoo 84–85 % of the time, so small real improvements are hard to
  see. Against copies of itself the picture is very different (see §1).
- **Addition:** commit the diagnostic harness as `arena --diagnose`; add a gauntlet of champion
  variants and a hall of fame; implement the per-stage (μ+λ) tuner from `OPTIMIZATION_LOOP.md`
  Part 3 to run on the Mac's idle cores.
- **Gate:** the null test still exactly zero; tuned profiles pass the usual paired gate per stage.
- **Effort:** ~3 h plus overnight CPU. **Risk:** over-fitting to ourselves — mitigated by keeping the zoo in the league.

#### P8 · Borrow the M3's CPU without trusting it
- **Problem:** Render gives ~0.1 CPU; the Mac has 10 cores sitting idle.
- **Addition:** Render stays the registered URL and keeps answering exactly as now. For each move it
  computes TVAE locally (< 1 ms), forwards the same request to the Mac over a Cloudflare quick tunnel
  with a hard deadline (~120 ms), and plays the Mac's deeper answer only if it arrives in time and is
  legal. A shared secret header keeps anyone else from using the Mac endpoint.
- **Gate:** step 1 is a timing probe (Render → Mac round trip). Only if that is comfortably under the
  deadline, arena-and-live verify that the move never arrives later than today's p99.
- **Effort:** ~2 h plus the laptop staying awake on venue wifi. **Needs from you:** permission to
  install `cloudflared`. **Risk:** an extra network hop — bounded by the deadline, never worse than today's move.

### Tier 3 — research bets

#### P9 · Simultaneous-move duel search
Today's 1v1 search assumes the opponent sees our move first (paranoid). At depth 3 that lost to plain
TVAE 58.5–41.5; at depth 4 it won 55–45. Solving each 3×3 simultaneous choice as a small matrix game
instead should be less timid and may win at shallower depth, which is what Render can afford.
Gate: head-to-head 19×19 vs today's search. ~2–3 h.

#### P10 · Learned "doomed within 3 turns" signal
Label ~50 000 arena positions by whether our snake died within 3 turns, fit a logistic model on the
features the fill already computes, and add it as one extra risk term (a dot product per outcome).
Cheap at runtime; the risk is that it learns the zoo, not real opponents. ~3 h.

#### P11 · GitHub Codespaces as the P8 compute node
Free accounts get 120 core-hours a month (60 h on a 2-core machine) and can make a forwarded port
public. It stops after an idle timeout (default 30 min, configurable) unless there is terminal output —
our per-move log lines count, idle gaps between games may not. Better as P8's helper than as the
primary host. ~1 h to try.

---

## 3. Checked and dropped

| Idea | Why not |
|---|---|
| Host on Hugging Face Spaces (2 vCPU free) | Since June–August 2026 new free accounts cannot run Docker/compute Spaces without PRO. |
| Remove royale storm penalties in constrictor on hazard maps | Tested on `hz_scatter`: 51 vs 51 wins in 100 games — no effect. |
| Parity rule (head-to-head only possible at even head distance) | Already implicit in the exact first-turn resolution and the grid fill. |
| Bigger growth drive, stronger exit penalty, flatter opponent weights, pessimistic shrink | Rejected earlier by paired tests (DEVLOG §7). |
| Opening book | Only 1 of 84 qualifying losses had its last real choice more than 10 turns back; early game is not where points go. |

---

## 4. Recommended order

1. **P1** — biggest measured target, main tournament.
2. **P3** — one hour, a quarter of qualifying losses.
3. **P2** — sharpens P1's branches and the CVaR weights.
4. **P5** — visibility for tournament day.
5. **P6** — royale storm deaths.
6. **P4** — constrictor side event and sealed endgames.
7. **P7**, then **P8** — measurement depth and compute headroom.
8. **P9–P11** — only after the above.

Each proposal is one branch and one PR: flag off by default → arena gate → merge → live check → a
separate one-line PR to switch it on.

## 5. What I need from you

- Your approval per proposal (the approval page saves your choices).
- The link to the practice game you played on play.battlesnake.com: it shows real engine latency and
  lets me test replaying real games into fixtures.
- Where the tournament engine runs (hosted play.battlesnake.com, or a laptop at the venue) — it
  decides whether P8 is worth it.
