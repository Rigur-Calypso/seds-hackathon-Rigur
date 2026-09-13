package main

import "testing"

func TestPromotionGate(t *testing.T) {
	good := &Paired{N: 500, MeanDiffPoints: 0.4, PPoints: 0.001, CIPoints: [2]float64{0.2, 0.6}}
	clean := &Summary{}
	cases := []struct {
		name string
		p    *Paired
		b    *Summary
		pass bool
	}{
		{"clear improvement", good, clean, true},
		{"significant regression", &Paired{N: 500, MeanDiffPoints: -0.4, PPoints: 0.001, CIPoints: [2]float64{-0.6, -0.2}, Significant: true}, clean, false},
		{"CI lower bound at or below zero", &Paired{N: 500, MeanDiffPoints: 0.2, PPoints: 0.04, CIPoints: [2]float64{-0.01, 0.4}}, clean, false},
		{"p not below 0.05", &Paired{N: 500, MeanDiffPoints: 0.2, PPoints: 0.2, CIPoints: [2]float64{0.01, 0.4}}, clean, false},
		{"significant on wins only", &Paired{N: 500, MeanDiffPoints: 0.1, PPoints: 0.3, CIPoints: [2]float64{-0.1, 0.3}, PWin: 0.01, Significant: true}, clean, false},
		{"B timed out", good, &Summary{Timeouts: 1}, false},
		{"no comparison", nil, nil, false},
	}
	for _, c := range cases {
		if g := promotionGate(c.p, c.b); g.Pass != c.pass {
			t.Errorf("%s: pass=%v want %v (reasons %v)", c.name, g.Pass, c.pass, g.Reasons)
		}
	}
}

// The two-sided significantAt05 flag fires on a clear regression; the
// promotion gate must never pass one.
func TestPairedRegressionNeverPromotes(t *testing.T) {
	a, b := make([]GameResult, 60), make([]GameResult, 60)
	for i := range a {
		a[i].Points = 10
		b[i].Points = []float64{1, 3, 6}[i%3]
	}
	p := paired(a, b, 500)
	if !p.Significant {
		t.Fatalf("setup: a clear regression should be two-sided significant: %+v", p)
	}
	if g := promotionGate(p, &Summary{}); g.Pass {
		t.Fatalf("regression promoted: %+v", p)
	}
}

// A grid promotes only if every cell passes on its own.
func TestGridGateRequiresEveryCell(t *testing.T) {
	cell := func(pass bool, shrink string) GridCell {
		g := Gate{Pass: pass}
		if !pass {
			g.Reasons = []string{"mean points difference not positive"}
		}
		return GridCell{Settings: map[string]string{"shrink": shrink}, Report: Report{Gate: &g}}
	}
	if g := gridGate([]GridCell{cell(true, "15"), cell(true, "25")}); !g.Pass {
		t.Fatalf("all cells pass but grid failed: %v", g.Reasons)
	}
	if g := gridGate([]GridCell{cell(true, "15"), cell(false, "25")}); g.Pass || len(g.Reasons) != 1 {
		t.Fatalf("one failing cell must fail the grid with one reason: %+v", g)
	}
	if g := gridGate([]GridCell{{Settings: map[string]string{}, Report: Report{}}}); g.Pass {
		t.Fatal("a cell without a comparison must fail the grid")
	}
	if g := gridGate(nil); g.Pass {
		t.Fatal("an empty grid must not pass")
	}
}
