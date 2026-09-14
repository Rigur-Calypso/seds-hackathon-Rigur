package brain

import (
	"context"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/eval"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/sim"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/testgen"
)

func params() *config.Params {
	p := config.Defaults()
	p.Engine = "v2"
	p.SearchNodes = 20000
	return &p
}

func state(t testing.TB, text string) *board.State {
	t.Helper()
	s, ok := board.FromAPI(fixture.MustState(text))
	if !ok {
		t.Fatal("you not found")
	}
	return s
}

func search(t testing.TB, s *board.State, p *config.Params) Result {
	t.Helper()
	res, err := Search(context.Background(), s, p, legal.Safe(s, 0))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	return res
}

// The leaf evaluator must be internal/eval's heuristic exactly, so v2 at depth
// one differs from v1 only by search, never by a porting slip.
func TestHeuristicMatchesV1(t *testing.T) {
	type variant struct {
		name  string
		w, h  int
		n     int
		rules board.Rules
		hz    func(rng *rand.Rand, s *board.State)
	}
	ring := func(rng *rand.Rand, s *board.State) {
		s.Hazard = board.RingHazards(s.W, s.H, board.Rect{MinX: 1 + rng.Intn(2), MinY: 1, MaxX: s.W - 2, MaxY: s.H - 1 - rng.Intn(3)})
	}
	scatter := func(rng *rand.Rand, s *board.State) {
		s.Hazard = make([]uint8, s.W*s.H)
		for i := range s.Hazard {
			if rng.Intn(4) == 0 {
				s.Hazard[i] = 1
			}
		}
	}
	variants := []variant{
		{"standard", 11, 11, 4, board.Rules{Name: "standard", HazardDamage: 14}, nil},
		{"royale", 11, 11, 4, board.Rules{Name: "royale", Royale: true, HazardDamage: 14, ShrinkEveryN: 25}, ring},
		{"duel19", 19, 19, 2, board.Rules{Name: "royale", Royale: true, HazardDamage: 14, ShrinkEveryN: 25}, ring},
		{"scatter", 11, 11, 4, board.Rules{Name: "standard", HazardDamage: 14}, scatter},
		{"constrictor", 11, 11, 4, board.Rules{Name: "constrictor", Constrictor: true}, nil},
		{"wrapped", 11, 11, 4, board.Rules{Name: "wrapped", Wrapped: true, HazardDamage: 14}, nil},
	}
	checked := 0
	for _, v := range variants {
		for seed := int64(1); seed <= 120; seed++ {
			rng := rand.New(rand.NewSource(seed))
			s := testgen.Position(seed*31337, v.w, v.h, v.n, rng.Intn(60), v.rules)
			s.Turn += rng.Intn(120)
			if v.hz != nil {
				v.hz(rng, s)
			}
			if !s.Snakes[0].Alive() {
				continue
			}
			p := params()
			g := sim.NewGame(s, sim.ShrinkPessimistic)
			st, ok := sim.New(s, g)
			if !ok {
				t.Fatal("sim.New failed")
			}
			se := new(Searcher)
			se.reset(context.Background(), p, g, st)
			r := se.fill.Compute(st, 0, true)
			got := se.heuristic(&r)
			want := eval.Heuristic(s, s, p)
			if math.Abs(got-want) > 1e-9 {
				t.Fatalf("%s seed %d: brain %.12f eval %.12f", v.name, seed, got, want)
			}
			checked++
		}
	}
	if checked < 500 {
		t.Fatalf("only %d positions checked", checked)
	}
}

// A cornered shorter snake whose only exit is a cell we can take: a forced
// win in one turn (R2).
func TestFindsForcedWin(t *testing.T) {
	s := state(t, "you 90 2,0 3,0 4,0 5,0 6,0\nsnake prey 90 0,0 0,1 0,2\nfood 9,9")
	res := search(t, s, params())
	if res.Move != board.Left || !IsWin(res.Scores[0].Value) {
		t.Fatalf("want left as a proven win, got %v %+v", res.Move, res.Scores)
	}
}

