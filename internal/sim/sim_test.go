package sim

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/rules"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/testgen"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/voronoi"
)

type variant struct {
	name    string
	w, h, n int
	rules   board.Rules
	hazards func(rng *rand.Rand, s *board.State)
	shrink  rules.ShrinkMode
	mode    ShrinkMode
	turn    int
}

func ring(rng *rand.Rand, s *board.State) {
	s.Hazard = board.RingHazards(s.W, s.H, board.Rect{MinX: 1 + rng.Intn(2), MinY: 1, MaxX: s.W - 2, MaxY: s.H - 1 - rng.Intn(3)})
}

func scatter(rng *rand.Rand, s *board.State) {
	s.Hazard = make([]uint8, s.W*s.H)
	for i := range s.Hazard {
		if rng.Intn(5) == 0 {
			s.Hazard[i] = uint8(1 + rng.Intn(2)) // stacked hazards apply per entry
		}
	}
}

var variants = []variant{
	{name: "standard", w: 11, h: 11, n: 4, rules: board.Rules{Name: "standard", HazardDamage: 14}},
	{name: "royale-keep", w: 11, h: 11, n: 4, rules: board.Rules{Name: "royale", Royale: true, HazardDamage: 14, ShrinkEveryN: 7}, hazards: ring, turn: 100},
	{name: "royale-pessimistic", w: 11, h: 11, n: 4, rules: board.Rules{Name: "royale", Royale: true, HazardDamage: 14, ShrinkEveryN: 5}, shrink: rules.ShrinkPessimistic, mode: ShrinkPessimistic, turn: 3},
	{name: "duel19", w: 19, h: 19, n: 2, rules: board.Rules{Name: "royale", Royale: true, HazardDamage: 14, ShrinkEveryN: 9}, hazards: ring, turn: 90},
	{name: "scatter", w: 11, h: 11, n: 4, rules: board.Rules{Name: "standard", HazardDamage: 9}, hazards: scatter},
	{name: "constrictor", w: 11, h: 11, n: 4, rules: board.Rules{Name: "constrictor", Constrictor: true}},
	{name: "wrapped", w: 11, h: 11, n: 4, rules: board.Rules{Name: "wrapped", Wrapped: true, HazardDamage: 14}, hazards: scatter},
	{name: "wrapped-constrictor", w: 7, h: 7, n: 4, rules: board.Rules{Name: "wrapped_constrictor", Wrapped: true, Constrictor: true}},
}

func position(v variant, seed int64) *board.State {
	rng := rand.New(rand.NewSource(seed))
	s := testgen.Position(seed, v.w, v.h, v.n, rng.Intn(40), v.rules)
	s.Turn += v.turn
	if v.hazards != nil && !(v.rules.Royale && s.Turn < v.rules.ShrinkEveryN) {
		v.hazards(rng, s)
	}
	return s
}

type snap struct {
	Turn   int32
	Snakes []string
	Food   []int16
	Occ    []uint8
	Hash   uint64
}

func snapshot(st *State) snap {
	sp := snap{Turn: st.Turn, Occ: append([]uint8(nil), st.occ...), Hash: st.Hash}
	for i := 0; i < st.N; i++ {
		sn := &st.S[i]
		body := make([]int16, sn.Len)
		for k := int32(0); k < sn.Len; k++ {
			body[k] = sn.Seg(k)
		}
		sp.Snakes = append(sp.Snakes, fmt.Sprint(sn.Alive, sn.Health, body))
	}
	st.FoodCells(func(c int16) { sp.Food = append(sp.Food, c) })
	return sp
}

func sortedPts(ps []board.Point) []board.Point {
	c := append([]board.Point(nil), ps...)
	sort.Slice(c, func(a, b int) bool { return c[a].Y*1000+c[a].X < c[b].Y*1000+c[b].X })
	return c
}

func occFromScratch(st *State) []uint8 {
	occ := make([]uint8, st.G.Cells)
	for i := 0; i < st.N; i++ {
		if sn := &st.S[i]; sn.Alive {
			for k := int32(0); k < sn.Len; k++ {
				occ[sn.Seg(k)]++
			}
		}
	}
	return occ
}

