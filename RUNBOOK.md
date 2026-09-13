# RUNBOOK — operating the snake on the day

Everything here is free tier, no card. Mac M3 locally.

## 0. Facts to have on a sticky note

| Item | Value |
|---|---|
| Repo | https://github.com/Rigur-Calypso/seds-hackathon-Rigur (private) |
| Primary host | Render free web service, native Go runtime, auto-deploy from `main` |
| Build / start | `go build -o app ./cmd/server` / `./app` |
| Backup host | Koyeb free service (same repo, same commands) |
| Emergency | `cloudflared tunnel --url http://localhost:8080` from the Mac |
| Keep-warm owner | **(write a name here)** — UptimeRobot 5-min monitor + GitHub Actions `keepwarm.yml` |
| Known-good tag | `known-good` — always the last build that passed every gate |

## 1. Health check (do this before every round)

```bash
URL=https://YOUR-SERVICE.onrender.com
curl -s -o /dev/null -w "%{http_code} %{time_total}s\n" "$URL"      # expect 200, < 1s
curl -s "$URL" | jq .                                                  # apiversion "1", version = short commit
```
Also open `$URL` from a phone **on mobile data**. "Works on localhost" is not "works when the
tournament calls it".

If `time_total` is 30s+, the service slept: keep-warm is broken. Fix that before anything else.

## 2. Redeploy

Render auto-deploys every push to `main` (~1–2 min). To force: Render dashboard → service →
**Manual Deploy → Deploy latest commit**. The server handles SIGTERM and finishes in-flight moves.

Never deploy while a match is running.

## 3. Revert to known-good (bad deploy)

```bash
git fetch --tags
git checkout main && git pull
git revert --no-edit <bad-sha>          # never git reset --hard, never git checkout .
git push origin main                    # Render redeploys
```
If several commits are bad, revert each (newest first), or in Render: **Rollback** to the deploy
whose commit equals `git rev-parse known-good`.

## 4. Switch to Koyeb (Render is down)

1. Koyeb dashboard → the pre-created service → confirm it is **Healthy** and on the same commit.
2. `curl` the Koyeb URL as in §1.
3. play.battlesnake.com → your snake → **Edit** → replace the URL → save.
4. Run one practice game against yourself.
Takes ~30 seconds if the service already exists. Create it **before** the tournament.

## 5. Emergency tunnel (both hosts down)

```bash
cd ~/Desktop/seds-hackathon
go build -o app ./cmd/server && PORT=8080 ./app &
brew install cloudflared   # once
cloudflared tunnel --url http://localhost:8080
```
Register the printed `https://….trycloudflare.com` URL. Venue wifi latency comes out of the
500 ms budget; the server measures it (`overhead_ms` in logs) and shrinks its search budget.
Keep the laptop plugged in and awake (`caffeinate -dimsu &`).

## 6. Reading logs

Render dashboard → Logs. Every line is JSON.

| Event | Meaning |
|---|---|
| `start` | all parsed settings (R10): check `shrinkEveryNTurns`, `hazardDamagePerTurn`, `timeout`, `stage` |
| `move` | `reason` (`tvae`, `duel`, `forced`, `fallback`, `no_legal`, `panic`), `elapsed_ms`, `budget_ms`, `overhead_ms`, `scores` |
| `end` | `result`; on a loss, `fatal_board` in fixture DSL |
| level WARN `move` | a panic or a response later than budget+50 ms — investigate |

## 7. Turning a loss into a fixture

1. Copy `fatal_board` from the `end` log line.
2. Save it as `testdata/fixtures/<short-name>.txt`, add `expect <move>` / `reject <move>`.
3. `go test ./internal/decide/ -run TestFixtures`
4. Never delete a fixture.

During the unscored practice window: collect fixtures, **do not change code**.

## 8. Local game against the official engine

```bash
export PATH="$HOME/go/bin:$PATH"
go build -o app ./cmd/server && PORT=8080 ./app &
battlesnake play -W 11 -H 11 --name me --url http://localhost:8080 -g standard --browser
battlesnake play -W 11 -H 11 -n a -u http://localhost:8080 -n b -u http://localhost:8080 -n c -u http://localhost:8080 -n d -u http://localhost:8080 -g royale --browser
battlesnake play -W 19 -H 19 -n a -u http://localhost:8080 -n b -u http://localhost:8080 -g royale --browser
```

## 9. Arena (in-process, exact official rules, no HTTP)

```bash
cd tools/arena
go run . --rules standard --snakes 4 --games 200            # qualifying report
go run . --rules royale --snakes 4 --games 200              # bracket report
go run . --rules royale --width 19 --height 19 --snakes 2 --games 200   # final report
go run . --profile-a ../../config/qualifying.json --profile-b ../../config/qualifying.json --games 500 --paired   # null test
```

## 10. Rehearsal log

| Drill | Date/time | Result |
|---|---|---|
| Redeploy from `main` | | |
| Revert a commit with `git revert` | | |
| Koyeb switch | | |
| Tunnel from Mac | | |
| 20-minute idle, then `curl` < 1s | | |
