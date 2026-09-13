package main

import (
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/BattlesnakeOfficial/rules"
	"github.com/BattlesnakeOfficial/rules/maps"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
	ours "github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/rules"
)

const diffShrink = 5 // fast shrink so R8 is exercised constantly

func fromOfficial(bs *rules.BoardState, name string) *board.State {
	s := &board.State{W: bs.Width, H: bs.Height, Turn: bs.Turn, Rules: board.Rules{
		Name:         name,
		Royale:       name == "royale",
		Constrictor:  strings.Contains(name, "constrictor"),
		Wrapped:      strings.Contains(name, "wrapped"),
		HazardDamage: 14, ShrinkEveryN: diffShrink,
	}}
	for _, sn := range bs.Snakes {
		body := make([]board.Point, len(sn.Body))
		for i, p := range sn.Body {
			body[i] = board.Point{X: p.X, Y: p.Y}
		}
		s.Snakes = append(s.Snakes, board.Snake{ID: sn.ID, Body: body, Health: sn.Health,
			Cause: board.CauseFromString(sn.EliminatedCause), ElimTurn: sn.EliminatedOnTurn})
	}
	for _, f := range bs.Food {
		s.Food = append(s.Food, board.Point{X: f.X, Y: f.Y})
	}
	if len(bs.Hazards) > 0 {
		s.Hazard = make([]uint8, s.W*s.H)
		for _, h := range bs.Hazards {
			s.Hazard[h.Y*s.W+h.X]++
		}
	}
	return s
}

func sortedPoints(ps []board.Point) []board.Point {
	c := append([]board.Point(nil), ps...)
	sort.Slice(c, func(a, b int) bool { return c[a].Y*100+c[a].X < c[b].Y*100+c[b].X })
	return c
}

