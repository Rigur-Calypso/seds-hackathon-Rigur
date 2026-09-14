// Command arena plays Battlesnake games in-process with the OFFICIAL rules
// package (AGPL-3.0 — this module is local tooling, never served or
// distributed; CLAUDE.md §8) against our decide.Engine. No HTTP.
//
//	go run . --rules standard --snakes 4 --games 500
//	go run . --rules royale --width 19 --height 19 --snakes 2 --games 200
//	go run . --profile-b cand.json --games 500                       # paired A/B with bootstrap CIs
//	go run . --profile-b cand.json --grid "shrink=15,25;food-spawn=10,25"   # robust across settings
//	go run . --diagnose --examples 2                                  # why the snake loses
//
// Fitness is reported per stage and never blended: mean placement points
// (10/6/3/1) for qualifying, win rate for royale and duel. Every game setting
// the live engine sends is a flag, so tuning is never done against one
// assumed environment.
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
	Rules           string  `json:"rules"`
	Map             string  `json:"map"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	Snakes          int     `json:"snakes"`
	Games           int     `json:"games"`
	Seeds           []int64 `json:"seeds"`
	FoodSpawnChance int     `json:"foodSpawnChance"`
	MinimumFood     int     `json:"minimumFood"`
	HazardDamage    int     `json:"hazardDamage"`
	ShrinkEveryN    int     `json:"shrinkEveryN"`
	MaxTurns        int     `json:"maxTurns"`
	TieBreak        string  `json:"tieBreak"`
	ProfileA        string  `json:"profileA,omitempty"`
	ProfileB        string  `json:"profileB,omitempty"`
	Paired          bool    `json:"paired"`
	Opponents       string  `json:"opponents"`
	BudgetMs        int     `json:"budgetMs"`
	TimeoutMs       int     `json:"timeoutMs"`
	Concurrency     int     `json:"concurrency"`
	DuelDepth       int     `json:"duelDepth"`
	Nodes           int     `json:"nodes"`
	Bootstrap       int     `json:"bootstrap"`
	Diagnose        bool    `json:"diagnose"`
	Grid            string  `json:"grid,omitempty"`
	Examples        int     `json:"-"`
	DumpLosses      bool    `json:"-"`
	TraceSeed       int64   `json:"-"`
}

func defaultConfig() Config {
	return Config{
		Rules: "standard", Map: "standard", Width: 11, Height: 11, Snakes: 4, Games: 100,
		Seeds:           []int64{42, 5, 725, 1337, 99},
		FoodSpawnChance: 15, MinimumFood: 1, HazardDamage: 14, ShrinkEveryN: 25,
		MaxTurns: 600, TieBreak: "length", Paired: true, Opponents: "zoo",
		TimeoutMs: 500, Concurrency: runtime.NumCPU(), Bootstrap: 2000,
	}
}

func (cfg *Config) mapID() string {
	if cfg.Map == "" {
		return "standard"
	}
	return cfg.Map
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
	Diagnosis    *Diagnosis         `json:"diagnosis,omitempty"`
}

// Paired is the common-random-numbers comparison of B against A.
type Paired struct {
	N              int        `json:"n"`
	MeanDiffPoints float64    `json:"meanDiffPoints"`
	PPoints        float64    `json:"pPoints"`
	CIPoints       [2]float64 `json:"ci95Points"`
	MeanDiffWin    float64    `json:"meanDiffWin"`
	PWin           float64    `json:"pWin"`
	CIWin          [2]float64 `json:"ci95Win"`
	// Significant is descriptive only: two-sided, on points OR wins, so it is
	// also true for a significant regression. Never promote on it; use Gate.
	Significant bool `json:"significantAt05"`
}

// Gate is the promotion decision for B over A. Pass requires every criterion;
// Reasons lists each one that failed.
type Gate struct {
	Pass    bool     `json:"pass"`
	Reasons []string `json:"failReasons,omitempty"`
}

// gateAlpha is the significance level of the promotion gate.
const gateAlpha = 0.05

// promotionGate passes B only for a real improvement in points: a positive
// mean paired difference, p < gateAlpha, a bootstrap 95 % CI whose lower bound
// is above zero, and zero timeouts for B. Win-rate significance never promotes.
func promotionGate(p *Paired, b *Summary) Gate {
	var g Gate
	if p == nil || b == nil {
		g.Reasons = append(g.Reasons, "no paired comparison")
		return g
	}
	if p.MeanDiffPoints <= 0 {
		g.Reasons = append(g.Reasons, fmt.Sprintf("mean points difference %.3f not positive", p.MeanDiffPoints))
	}
	if p.PPoints >= gateAlpha {
		g.Reasons = append(g.Reasons, fmt.Sprintf("p=%.3f not below %.2f", p.PPoints, gateAlpha))
	}
	if p.CIPoints[0] <= 0 {
		g.Reasons = append(g.Reasons, fmt.Sprintf("95%% CI lower bound %.3f not above zero", p.CIPoints[0]))
	}
	if b.Timeouts > 0 {
		g.Reasons = append(g.Reasons, fmt.Sprintf("B timed out %d times", b.Timeouts))
	}
	g.Pass = len(g.Reasons) == 0
	return g
}

// gridGate passes only if every environment passes promotionGate on its own:
// a candidate that regresses in any plausible event setting is not promoted.
func gridGate(cells []GridCell) Gate {
	var g Gate
	if len(cells) == 0 {
		g.Reasons = append(g.Reasons, "no grid cells")
	}
	for _, c := range cells {
		if c.Report.Gate == nil || !c.Report.Gate.Pass {
			why := "no paired comparison"
			if c.Report.Gate != nil {
				why = strings.Join(c.Report.Gate.Reasons, "; ")
			}
			g.Reasons = append(g.Reasons, fmt.Sprintf("cell %s: %s", settingsKey(c.Settings), why))
		}
	}
	g.Pass = len(g.Reasons) == 0
	return g
}

// settingsKey renders grid settings in a stable order.
func settingsKey(set map[string]string) string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + set[k]
	}
	return strings.Join(parts, ",")
}

// Report is the JSON output of a single-environment run.
type Report struct {
	Config Config   `json:"config"`
	Stage  string   `json:"stageProfile"`
	A      Summary  `json:"a"`
	B      *Summary `json:"b,omitempty"`
	Paired *Paired  `json:"paired,omitempty"`
	Gate   *Gate    `json:"promotionGate,omitempty"`
}

// GridCell is one environment of a grid run.
type GridCell struct {
	Settings map[string]string `json:"settings"`
	Report   Report            `json:"report"`
}

// GridReport summarises a comparison across environments. A candidate that is
// better on average but worse in some plausible event setting shows up here.
type GridReport struct {
	Cells           []GridCell        `json:"cells"`
	WorstDiffPoints float64           `json:"worstMeanDiffPoints"`
	WorstDiffWin    float64           `json:"worstMeanDiffWin"`
	WorstCell       map[string]string `json:"worstCell,omitempty"`
	// AnySignificant is descriptive only (true for a regression too); Gate decides.
	AnySignificant bool `json:"anyCellSignificantAt05"`
	Gate           Gate `json:"promotionGate"`
}

func stageProfile(cfg *Config) string {
	royale := cfg.Rules == "royale" || cfg.Map == "royale"
	switch {
	case strings.Contains(cfg.Rules, "constrictor"):
		return "constrictor"
	case royale && (cfg.Width >= 19 || cfg.Height >= 19):
		return "duel"
	case royale:
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
			if p.DuelMinDepth > cfg.DuelDepth {
				p.DuelMinDepth = cfg.DuelDepth // otherwise --duel-depth below the gate silently disables search
			}
			ps.Set(n, &p)
		}
	}
	if cfg.Nodes > 0 {
		for _, n := range config.Names {
			// A profile that sets its own searchNodes keeps it, so a candidate can
			// be measured at a different budget from its opponents.
			if p := *ps.Get(n); p.SearchNodes == 0 {
				p.SearchNodes = cfg.Nodes
				ps.Set(n, &p)
			}
		}
	}
	return decide.New(ps, search.Evaluate), nil
}

// withEngine is a copy of eng whose every profile uses the named engine
// ("v1" or "v2"), so both generations can sit at one table.
func withEngine(eng *decide.Engine, name string) *decide.Engine {
	ps := eng.Profiles.Clone()
	for _, n := range config.Names {
		p := *ps.Get(n)
		p.Engine = name
		ps.Set(n, &p)
	}
	return decide.New(ps, search.Evaluate)
}

type job struct {
	idx  int
	seed int64
	opps []string
}

func opponentNames(spec string) []string {
	switch spec {
	case "zoo", "":
		return zooOrder
	case "adversarial":
		return adversarialOrder
	case "full":
		return append(append([]string{}, zooOrder...), adversarialOrder...)
	}
	return strings.Split(spec, ",")
}

func buildJobs(cfg *Config) []job {
	names := opponentNames(cfg.Opponents)
	jobs := make([]job, cfg.Games)
	for g := 0; g < cfg.Games; g++ {
		base := cfg.Seeds[g%len(cfg.Seeds)]
		j := job{idx: g, seed: base*1_000_003 + int64(g)}
		for k := 1; k < cfg.Snakes; k++ {
			j.opps = append(j.opps, names[(g+k-1)%len(names)])
		}
		jobs[g] = j
	}
	if cfg.TraceSeed != 0 {
		for _, j := range jobs {
			if j.seed == cfg.TraceSeed {
				j.idx = 0
				return []job{j}
			}
		}
		return nil
	}
	return jobs
}

func runAll(cfg *Config, eng *decide.Engine, champion *decide.Engine, jobs []job) ([]GameResult, time.Duration) {
	start := time.Now()
	out := make([]GameResult, len(jobs))
	ch := make(chan job)
	generations := map[string]*decide.Engine{"v1": withEngine(champion, "v1"), "v2": withEngine(champion, "v2")}
	var wg sync.WaitGroup
	for w := 0; w < cfg.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				budget := time.Duration(cfg.BudgetMs) * time.Millisecond
				seats := []Seat{{"us", enginePolicy{eng, budget}}}
				for _, o := range j.opps {
					var p Policy
					if o == "champion" || o == "self" {
						p = enginePolicy{champion, budget}
					} else if ge, ok := generations[o]; ok {
						p = enginePolicy{ge, budget}
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

func summarize(cfg *Config, rs []GameResult, wall time.Duration) Summary {
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
	if cfg.Diagnose {
		s.Diagnosis = diagnose(rs)
	}
	return s
}

func paired(a, b []GameResult, resamples int) *Paired {
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
	return &Paired{
		N: len(a), MeanDiffPoints: mean(dp), PPoints: pp, CIPoints: bootstrapCI(dp, resamples, 7),
		MeanDiffWin: mean(dw), PWin: pw, CIWin: bootstrapCI(dw, resamples, 11),
		Significant: pp < 0.05 || pw < 0.05,
	}
}

// runOne runs A (and B when configured) in one environment.
func runOne(cfg Config) (Report, []GameResult, error) {
	champion, err := loadEngine(&cfg, "")
	if err != nil {
		return Report{}, nil, err
	}
	engA, err := loadEngine(&cfg, cfg.ProfileA)
	if err != nil {
		return Report{}, nil, err
	}
	jobs := buildJobs(&cfg)
	resA, wallA := runAll(&cfg, engA, champion, jobs)
	rep := Report{Config: cfg, Stage: stageProfile(&cfg), A: summarize(&cfg, resA, wallA)}
	if cfg.ProfileB != "" {
		engB, err := loadEngine(&cfg, cfg.ProfileB)
		if err != nil {
			return Report{}, nil, err
		}
		resB, wallB := runAll(&cfg, engB, champion, jobs)
		sb := summarize(&cfg, resB, wallB)
		rep.B = &sb
		rep.Paired = paired(resA, resB, cfg.Bootstrap)
		g := promotionGate(rep.Paired, rep.B)
		rep.Gate = &g
	}
	return rep, resA, nil
}

// applySetting sets one grid key on cfg.
func applySetting(cfg *Config, key, val string) error {
	num := func() (int, error) { return strconv.Atoi(val) }
	var err error
	switch key {
	case "shrink":
		cfg.ShrinkEveryN, err = num()
	case "food-spawn":
		cfg.FoodSpawnChance, err = num()
	case "min-food":
		cfg.MinimumFood, err = num()
	case "hazard-damage":
		cfg.HazardDamage, err = num()
	case "max-turns":
		cfg.MaxTurns, err = num()
	case "snakes":
		cfg.Snakes, err = num()
	case "map":
		cfg.Map = val
	case "rules":
		cfg.Rules = val
	case "tie-break":
		cfg.TieBreak = val
	default:
		return fmt.Errorf("unknown grid key %q", key)
	}
	return err
}

// parseGrid turns "shrink=15,25;food-spawn=10,25" into the Cartesian product.
func parseGrid(spec string) ([]map[string]string, error) {
	cells := []map[string]string{{}}
	for _, part := range strings.Split(spec, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("bad grid part %q", part)
		}
		var next []map[string]string
		for _, v := range strings.Split(kv[1], ",") {
			for _, c := range cells {
				m := map[string]string{}
				for k, x := range c {
					m[k] = x
				}
				m[strings.TrimSpace(kv[0])] = strings.TrimSpace(v)
				next = append(next, m)
			}
		}
		cells = next
	}
	return cells, nil
}

func runGrid(base Config) (GridReport, error) {
	settings, err := parseGrid(base.Grid)
	if err != nil {
		return GridReport{}, err
	}
	var gr GridReport
	first := true
	for _, set := range settings {
		cfg := base
		for k, v := range set {
			if err := applySetting(&cfg, k, v); err != nil {
				return GridReport{}, err
			}
		}
		rep, _, err := runOne(cfg)
		if err != nil {
			return GridReport{}, err
		}
		gr.Cells = append(gr.Cells, GridCell{Settings: set, Report: rep})
		if rep.Paired != nil {
			if first || rep.Paired.MeanDiffPoints < gr.WorstDiffPoints {
				gr.WorstDiffPoints, gr.WorstCell = rep.Paired.MeanDiffPoints, set
			}
			if first || rep.Paired.MeanDiffWin < gr.WorstDiffWin {
				gr.WorstDiffWin = rep.Paired.MeanDiffWin
			}
			gr.AnySignificant = gr.AnySignificant || rep.Paired.Significant
			first = false
		}
	}
	gr.Gate = gridGate(gr.Cells)
	return gr, nil
}

func main() {
	cfg := defaultConfig()
	var seeds string
	flag.StringVar(&cfg.Rules, "rules", cfg.Rules, "standard|royale|constrictor|wrapped")
	flag.StringVar(&cfg.Map, "map", cfg.Map, "engine map id (standard, royale, hz_scatter, ...)")
	flag.IntVar(&cfg.Width, "width", cfg.Width, "board width")
	flag.IntVar(&cfg.Height, "height", cfg.Height, "board height")
	flag.IntVar(&cfg.Snakes, "snakes", cfg.Snakes, "snakes per game (seat 0 is us)")
	flag.IntVar(&cfg.Games, "games", cfg.Games, "games per configuration")
	flag.StringVar(&seeds, "seeds", "42,5,725,1337,99", "FIXED base seeds; never change mid-tuning")
	flag.IntVar(&cfg.FoodSpawnChance, "food-spawn", cfg.FoodSpawnChance, "food spawn chance (%)")
	flag.IntVar(&cfg.MinimumFood, "min-food", cfg.MinimumFood, "minimum food on the board")
	flag.IntVar(&cfg.HazardDamage, "hazard-damage", cfg.HazardDamage, "hazard damage per turn")
	flag.IntVar(&cfg.ShrinkEveryN, "shrink", cfg.ShrinkEveryN, "royale: turns between shrinks")
	flag.IntVar(&cfg.MaxTurns, "max-turns", cfg.MaxTurns, "turn cap per game")
	flag.StringVar(&cfg.TieBreak, "tie-break", cfg.TieBreak, "at the turn cap: length | draw")
	flag.StringVar(&cfg.ProfileA, "profile-a", "", "profile JSON for A (default: shipped config)")
	flag.StringVar(&cfg.ProfileB, "profile-b", "", "profile JSON for B (enables comparison)")
	flag.BoolVar(&cfg.Paired, "paired", true, "paired seeds (common random numbers)")
	flag.StringVar(&cfg.Opponents, "opponents", cfg.Opponents, "zoo | adversarial | full | comma list (random,foodgreedy,spacegreedy,headhunter,hazardcoward,fallback,pincer,foodbait,edgeherder,stormtrapper,champion)")
	flag.IntVar(&cfg.BudgetMs, "budget", 0, "wall-clock decision budget in ms (0 = deterministic)")
	flag.IntVar(&cfg.TimeoutMs, "timeout", cfg.TimeoutMs, "timeout used to count timeouts (ms)")
	flag.IntVar(&cfg.Concurrency, "concurrency", cfg.Concurrency, "parallel games")
	flag.IntVar(&cfg.DuelDepth, "duel-depth", 0, "override duel search depth (0 = profile); also lowers duelMinDepth")
	flag.IntVar(&cfg.Nodes, "nodes", 0, "v2 search node budget per decision for every engine (0 = profile); deterministic without --budget")
	flag.IntVar(&cfg.Bootstrap, "bootstrap", cfg.Bootstrap, "bootstrap resamples for 95% confidence intervals")
	flag.BoolVar(&cfg.Diagnose, "diagnose", false, "classify every loss by avoidability and last real choice")
	flag.IntVar(&cfg.Examples, "examples", 0, "with --diagnose: print N fatal boards per (cause, horizon) to stderr")
	flag.StringVar(&cfg.Grid, "grid", "", `run across settings, e.g. "shrink=15,25;food-spawn=10,25;hazard-damage=14,28"`)
	flag.BoolVar(&cfg.DumpLosses, "dump-losses", false, "print the fatal board of every loss (fixture DSL)")
	flag.Int64Var(&cfg.TraceSeed, "trace", 0, "play only the game with this seed (needs --games large enough to include it) and print every decision of seat 0 to stderr")
	flag.Parse()
	cfg.Seeds = nil
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
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	if cfg.Grid != "" {
		gr, err := runGrid(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_ = enc.Encode(gr)
		return
	}

	rep, resA, err := runOne(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	writeResultsIfRequested(resA)
	if cfg.DumpLosses || (cfg.Diagnose && cfg.Examples > 0) {
		sort.SliceStable(resA, func(a, b int) bool { return resA[a].Seed < resA[b].Seed })
		shown := map[string]int{}
		for _, r := range resA {
			switch {
			case cfg.DumpLosses && r.FatalBoard != "":
				fmt.Fprintf(os.Stderr, "# loss seed=%d cause=%s turn=%d opponents=%v\n%s\n", r.Seed, r.Cause, r.Survived, r.Opponents, r.FatalBoard)
			case r.Diag != nil:
				key := r.Diag.Cause + "/" + horizonBucket(r.Diag.Horizon)
				if shown[key] < cfg.Examples {
					shown[key]++
					fmt.Fprintf(os.Stderr, "# loss seed=%d cause=%s horizon=%d viable_at_fatal=%d last_choice_best=%.3f opponents=%v\n%s\n",
						r.Seed, r.Diag.Cause, r.Diag.Horizon, r.Diag.ViableAtFatal, r.Diag.LastChoiceBest, r.Opponents, r.Diag.Board)
				}
			}
		}
	}
	_ = enc.Encode(rep)
}
