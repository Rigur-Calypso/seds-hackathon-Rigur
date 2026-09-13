package main

import (
	"math"
	"testing"

	"github.com/BattlesnakeOfficial/rules"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
)

func TestPlacementsTieBreak(t *testing.T) {
	bs := &rules.BoardState{Turn: 100, Snakes: []rules.Snake{
		{ID: "s0", Body: make([]rules.Point, 5)},
		{ID: "s1", Body: make([]rules.Point, 7)},
		{ID: "s2", Body: make([]rules.Point, 3), EliminatedCause: rules.EliminatedByHeadToHeadCollision, EliminatedOnTurn: 50},
		{ID: "s3", Body: make([]rules.Point, 3), EliminatedCause: rules.EliminatedByHeadToHeadCollision, EliminatedOnTurn: 50},
	}}
	byLen := placements(bs, "length")
	if byLen[1].Points != 10 || !byLen[1].Won || byLen[0].Points != 6 || byLen[2].Points != 2 || byLen[3].Points != 2 {
		t.Fatalf("length tie-break: %+v", byLen)
	}
	draw := placements(bs, "draw")
	if draw[0].Points != 8 || draw[1].Points != 8 || draw[0].Won || draw[1].Won || draw[2].Points != 2 {
		t.Fatalf("draw tie-break: %+v", draw)
	}
}

// Every game setting reaches both the official engine and the request our bot sees.
func TestConfigReachesEngineAndRequest(t *testing.T) {
	cfg := defaultConfig()
	cfg.Rules, cfg.Map, cfg.HazardDamage, cfg.ShrinkEveryN, cfg.FoodSpawnChance, cfg.MinimumFood = "royale", "standard", 7, 10, 30, 3
	p := engineParams(&cfg)
	if p[rules.ParamHazardDamagePerTurn] != "7" || p[rules.ParamShrinkEveryNTurns] != "10" || p[rules.ParamFoodSpawnChance] != "30" || p[rules.ParamMinimumFood] != "3" {
		t.Fatalf("engine params: %v", p)
	}
	bs := rules.NewBoardState(11, 11)
	bs.Snakes = []rules.Snake{{ID: "s0", Health: 90, Body: []rules.Point{{X: 1, Y: 1}, {X: 1, Y: 2}}}}
	gs := toAPI(&cfg, "g", bs, 0)
	set := gs.Game.Ruleset.Settings
	if set.HazardDamagePerTurn != 7 || set.Royale.ShrinkEveryNTurns != 10 || set.FoodSpawnChance != 30 || set.MinimumFood != 3 || gs.Game.Map != "standard" {
		t.Fatalf("request settings: %+v map=%s", set, gs.Game.Map)
	}
}

type hazardWatcher struct {
	firstTurn *int
	inner     Policy
}

func (h hazardWatcher) Move(gs *api.GameState) string {
	if len(gs.Board.Hazards) > 0 && *h.firstTurn < 0 {
		*h.firstTurn = gs.Turn
	}
	return h.inner.Move(gs)
}

func TestShrinkCadenceChangesTheGame(t *testing.T) {
	first := func(shrink int) int {
		cfg := defaultConfig()
		cfg.Rules, cfg.Snakes, cfg.MaxTurns, cfg.ShrinkEveryN = "royale", 2, 40, shrink
		turn := -1
		seats := []Seat{{"watch", hazardWatcher{&turn, zoo["hazardcoward"]}}, {"coward", zoo["hazardcoward"]}}
		if _, err := playGame(&cfg, 3, seats); err != nil {
			t.Fatal(err)
		}
		return turn
	}
	// R8: the first ring appears in the state whose turn equals the cadence.
	if f5, f25 := first(5), first(25); f5 != 5 || f25 != 25 {
		t.Fatalf("first hazard turn: shrink 5 → %d (want 5), shrink 25 → %d (want 25)", f5, f25)
	}
}

func TestZooPoliciesPlayWholeGames(t *testing.T) {
	cfg := defaultConfig()
	cfg.Rules, cfg.MaxTurns, cfg.ShrinkEveryN = "royale", 80, 10
	names := append(append([]string{}, zooOrder...), adversarialOrder...)
	for i, name := range names {
		seats := []Seat{{name, zoo[name]}, {"random", zoo["random"]}, {"foodgreedy", zoo["foodgreedy"]}, {"spacegreedy", zoo["spacegreedy"]}}
		r, err := playGame(&cfg, int64(100+i), seats)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r.Turns < 3 {
			t.Fatalf("%s: game over at turn %d", name, r.Turns)
		}
	}
}

func TestBootstrapCI(t *testing.T) {
	if ci := bootstrapCI([]float64{0, 0, 0, 0}, 500, 1); ci != ([2]float64{}) {
		t.Fatalf("zero diffs: %v", ci)
	}
	diffs := make([]float64, 200)
	for i := range diffs {
		diffs[i] = 1 + math.Sin(float64(i))
	}
	ci := bootstrapCI(diffs, 2000, 1)
	m := mean(diffs)
	if !(ci[0] < m && m < ci[1] && ci[0] > 0.7 && ci[1] < 1.3) {
		t.Fatalf("ci %v around mean %.3f", ci, m)
	}
	if again := bootstrapCI(diffs, 2000, 1); again != ci {
		t.Fatal("bootstrap must be reproducible")
	}
}

func TestParseGrid(t *testing.T) {
	cells, err := parseGrid("shrink=15,25;food-spawn=10,20,30")
	if err != nil || len(cells) != 6 {
		t.Fatalf("cells=%d err=%v", len(cells), err)
	}
	cfg := defaultConfig()
	for k, v := range cells[5] {
		if err := applySetting(&cfg, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if cfg.ShrinkEveryN != 25 || cfg.FoodSpawnChance != 30 {
		t.Fatalf("last cell applied as %+v", cfg)
	}
	if _, err := parseGrid("nonsense"); err == nil {
		t.Fatal("bad grid must fail")
	}
}

func TestDiagnoseClassification(t *testing.T) {
	trace := []decisionRec{{Turn: 10, Viable: 3}, {Turn: 11, Viable: 1}, {Turn: 12, Viable: 0}}
	d := classifyLoss(trace, rules.EliminatedByHeadToHeadCollision, nil)
	if d.Horizon != 2 || d.ViableAtFatal != 0 {
		t.Fatalf("diag %+v", d)
	}
	agg := diagnose([]GameResult{{Diag: d}, {}})
	if agg.Losses != 1 || agg.FatalMoveAvoidable != 0 || agg.Horizon["1-3"] != 1 {
		t.Fatalf("aggregate %+v", agg)
	}
}

// --duel-depth below the shipped min depth must still enable duel search.
func TestDuelDepthOverrideLowersGate(t *testing.T) {
	cfg := defaultConfig()
	cfg.DuelDepth = 2
	eng, err := loadEngine(&cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if p := eng.Profiles.Get("qualifying"); p.DuelMaxDepth != 2 || p.DuelMinDepth > 2 {
		t.Fatalf("depth override: max=%d min=%d", p.DuelMaxDepth, p.DuelMinDepth)
	}
}