// Walking beside an equal-length head into a dead-end corridor loses under
// best replies; the search must see it and refuse.
func TestRefusesEqualHeadToHead(t *testing.T) {
	s := state(t, "you 100 5,5 5,4 5,3\nsnake rival 100 5,7 5,8 5,9\nfood 0,10")
	if res := search(t, s, params()); res.Move == board.Up {
		t.Fatalf("up allows an equal head-to-head: %+v", res.Scores)
	}
}

// The real sandwich from the v1 loss diagnostics (qualifying seed 5000131),
// one turn earlier: stepping down to the wall between two longer heads is
// refuted by both opponents stepping down too.
func TestSeesSandwichComing(t *testing.T) {
	s := state(t, `size 11 11
you 58 5,1 5,2 5,3 5,4 5,5
snake s1 91 6,2 6,3 6,4 6,5 7,5 8,5 8,4 8,3 8,2
snake s2 69 4,2 4,3 4,4 4,5 4,6 4,7
food 9,7 1,0 3,8`)
	res := search(t, s, params())
	if res.Move == board.Down && !IsLoss(res.Scores[0].Value) {
		t.Fatalf("down walks into the sandwich: %+v", res.Scores)
	}
}

func TestNodeBudgetIsDeterministic(t *testing.T) {
	s := state(t, `you 90 5,5 5,4 5,3 4,3 3,3 3,4
snake a 80 1,1 1,2 1,3 1,4 2,4
snake b 70 9,9 9,8 9,7 8,7 7,7
snake c 60 8,2 8,3 8,4 7,4
food 3,8 6,6 10,0`)
	p := params()
	p.SearchNodes = 5000
	a, b := search(t, s, p), search(t, s, p)
	if a.Move != b.Move || a.Depth != b.Depth || a.Nodes != b.Nodes {
		t.Fatalf("not reproducible: %+v vs %+v", a, b)
	}
	if a.Depth < 2 {
		t.Fatalf("5000 nodes must reach depth 2, got %d", a.Depth)
	}
}

func TestDeadlineRespected(t *testing.T) {
	s := state(t, `rules royale
size 19 19
turn 80
safe 2 2 16 16
you 90 9,9 9,8 9,7 9,6 9,5 8,5 7,5 6,5
snake a 90 5,12 5,13 5,14 5,15 6,15 7,15
food 3,3 15,15 9,14`)
	p := params()
	p.SearchNodes = 0
	for _, budget := range []time.Duration{3 * time.Millisecond, 30 * time.Millisecond, 120 * time.Millisecond} {
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		start := time.Now()
		res, err := Search(ctx, s, p, legal.Safe(s, 0))
		el := time.Since(start)
		cancel()
		if err != nil || el > budget+15*time.Millisecond {
			t.Fatalf("budget %v: elapsed %v err %v depth %d", budget, el, err, res.Depth)
		}
		t.Logf("budget %v: depth %d nodes %d", budget, res.Depth, res.Nodes)
	}
}

func TestTooManySnakesUnsupported(t *testing.T) {
	text := "size 19 19\nyou 100 0,0 0,1 0,2\n"
	for i := 0; i < sim.MaxSnakes; i++ {
		text += "snake s 100 " + itoa(2*i+2) + ",10 " + itoa(2*i+2) + ",11 " + itoa(2*i+2) + ",12\n"
	}
	s := state(t, text)
	if _, err := Search(context.Background(), s, params(), legal.Safe(s, 0)); err != ErrUnsupported {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

func BenchmarkSearch4Snakes(b *testing.B) {
	s := state(b, `you 90 5,5 5,4 5,3 4,3 3,3 3,4
snake a 80 1,1 1,2 1,3 1,4 2,4
snake b 70 7,7 7,8 7,9 8,9 9,9
snake c 60 7,3 8,3 8,4 8,5
food 3,8 6,6 10,0`)
	p := params()
	p.SearchNodes = 10000
	for i := 0; i < b.N; i++ {
		search(b, s, p)
	}
}

func BenchmarkSearchDuel19(b *testing.B) {
	s := state(b, `rules royale
size 19 19
turn 80
safe 2 2 16 16
you 90 9,9 9,8 9,7 9,6 9,5 8,5 7,5 6,5
snake a 90 9,12 9,13 9,14 9,15 8,15 7,15
food 3,3 15,15 9,14`)
	p := params()
	p.SearchNodes = 10000
	for i := 0; i < b.N; i++ {
		search(b, s, p)
	}
}