// Differential: Make must agree exactly with the clean-room resolver (itself
// differential-tested against the official engine) on every turn of many
// random games, and Unmake must restore the position bit for bit.
func TestMakeMatchesResolver(t *testing.T) {
	turns, deaths := 0, 0
	for _, v := range variants {
		for seed := int64(1); seed <= 160; seed++ {
			cur := position(v, seed*7919)
			if cur.AliveCount() < 2 {
				continue
			}
			g := NewGame(cur, v.mode)
			st, ok := New(cur, g)
			if !ok {
				t.Fatalf("%s seed %d: New failed", v.name, seed)
			}
			rng := rand.New(rand.NewSource(seed))
			var u Undo
			for turn := 0; turn < 80 && cur.AliveCount() >= 2; turn++ {
				dirs := make([]board.Dir, len(cur.Snakes))
				var mv Moves
				for i := range cur.Snakes {
					if !cur.Snakes[i].Alive() {
						continue
					}
					d := board.Dir(rng.Intn(4))
					if safe := legal.Safe(cur, i); len(safe) > 0 && rng.Intn(10) < 8 {
						d = safe[rng.Intn(len(safe))]
					}
					dirs[i], mv[i] = d, d
				}
				want := rules.Resolve(cur, dirs, rules.Options{Shrink: v.shrink})
				before := snapshot(st)
				st.Make(&mv, &u)
				got := st.ToBoard(cur.Rules)
				ctx := fmt.Sprintf("%s seed %d turn %d moves %v", v.name, seed, cur.Turn, dirs)
				for i := range want.Snakes {
					w, gs := &want.Snakes[i], &got.Snakes[i]
					if w.Alive() != gs.Alive() {
						t.Fatalf("%s snake %d alive: sim %v resolver %v (%s)", ctx, i, gs.Alive(), w.Alive(), w.Cause)
					}
					if w.Alive() && (w.Health != gs.Health || !reflect.DeepEqual(w.Body, gs.Body)) {
						t.Fatalf("%s snake %d:\n sim      %d %v\n resolver %d %v", ctx, i, gs.Health, gs.Body, w.Health, w.Body)
					}
					if w.Alive() != cur.Snakes[i].Alive() {
						deaths++
					}
				}
				if !reflect.DeepEqual(sortedPts(got.Food), sortedPts(want.Food)) {
					t.Fatalf("%s food: sim %v resolver %v", ctx, got.Food, want.Food)
				}
				wh, gh := want.Hazard, got.Hazard
				if wh == nil {
					wh = make([]uint8, want.W*want.H)
				}
				if gh == nil {
					gh = make([]uint8, want.W*want.H)
				}
				if !reflect.DeepEqual(wh, gh) {
					t.Fatalf("%s hazards differ", ctx)
				}
				if st.Hash != st.ComputeHash() {
					t.Fatalf("%s incremental hash drifted", ctx)
				}
				if !reflect.DeepEqual(st.occ, occFromScratch(st)) {
					t.Fatalf("%s occupancy drifted", ctx)
				}
				after := snapshot(st)
				st.Unmake(&u)
				if back := snapshot(st); !reflect.DeepEqual(back, before) {
					t.Fatalf("%s Unmake did not restore:\n before %+v\n after  %+v", ctx, before, back)
				}
				st.Make(&mv, &u)
				if again := snapshot(st); !reflect.DeepEqual(again, after) {
					t.Fatalf("%s Make is not repeatable", ctx)
				}
				cur = want
				turns++
			}
		}
	}
	t.Logf("%d turns identical, %d eliminations", turns, deaths)
	if turns < 5000 || deaths < 300 {
		t.Fatalf("coverage too low: turns=%d deaths=%d", turns, deaths)
	}
}

// SafeMoves must equal v1's legal.Safe on every position.
func TestSafeMovesMatchLegal(t *testing.T) {
	for _, v := range variants {
		for seed := int64(1); seed <= 150; seed++ {
			bs := position(v, seed*104729)
			st, ok := New(bs, NewGame(bs, ShrinkKeep))
			if !ok {
				t.Fatal("New failed")
			}
			for i := range bs.Snakes {
				if !bs.Snakes[i].Alive() {
					continue
				}
				var out [4]board.Dir
				got := out[:st.SafeMoves(i, &out)]
				want := legal.Safe(bs, i)
				if len(want) == 0 {
					want = nil
				}
				if len(got) == 0 {
					got = nil
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s seed %d snake %d: sim %v legal %v", v.name, seed, i, got, want)
				}
			}
		}
	}
}

// The fast fill must reproduce v1's temporal Voronoi exactly.
func TestFillMatchesVoronoi(t *testing.T) {
	var f Fill
	for _, v := range variants {
		for seed := int64(1); seed <= 150; seed++ {
			bs := position(v, seed*15485863)
			st, ok := New(bs, NewGame(bs, ShrinkKeep))
			if !ok {
				t.Fatal("New failed")
			}
			want := voronoi.Compute(bs, 0)
			got := f.Compute(st, 0, true)
			w := Result{N: want.N, FreeCells: want.FreeCells, Guaranteed: want.Guaranteed, Contested: want.Contested,
				Attack: want.Attack, FoodDist: want.FoodDist, SafeExits: want.SafeExits, ExitsUncontested: want.ExitsUncontested,
				Trapped: want.Trapped, Robust: want.Robust, CutCells: want.CutCells}
			w.FoodHealth = got.FoodHealth   // v2-only field, tested in TestFoodHealth
			w.OwnReleased = got.OwnReleased // v2-only field, tested in TestOwnReleased
			if got != w {
				t.Fatalf("%s seed %d:\n sim     %+v\n voronoi %+v", v.name, seed, got, w)
			}
		}
	}
}

