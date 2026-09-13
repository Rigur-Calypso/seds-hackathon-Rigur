# SETUP.md — do this yourself, before Claude Code touches anything

Target: ~25 minutes. Everything here is free and requires no credit card.
Machine: Mac M3 (arm64).

---

## 1. Local toolchain

```bash
# Go — arm64 native via Homebrew
brew install go gh jq
go version        # expect go1.22+ ; anything 1.21+ is fine

# Official Battlesnake CLI (our rules oracle). Builds natively on arm64.
go install github.com/BattlesnakeOfficial/rules/cli/battlesnake@latest
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
battlesnake --help    # must print usage
```

If `battlesnake` is not found, `$HOME/go/bin` is not on your PATH — fix that before continuing,
because every acceptance gate in the build plan depends on it.

---

## 2. Repository

You already have `~/Desktop/seds-hackathon` with the poster and handbook. Turn it into the repo.

```bash
cd ~/Desktop/seds-hackathon
git init
gh auth login                 # choose HTTPS, authenticate in browser

mkdir -p docs
mv *.pdf *.jpeg *.jpg *.png docs/ 2>/dev/null

# Drop the four planning documents in the root:
#   CLAUDE.md  BUILD_PLAN.md  OPTIMIZATION_LOOP.md  KICKOFF.md

cat > .gitignore <<'EOF'
bin/
build/
*.log
replays/
profiles.json
.DS_Store
.env
EOF

git add -A
git commit -m "chore: planning documents, handbook, gitignore"
git branch -M main
git remote add origin https://github.com/Rigur-Calypso/seds-hackathon-Rigur.git
git push -u origin main
git tag known-good && git push --tags
```

**Verify:** `gh repo view --web` opens your repo and `main` shows the documents.

---

## 3. Hosting — Render (primary)

Verified current as of this event: free tier, **no credit card**, 750 hours/month, native Go
runtime. It sleeps after 15 minutes idle and takes ~60 seconds to wake, which §5 solves.

1. Sign up at render.com with your GitHub account
2. **New → Web Service** → connect `seds-hackathon-Rigur` (grant access to the private repo)
3. Settings:
   - Runtime: **Go** (not Docker — this sidesteps the arm64→x86 cross-build problem entirely)
   - Build command: `go build -o app ./cmd/server`
   - Start command: `./app`
   - Instance type: **Free**
   - Region: whichever is closest to India (Singapore if offered)
4. Auto-deploy from `main`: **on**

**Note the URL.** It looks like `https://seds-hackathon-rigur.onrender.com`.

Your server must bind `0.0.0.0` and read `$PORT` from the environment. This is in the build
plan, but if the first deploy fails, that is almost always why.

### Backup host

**Koyeb** — free tier, one service, no credit card. Set it up now while you have time, deploy
the same repo, note the URL, and leave it. Switching registration takes thirty seconds if
Render has a bad night.

### Emergency only — Cloudflare Tunnel from your Mac

```bash
brew install cloudflared
cloudflared tunnel --url http://localhost:8080
```

Gives an instant public HTTPS URL with no account. **Do not use this as your tournament
primary** — routing through venue wifi adds latency that comes straight out of your 500 ms
budget. It is for local development against the real engine, and for the scenario where both
managed hosts fail.

---

## 4. Battlesnake account

1. Create a free account at play.battlesnake.com
2. Add your snake with the Render URL
3. Run one practice game against itself to confirm it responds
4. Submit through the event tournament page when it opens

Keep the URL stable. If it changes, update the registration immediately.

---

## 5. Cold-start prevention — do this the moment §3 works

The handbook names this as the single most common way a good bot loses a match it should have
won. Render sleeps at 15 minutes; waking takes ~60 seconds against a 500 ms limit.

**Primary — UptimeRobot** (free, 50 monitors, 5-minute interval, no card):
1. uptimerobot.com → sign up
2. Add New Monitor → HTTP(s) → your Render URL → interval 5 minutes

**Backup — GitHub Actions cron** (free for private repos). Create
`.github/workflows/keepwarm.yml`:

```yaml
name: keepwarm
on:
  schedule:
    - cron: '*/10 * * * *'
  workflow_dispatch:
jobs:
  ping:
    runs-on: ubuntu-latest
    steps:
      - run: curl -sSf --max-time 90 "${{ secrets.SNAKE_URL }}" || true
```

Then `gh secret set SNAKE_URL --body "https://your-url.onrender.com"`.

Run both. GitHub's cron is best-effort and can drift by several minutes, so it is a backup to
UptimeRobot, not a replacement.

**Verify at 20 minutes:** leave it alone for twenty minutes, then
`time curl -s https://your-url.onrender.com`. Under one second means keep-warm is working.
Thirty-plus seconds means it slept and you need to fix this before anything else matters.

---

## 6. External reachability check

"Works on localhost" is not "works when the tournament calls it."

- From your **phone on mobile data** (not venue wifi), open the Render URL in a browser. You must see your snake's info JSON.
- Also `curl -s -o /dev/null -w "%{http_code} %{time_total}\n" https://your-url.onrender.com`

---

## 7. Claude Code

```bash
cd ~/Desktop/seds-hackathon
claude
```

Claude Code reads `CLAUDE.md` automatically at the start of every session. Then follow
`KICKOFF.md`.

---

## 8. Pre-flight checklist

- [ ] `go version` and `battlesnake --help` both work
- [ ] Repo pushed to `main`, `known-good` tag exists
- [ ] Render deployed, URL noted, auto-deploy from `main` on
- [ ] Koyeb backup deployed, URL noted
- [ ] Battlesnake account created, snake registered
- [ ] UptimeRobot monitor live, GitHub Actions keepwarm live
- [ ] 20-minute idle test returns in under 1 second
- [ ] URL loads from phone on mobile data
- [ ] `cloudflared` installed for local dev

---

## 9. Questions to get answered at kickoff

Ask a mentor. Each one removes a guess from the build:

1. **Can you get one real `/move` payload?** This is the single most valuable input — it locks the parser and the fixtures against reality instead of against my reading of the source.
2. What are `shrinkEveryNTurns` and `hazardDamagePerTurn` in the Royale config?
3. Is there a turn cap? The handbook says you can win "by having the best result when time runs out," which implies one. If so, being longest at the cap matters.
4. Exactly how many qualifying rounds?
5. Is the bracket best-of-1 confirmed? (You believe yes — worth confirming, as it sets how much variance qualifying can absorb.)
