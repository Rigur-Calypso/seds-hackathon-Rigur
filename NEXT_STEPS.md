# NEXT STEPS — what YOU do now, in order

Written 07:12 IST, 13 Sep. The code is built, tested, merged to `main` and tagged `known-good`.
Everything left is a task only a human with your accounts can do. All free, no card.
Feature freeze ≈ 08:45. Tournament after the build window closes.

---

## A. Get it live (≈15 min) — do this first

> ✅ **Render is live** at https://seds-hackathon-rigur.onrender.com — verified 07:55 IST
> (version matched `known-good`; a real 4-snake CLI game against it: p99 137 ms, 0 failures).
> Steps 1–2 are done; continue from step 3.
>
> ✅ **Tuned for Render's CPU** (08:04, version `b9a95ac` = `known-good`): real games against the
> live URL — 4 snakes 11×11: p99 137 ms; 1v1 19×19 (grand-final shape, 455 turns): p50 190 ms,
> p99 307 ms, 0 failed requests. Check what the live snake is doing any time with:
> `curl -s -D - -o /dev/null -X POST -H 'Content-Type: application/json' --data @testdata/payloads/royale19_cli_turn.json https://seds-hackathon-rigur.onrender.com/move`
> and read the `X-Snake-Decision` header.

1. **Render** (skip if already connected): render.com → New → Web Service → pick
   `seds-hackathon-Rigur` → Runtime **Go** → Build `go build -o app ./cmd/server` → Start `./app`
   → Instance **Free** → Region **Singapore** → Auto-deploy **on**.
2. When it says **Live**:
   ```bash
   curl -s https://YOUR-NAME.onrender.com | jq .
   ```
   Expect `"apiversion":"1"` and `"version"` = the first 7 characters of the latest commit on `main`.
3. Open the same URL **on your phone using mobile data** (not venue wifi). You must see the JSON.
4. Keep-warm, both of these:
   ```bash
   gh secret set SNAKE_URL --body "https://YOUR-NAME.onrender.com"
   gh workflow run keepwarm
   ```
   Then uptimerobot.com → New monitor → HTTP(s) → your URL → every 5 minutes.
   > Status 08:05: `SNAKE_URL` secret is set and a manual keepwarm run succeeded (07:27). No
   > *scheduled* runs have appeared yet — GitHub cron often starts late — so the UptimeRobot
   > monitor is the one to rely on. Check with `gh run list --workflow keepwarm.yml`.
5. **play.battlesnake.com** → Create snake → paste the URL → run one practice game against
   yourself. In Render → Logs you should see `start`, then `move` lines with `"reason":"tvae"`.
6. **Backup**: koyeb.com → create one free service from the same repo, same build/start
   commands. Write both URLs in `RUNBOOK.md` §0. Do not register the backup unless Render fails.

## B. Ask a mentor (≈5 min) — each answer removes a guess

1. **Can I get one real `/move` request body?** Save it as `testdata/payloads/real_move.json` and
   run `go test ./internal/api/` — proves the parser against their engine.
2. Royale settings: `shrinkEveryNTurns` and `hazardDamagePerTurn` (we read them from every request
   anyway, but it's good to know).
3. Is there a turn cap? (If yes, being longest at the cap matters.)
4. How many qualifying rounds? Is the bracket really best-of-1?
5. Do games run on play.battlesnake.com (then Render is right) or on a laptop CLI at the venue
   (then a local tunnel may have lower latency — see RUNBOOK §5)?

## C. Practice window — collect data, change nothing

- Watch Render logs. Every lost game logs an `end` line with `fatal_board`. Copy it into
  `testdata/fixtures/<short-name>.txt`, add one line `expect <correct move>` (or `reject <bad move>`),
  run `go test ./internal/decide/`. **Never delete a fixture.**
- Check two numbers on `move` lines:
  - `elapsed_ms` should stay well under 300.
  - `overhead_ms` is the measured network cost; the budget adapts to it automatically.
- A `WARN` level `move` line means a panic or a slow response. Tell someone before changing anything.

## D. The one setting you may need to flip

Render's free tier is about 0.1 CPU. TVAE (the main evaluator) takes ~0.03 ms per move and always
runs first. The **duel search** (only when exactly 2 snakes are alive) then uses the remaining
budget and is trusted only if it finished depth ≥ 4 — otherwise the TVAE move is played. Check
`"reason"` and `"depth"` on 1v1 `move` lines: `duel` with depth ≥ 4 means the search is reaching
useful depth; `tvae` with depth 2–3 means the CPU is too slow for it and TVAE is carrying you,
which is fine. Only if you see `elapsed_ms` near 500 or `WARN` lines, set `"duelEnabled": false`
in `config/*.json` (one-line PR). Measured cost of disabling everywhere: −0.07 pts/game in
qualifying, −0.21 pts/game and −5 pp wins in the bracket — so don't do it without evidence.

## E. At feature freeze (≈08:45)

- [ ] Render is Live on the `known-good` commit (`git rev-parse --short known-good` = `version` in `curl`)
- [ ] 20-minute idle test: leave it, then `time curl -s YOUR-URL` → under 1 second
- [ ] UptimeRobot green, Actions keepwarm green
- [ ] Snake registered on the tournament page
- [ ] From now on: operations and proven rule bugs only (KICKOFF.md "At feature freeze")

## F. If anything breaks

`RUNBOOK.md`: §3 revert (`git revert`, never `reset --hard`), §4 switch to Koyeb, §5 emergency
Cloudflare tunnel from your Mac.

## G. After the event (not before)

Open ideas not yet built, in expected-value order: a 2-ply envelope for "sandwich" corridors
(the largest remaining loss bucket), reweighting the opponent ensemble by snake name, a
transposition table for the duel search, and the (μ+λ) parameter search in
`OPTIMIZATION_LOOP.md` Part 3.