// FoodHealth charges storm damage on the way (R6) and equals health minus
// distance when there is no storm.
func TestFoodHealth(t *testing.T) {
	var f Fill
	for _, v := range variants {
		for seed := int64(1); seed <= 60; seed++ {
			bs := position(v, seed*977)
			st, _ := New(bs, NewGame(bs, ShrinkKeep))
			r := f.Compute(st, 0, false)
			if bs.Hazard != nil || bs.Rules.Constrictor {
				continue
			}
			for j := 0; j < st.N; j++ {
				if !st.S[j].Alive {
					continue
				}
				want := -1
				if r.FoodDist[j] >= 0 {
					h := int(st.S[j].Health)
					if h > MaxHealth {
						h = MaxHealth
					}
					want = h - r.FoodDist[j]
				}
				if r.FoodHealth[j] != want {
					t.Fatalf("%s seed %d snake %d: FoodHealth %d want %d (dist %d)", v.name, seed, j, r.FoodHealth[j], want, r.FoodDist[j])
				}
			}
		}
	}
	bs, _ := board.FromAPI(fixture.MustState("you 50 5,5 5,4 5,3\nsnake far 100 0,10 1,10 2,10\nfood 5,8\nhazard 5,6 5,7\ndamage 14"))
	st, _ := New(bs, NewGame(bs, ShrinkKeep))
	r := f.Compute(st, 0, false)
	// The only 3-step path crosses both hazards: 50 - 3 - 28 = 19. (A longer,
	// storm-free detour is not seen: the fill is first-arrival; it keeps the
	// best health only among equally short paths.)
	if r.FoodDist[0] != 3 || r.FoodHealth[0] != 19 {
		t.Fatalf("dist %d health %d, want 3 and 19", r.FoodDist[0], r.FoodHealth[0])
	}
}

// A coiled snake in a pocket depends on its own body for room; a straight snake
// in the open does not.
func TestOwnReleased(t *testing.T) {
	var f Fill
	open, _ := board.FromAPI(fixture.MustState("you 100 5,5 5,4 5,3\nsnake far 100 0,10 1,10 2,10"))
	st, _ := New(open, NewGame(open, ShrinkKeep))
	if r := f.Compute(st, 0, false); r.OwnReleased[0] > 2 || r.Reach(0) < 50 {
		t.Fatalf("open board: own released %d of reach %d", r.OwnReleased[0], r.Reach(0))
	}
	// A 3×3 board our coiled body fills but one cell: the head is next to the
	// tail and every other cell it can reach is its own body freeing in turn.
	coiled, _ := board.FromAPI(fixture.MustState("size 3 3\nyou 100 1,1 2,1 2,2 1,2 0,2 0,1 0,0 1,0"))
	st, _ = New(coiled, NewGame(coiled, ShrinkKeep))
	r := f.Compute(st, 0, false)
	if r.Reach(0) < 6 || r.OwnReleased[0] < r.Reach(0)-1 {
		t.Fatalf("coiled: own released %d of reach %d; want all but the one free cell", r.OwnReleased[0], r.Reach(0))
	}
}

func TestNewRejectsWhatDoesNotFit(t *testing.T) {
	bs, _ := board.FromAPI(fixture.MustState("you 100 5,5 5,4 5,3\nsnake a 100 1,1 1,2 1,3"))
	if _, ok := New(bs, NewGame(bs, ShrinkKeep)); !ok {
		t.Fatal("normal position must fit")
	}
	many := *bs
	many.Snakes = nil
	for i := 0; i <= MaxSnakes; i++ {
		many.Snakes = append(many.Snakes, board.Snake{Body: []board.Point{{X: i, Y: 0}}, Health: 100})
	}
	if _, ok := New(&many, NewGame(&many, ShrinkKeep)); ok {
		t.Fatal("more than MaxSnakes must not fit")
	}
}

func benchState(b *testing.B, text string) *State {
	bs, _ := board.FromAPI(fixture.MustState(text))
	st, ok := New(bs, NewGame(bs, ShrinkPessimistic))
	if !ok {
		b.Fatal("New failed")
	}
	return st
}

const bench4 = `you 90 5,5 5,4 5,3 4,3 3,3 3,4
snake a 80 1,1 1,2 1,3 1,4 2,4
snake b 70 9,9 9,8 9,7 8,7 7,7
snake c 60 8,2 8,3 8,4 7,4
food 3,8 6,6 10,0`

func BenchmarkMakeUnmake4(b *testing.B) {
	st := benchState(b, bench4)
	mv := Moves{board.Up, board.Right, board.Left, board.Down}
	var u Undo
	for i := 0; i < b.N; i++ {
		st.Make(&mv, &u)
		st.Unmake(&u)
	}
}

func BenchmarkFill4(b *testing.B) {
	st := benchState(b, bench4)
	var f Fill
	for i := 0; i < b.N; i++ {
		f.Compute(st, 0, true)
	}
}

func BenchmarkFill19Duel(b *testing.B) {
	st := benchState(b, `rules royale
size 19 19
turn 80
safe 2 2 16 16
you 90 9,9 9,8 9,7 9,6 9,5 8,5 7,5 6,5
snake a 90 5,12 5,13 5,14 5,15 6,15 7,15
food 3,3 15,15 9,14`)
	var f Fill
	for i := 0; i < b.N; i++ {
		f.Compute(st, 0, true)
	}
}