// Differential test: our clean-room resolver must agree exactly with the
// official engine pipeline on every turn of many random games across
// standard, royale (11 and 19), constrictor and wrapped rulesets.
func TestResolverMatchesOfficialRules(t *testing.T) {
	cases := []struct {
		name    string
		w, h, n int
	}{
		{"standard", 11, 11, 4}, {"royale", 11, 11, 4}, {"royale", 19, 19, 2},
		{"constrictor", 11, 11, 4}, {"wrapped", 11, 11, 4}, {"wrapped_constrictor", 7, 7, 4},
	}
	params := map[string]string{
		rules.ParamFoodSpawnChance: "30", rules.ParamMinimumFood: "3",
		rules.ParamHazardDamagePerTurn: "14", rules.ParamShrinkEveryNTurns: "5",
	}
	gm, _ := maps.GetMap("standard")
	checked, hazardChecked, deaths := 0, 0, 0
	for _, c := range cases {
		for g := 0; g < 40; g++ {
			seed := int64(7000 + g)
			rng := rand.New(rand.NewSource(seed))
			ids := make([]string, c.n)
			for i := range ids {
				ids[i] = string(rune('a' + i))
			}
			rs := rules.NewRulesetBuilder().WithSeed(seed).WithParams(params).NamedRuleset(c.name)
			bs, err := maps.SetupBoard(gm.ID(), rs.Settings(), c.w, c.h, ids)
			if err != nil {
				t.Fatal(err)
			}
			if _, bs, err = rs.Execute(bs, nil); err != nil {
				t.Fatal(err)
			}
			for turn := 0; turn < 250; turn++ {
				cur := fromOfficial(bs, c.name)
				if cur.AliveCount() <= 1 {
					break
				}
				dirs := make([]board.Dir, len(cur.Snakes))
				var moves []rules.SnakeMove
				for i := range cur.Snakes {
					if !cur.Snakes[i].Alive() {
						continue
					}
					safe := legal.Safe(cur, i)
					d := board.Dir(rng.Intn(4))
					if len(safe) > 0 && rng.Intn(10) < 8 {
						d = safe[rng.Intn(len(safe))]
					}
					dirs[i] = d
					moves = append(moves, rules.SnakeMove{ID: cur.Snakes[i].ID, Move: d.String()})
				}
				over, next, err := rs.Execute(bs, moves)
				if err != nil {
					t.Fatal(err)
				}
				if over {
					break
				}
				want := fromOfficial(next, c.name)
				opt := ours.Options{}
				compareHazards := !cur.Rules.Royale
				if cur.Rules.Royale {
					// Execute does not advance Turn (the game loop does), so the
					// engine's regeneration turn is cur.Turn+1.
					newTurn := cur.Turn + 1
					pr, nr := board.SafeRect(cur), board.SafeRect(want)
					if newTurn%diffShrink != 0 || newTurn < diffShrink {
						compareHazards = true
					} else {
						for side := 0; side < 4; side++ {
							if pr.Shrink(side) == nr {
								opt, compareHazards = ours.Options{Shrink: ours.ShrinkExact, Side: side}, true
								break
							}
						}
					}
				}
				got := ours.Resolve(cur, dirs, opt)
				for i := range want.Snakes {
					if !reflect.DeepEqual(got.Snakes[i], want.Snakes[i]) {
						t.Fatalf("%s %dx%d seed %d turn %d snake %d:\n ours     %+v\n official %+v\nmoves %v",
							c.name, c.w, c.h, seed, bs.Turn, i, got.Snakes[i], want.Snakes[i], dirs)
					}
					if want.Snakes[i].Alive() != cur.Snakes[i].Alive() {
						deaths++
					}
				}
				if !reflect.DeepEqual(sortedPoints(got.Food), sortedPoints(want.Food)) {
					t.Fatalf("%s seed %d turn %d food: ours %v official %v", c.name, seed, bs.Turn, got.Food, want.Food)
				}
				if compareHazards {
					gh, wh := got.Hazard, want.Hazard
					if gh == nil {
						gh = make([]uint8, got.W*got.H)
					}
					if wh == nil {
						wh = make([]uint8, want.W*want.H)
					}
					if !reflect.DeepEqual(gh, wh) {
						t.Fatalf("%s seed %d turn %d hazards differ (opt %+v)", c.name, seed, bs.Turn, opt)
					}
					hazardChecked++
				}
				checked++
				if bs, err = maps.PostUpdateBoard(gm, next, rs.Settings()); err != nil {
					t.Fatal(err)
				}
				bs.Turn++
			}
		}
	}
	t.Logf("differential: %d turns identical (%d with hazards compared, %d eliminations)", checked, hazardChecked, deaths)
	if checked < 2000 || deaths < 100 {
		t.Fatalf("differential coverage too low: turns=%d deaths=%d", checked, deaths)
	}
}

// Step 5 gate: the arena is deterministic, so the null test (champion vs an
// identical copy on paired seeds) must show no difference at all.
func TestNullTestDeterministic(t *testing.T) {
	cfg := Config{Rules: "standard", Width: 11, Height: 11, Snakes: 4, Games: 12, Seeds: []int64{42, 5, 725}, Opponents: "zoo",
		TimeoutMs: 500, Concurrency: 4, MaxTurns: 200, DuelDepth: 2}
	eng, err := loadEngine(&cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	jobs := buildJobs(&cfg)
	a, _ := runAll(&cfg, eng, eng, jobs)
	b, _ := runAll(&cfg, eng, eng, jobs)
	p := paired(a, b)
	if p.MeanDiffPoints != 0 || p.Significant {
		t.Fatalf("null test failed: %+v", p)
	}
	for i := range a {
		if a[i].Turns != b[i].Turns || a[i].Points != b[i].Points {
			t.Fatalf("game %d not reproducible: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestPairedTSanity(t *testing.T) {
	if _, p := pairedT([]float64{0, 0, 0, 0}); p != 1 {
		t.Fatalf("zero diffs p=%v", p)
	}
	diffs := make([]float64, 100)
	for i := range diffs {
		diffs[i] = 1 + float64(i%3) - 1
	}
	if _, p := pairedT(diffs); p > 1e-6 {
		t.Fatalf("clear effect must be significant, p=%v", p)
	}
	noise := []float64{1, -1, 1, -1, 1, -1, 1, -1}
	if _, p := pairedT(noise); p < 0.5 {
		t.Fatalf("pure noise must not be significant, p=%v", p)
	}
}
