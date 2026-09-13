// Command arena plays Battlesnake games in-process with the OFFICIAL rules
// package (AGPL-3.0 — this module is local tooling, never served or
// distributed; CLAUDE.md §8) against our decide.Engine. No HTTP.
//
//	go run . --rules standard --snakes 4 --games 500
//	go run . --rules royale --width 19 --height 19 --snakes 2 --games 200
//	go run . --profile-a ../../config/qualifying.json --profile-b cand.json --games 500 --paired
//
// Fitness is reported per stage and never blended: mean placement points
// (10/6/3/1) for qualifying, win rate for royale and duel.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	seds "github.com/Rigur-Calypso/seds-hackathon-Rigur"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/decide"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/search"
)

// Config is the run configuration.
type Config struct {
	Rules       string  `json:"rules"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Snakes      int     `json:"snakes"`
	Games       int     `json:"games"`
	Seeds       []int64 `json:"seeds"`
	ProfileA    string  `json:"profileA,omitempty"`
	ProfileB    string  `json:"profileB,omitempty"`
	Paired      bool    `json:"paired"`
	Opponents   string  `json:"opponents"`
	BudgetMs    int     `json:"budgetMs"`
	TimeoutMs   int     `json:"timeoutMs"`
	Concurrency int     `json:"concurrency"`
	MaxTurns    int     `json:"maxTurns"`
	DuelDepth   int     `json:"duelDepth"`
	DumpLosses  bool    `json:"-"`
	Verbose     bool    `json:"-"`
}

// Summary is one configuration's per-stage report.
type Summary struct {
	Games        int                `json:"games"`
	MeanPoints   float64            `json:"meanPoints"`
	WinRate      float64            `json:"winRate"`
	MeanPlace    float64            `json:"meanPlace"`
	MeanSurvival float64            `json:"meanSurvivalTurns"`
	Timeouts     int                `json:"timeouts"`
	P50Ms        float64            `json:"p50Ms"`
	P95Ms        float64            `json:"p95Ms"`
	P99Ms        float64            `json:"p99Ms"`
	MaxMs        float64            `json:"maxMs"`
	Kills        int                `json:"kills"`
	Deaths       map[string]int     `json:"deaths"`
	VsOpponent   map[string]float64 `json:"meanPointsVsOpponent"`
	WallSeconds  float64            `json:"wallSeconds"`
}

// Paired is the common-random-numbers comparison of B against A.
type Paired struct {
	N              int     `json:"n"`
	MeanDiffPoints float64 `json:"meanDiffPoints"`
	PPoints        float64 `json:"pPoints"`
	MeanDiffWin    float64 `json:"meanDiffWin"`
	PWin           float64 `json:"pWin"`
	Significant    bool    `json:"significantAt05"`
}

// Report is the JSON output.
type Report struct {
	Config Config   `json:"config"`
	Stage  string   `json:"stageProfile"`
	A      Summary  `json:"a"`
	B      *Summary `json:"b,omitempty"`
	Paired *Paired  `json:"paired,omitempty"`
}

func stageProfile(cfg *Config) string {
	switch {
	case strings.Contains(cfg.Rules, "constrictor"):
		return "constrictor"
	case cfg.Rules == "royale" && (cfg.Width >= 19 || cfg.Height >= 19):
		return "duel"
	case cfg.Rules == "royale":
		return "royale"
	}
	return "qualifying"
}

func loadEngine(cfg *Config, path string) (*decide.Engine, error) {
	ps, err := config.Load(seds.ConfigFS)
	if err != nil {
		return nil, err
	}
	name := stageProfile(cfg)
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		p, err := config.Decode(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		p.Name = name
		ps.Set(name, p)
	}
	if cfg.DuelDepth > 0 {
		for _, n := range config.Names {
			p := *ps.Get(n)
			p.DuelMaxDepth = cfg.DuelDepth
			ps.Set(n, &p)
		}
	}
	return decide.New(ps, search.Evaluate), nil
}

type job struct {
	idx  int
	seed int64
	opps []string
}

func buildJobs(cfg *Config) []job {
	var names []string
	switch cfg.Opponents {
	case "zoo", "":
		names = zooOrder
	default:
		names = strings.Split(cfg.Opponents, ",")
	}
	jobs := make([]job, cfg.Games)
	for g := 0; g < cfg.Games; g++ {
		base := cfg.Seeds[g%len(cfg.Seeds)]
		j := job{idx: g, seed: base*1_000_003 + int64(g)}
		for k := 1; k < cfg.Snakes; k++ {
			j.opps = append(j.opps, names[(g+k-1)%len(names)])
		}
		jobs[g] = j
	}
	return jobs
}

func runAll(cfg *Config, eng *decide.Engine, champion *decide.Engine, jobs []job) ([]GameResult, time.Duration) {
	start := time.Now()
	out := make([]GameResult, len(jobs))
	ch := make(chan job)
	var wg sync.WaitGroup
	for w := 0; w < cfg.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				seats := []Seat{{"us", enginePolicy{eng, time.Duration(cfg.BudgetMs) * time.Millisecond}}}
				for _, o := range j.opps {
					var p Policy
					if o == "champion" || o == "self" {
						p = enginePolicy{champion, time.Duration(cfg.BudgetMs) * time.Millisecond}
					} else if zp, ok := zoo[o]; ok {
						p = zp
					} else {
						p = zoo["random"]
					}
					seats = append(seats, Seat{o, p})
				}
				r, err := playGame(cfg, j.seed, seats)
				if err != nil {
					fmt.Fprintf(os.Stderr, "game %d seed %d: %v\n", j.idx, j.seed, err)
				}
				out[j.idx] = r
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	return out, time.Since(start)
}

func summarize(rs []GameResult, wall time.Duration) Summary {
	s := Summary{Games: len(rs), Deaths: map[string]int{}, VsOpponent: map[string]float64{}, WallSeconds: wall.Seconds()}
	var pts, place, surv []float64
	var all []time.Duration
	vs, vsN := map[string]float64{}, map[string]int{}
	wins := 0
	for _, r := range rs {
		pts = append(pts, r.Points)
		place = append(place, r.Place)
		surv = append(surv, float64(r.Survived))
		all = append(all, r.Decisions...)
		s.Timeouts += r.Timeouts
		s.Kills += r.Kills
		if r.Won {
			wins++
		}
		cause := r.Cause
		if cause == "" {
			cause = "survived"
		}
		s.Deaths[cause]++
		for _, o := range r.Opponents {
			vs[o] += r.Points
			vsN[o]++
		}
	}
	s.MeanPoints, s.MeanPlace, s.MeanSurvival = mean(pts), mean(place), mean(surv)
	if len(rs) > 0 {
		s.WinRate = float64(wins) / float64(len(rs))
	}
	s.P50Ms, s.P95Ms, s.P99Ms, s.MaxMs = percentileMs(all, 0.50), percentileMs(all, 0.95), percentileMs(all, 0.99), percentileMs(all, 1)
	for o, v := range vs {
		s.VsOpponent[o] = v / float64(vsN[o])
	}
	return s
}

func paired(a, b []GameResult) *Paired {
	var dp, dw []float64
	for i := range a {
		dp = append(dp, b[i].Points-a[i].Points)
		wa, wb := 0.0, 0.0
		if a[i].Won {
			wa = 1
		}
		if b[i].Won {
			wb = 1
		}
		dw = append(dw, wb-wa)
	}
	_, pp := pairedT(dp)
	_, pw := pairedT(dw)
	return &Paired{N: len(a), MeanDiffPoints: mean(dp), PPoints: pp, MeanDiffWin: mean(dw), PWin: pw, Significant: pp < 0.05 || pw < 0.05}
}

func main() {
	cfg := Config{}
	var seeds string
	flag.StringVar(&cfg.Rules, "rules", "standard", "standard|royale|constrictor|wrapped")
	flag.IntVar(&cfg.Width, "width", 11, "board width")
	flag.IntVar(&cfg.Height, "height", 11, "board height")
	flag.IntVar(&cfg.Snakes, "snakes", 4, "snakes per game (seat 0 is us)")
	flag.IntVar(&cfg.Games, "games", 100, "games per configuration")
	flag.StringVar(&seeds, "seeds", "42,5,725,1337,99", "FIXED base seeds; never change mid-tuning")
	flag.StringVar(&cfg.ProfileA, "profile-a", "", "profile JSON for A (default: shipped config)")
	flag.StringVar(&cfg.ProfileB, "profile-b", "", "profile JSON for B (enables comparison)")
	flag.BoolVar(&cfg.Paired, "paired", true, "paired seeds (common random numbers)")
	flag.StringVar(&cfg.Opponents, "opponents", "zoo", "zoo | comma list of random,foodgreedy,spacegreedy,headhunter,hazardcoward,fallback,champion")
	flag.IntVar(&cfg.BudgetMs, "budget", 0, "wall-clock decision budget in ms (0 = deterministic)")
	flag.IntVar(&cfg.TimeoutMs, "timeout", 500, "timeout used to count timeouts (ms)")
	flag.IntVar(&cfg.Concurrency, "concurrency", runtime.NumCPU(), "parallel games")
	flag.IntVar(&cfg.MaxTurns, "max-turns", 600, "turn cap per game")
	flag.IntVar(&cfg.DuelDepth, "duel-depth", 0, "override duel search depth (0 = profile)")
	flag.BoolVar(&cfg.DumpLosses, "dump-losses", false, "print the fatal board of every loss (fixture DSL)")
	flag.Parse()
	for _, f := range strings.Split(seeds, ",") {
		v, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad seed:", f)
			os.Exit(2)
		}
		cfg.Seeds = append(cfg.Seeds, v)
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}

	champion, err := loadEngine(&cfg, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	engA, err := loadEngine(&cfg, cfg.ProfileA)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	jobs := buildJobs(&cfg)
	resA, wallA := runAll(&cfg, engA, champion, jobs)
	rep := Report{Config: cfg, Stage: stageProfile(&cfg), A: summarize(resA, wallA)}
	if cfg.ProfileB != "" {
		engB, err := loadEngine(&cfg, cfg.ProfileB)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		resB, wallB := runAll(&cfg, engB, champion, jobs)
		sb := summarize(resB, wallB)
		rep.B = &sb
		rep.Paired = paired(resA, resB)
	}
	if cfg.DumpLosses {
		sort.SliceStable(resA, func(a, b int) bool { return resA[a].Seed < resA[b].Seed })
		for _, r := range resA {
			if r.FatalBoard != "" {
				fmt.Fprintf(os.Stderr, "# loss seed=%d cause=%s turn=%d opponents=%v\n%s\n", r.Seed, r.Cause, r.Survived, r.Opponents, r.FatalBoard)
			}
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(rep)
}
