package voronoi

import (
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/testgen"
)

func st(t *testing.T, text string) *board.State {
	t.Helper()
	s, ok := board.FromAPI(fixture.MustState(text))
	if !ok {
		t.Fatal("you not found")
	}
	return s
}

// Tail-chase: our own body frees over time, so the whole board is ours.
func TestSoloTailRelease(t *testing.T) {
	r := Compute(st(t, "you 100 5,5 5,4 5,3"), 0)
	if r.Guaranteed[0] != 120 || r.Contested[0] != 0 || r.FreeCells != 118 {
		t.Fatalf("G=%d C=%d free=%d", r.Guaranteed[0], r.Contested[0], r.FreeCells)
	}
}

// Contested cells are shared, never assigned by index; the longer snake gets Attack.
func TestContestedNotAssignedByIndex(t *testing.T) {
	r := Compute(st(t, "you 100 2,5 1,5 0,5\nsnake mirror 100 8,5 9,5 10,5"), 0)
	if r.Guaranteed[0] != r.Guaranteed[1] || r.Contested[0] != r.Contested[1] || r.Contested[0] == 0 {
		t.Fatalf("symmetric position must be symmetric: G=%v C=%v", r.Guaranteed[:2], r.Contested[:2])
	}
	if r.Attack[0] != 0 || r.Attack[1] != 0 {
		t.Fatal("equal lengths win no contested cells")
	}
	r = Compute(st(t, "you 100 2,5 1,5 0,5 0,6\nsnake mirror 100 8,5 9,5 10,5"), 0)
	if r.Attack[0] != r.Contested[0] || r.Attack[1] != 0 {
		t.Fatalf("longer snake wins every contested cell: A=%v C=%v", r.Attack[:2], r.Contested[:2])
	}
}

// Just-eaten tail: a duplicated tail frees one turn later (R4).
func TestJustEatenTail(t *testing.T) {
	// Head boxed in a corner: the only exit is our own tail cell.
	free := Compute(st(t, "size 3 3\nyou 100 0,0 0,1 1,1 1,0"), 0)
	ate := Compute(st(t, "size 3 3\nyou 100 0,0 0,1 1,1 1,0 1,0"), 0)
	if free.SafeExits[0] != 1 || ate.SafeExits[0] != 0 {
		t.Fatalf("exits free=%d ate=%d", free.SafeExits[0], ate.SafeExits[0])
	}
}

// Health carried through the fill: a hazard wall stops a weak snake; food on the
// wall (R5) opens it.
func TestHazardCostAndFoodReset(t *testing.T) {
	wall := "hazard 3,0 3,1 3,2 3,3 3,4 3,5 3,6 3,7 3,8 3,9 3,10\ndamage 14\n"
	blocked := Compute(st(t, wall+"you 10 1,5 1,4 1,3"), 0)
	if blocked.Reach(0) >= 33 {
		t.Fatalf("health 10 must not cross a 14-damage wall, reach=%d", blocked.Reach(0))
	}
	open := Compute(st(t, wall+"food 3,5\nyou 10 1,5 1,4 1,3"), 0)
	if open.Reach(0) <= 40 {
		t.Fatalf("food in the wall must open it, reach=%d", open.Reach(0))
	}
}

func TestTrappedAndFoodDist(t *testing.T) {
	r := Compute(st(t, "size 5 5\nyou 100 0,0 1,0 2,0\nsnake wall 100 0,1 1,1 2,1 3,1 3,0"), 0)
	if !r.Trapped[0] {
		t.Fatalf("boxed-in snake must be trapped, reach=%d", r.Reach(0))
	}
	r = Compute(st(t, "you 100 5,5 5,4 5,3\nfood 5,8"), 0)
	if r.FoodDist[0] != 3 {
		t.Fatalf("food dist %d", r.FoodDist[0])
	}
}

// A pocket behind one cut cell that an opponent can reach is discounted.
func TestCutCellDiscount(t *testing.T) {
	// Row y=1 is a wall of opponent body except the gap at 9,1; the region
	// below it is reachable only through that gap, which the opponent head can seal.
	r := Compute(st(t, "you 100 9,3 9,4 9,5\nsnake wall 100 8,2 8,1 7,1 6,1 5,1 4,1 3,1 2,1 1,1 0,1 0,2 0,3"), 0)
	if r.CutCells == 0 || r.Robust >= r.Reach(0) {
		t.Fatalf("expected a sealable cut: cuts=%d robust=%d reach=%d", r.CutCells, r.Robust, r.Reach(0))
	}
}

// Metamorphic: rotating/reflecting the board or permuting opponents must
// transform the result correspondingly.
func TestMetamorphic(t *testing.T) {
	for seed := int64(1); seed <= 80; seed++ {
		s := testgen.Position(seed, 11, 11, 4, int(seed%60), board.Rules{Name: "standard"})
		if !s.Snakes[0].Alive() {
			continue
		}
		base := Compute(s, 0)
		for _, sym := range []board.Symmetry{board.Rot90, board.ReflectX} {
			if got := Compute(s.Apply(sym), 0); got != base {
				t.Fatalf("seed %d sym %d:\n got %+v\nwant %+v", seed, sym, got, base)
			}
		}
		perm := []int{2, 0, 1}
		got := Compute(s.PermuteOpponents(perm), 0)
		for k, old := range perm {
			if got.Guaranteed[k+1] != base.Guaranteed[old+1] || got.Contested[k+1] != base.Contested[old+1] ||
				got.Attack[k+1] != base.Attack[old+1] || got.FoodDist[k+1] != base.FoodDist[old+1] {
				t.Fatalf("seed %d: permutation broke opponent %d", seed, old+1)
			}
		}
		if got.Guaranteed[0] != base.Guaranteed[0] || got.Robust != base.Robust {
			t.Fatalf("seed %d: permutation changed our result", seed)
		}
	}
}

func BenchmarkCompute11(b *testing.B) {
	s := testgen.Position(3, 11, 11, 4, 40, board.Rules{Name: "standard"})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Compute(s, 0)
	}
}

func BenchmarkCompute19(b *testing.B) {
	s := testgen.Position(3, 19, 19, 2, 60, board.Rules{Name: "royale", Royale: true})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Compute(s, 0)
	}
}
