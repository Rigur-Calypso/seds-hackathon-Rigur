package envelope

import (
	"context"
	"math"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/testgen"
)

func TestCVaRPrefersSafeOverRiskyMean(t *testing.T) {
	p := config.Defaults()
	p.RiskMean, p.RiskCVaR, p.RiskMin, p.CVaRAlpha = 0, 1, 0, 0.25
	// risky: better mean (0.51 > 0.3) but a 10% forced-loss path.
	risky := Aggregate(board.Up, []outcome{{-3, 0.1}, {0.9, 0.9}}, &p)
	safe := Aggregate(board.Down, []outcome{{0.3, 1}}, &p)
	if risky.Mean <= safe.Mean || risky.Score >= safe.Score {
		t.Fatalf("CVaR must reject a forced-loss path: risky=%+v safe=%+v", risky, safe)
	}
}

// Step 6 gate: at most 256 first-ply outcomes per turn.
func TestOutcomeBudget(t *testing.T) {
	p := config.Defaults()
	for seed := int64(1); seed <= 50; seed++ {
		s := testgen.Position(seed, 11, 11, 4, 10, board.Rules{Name: "standard"})
		if !s.Snakes[0].Alive() {
			continue
		}
		safe := legal.Safe(s, 0)
		if len(safe) == 0 {
			continue
		}
		cands, err := Evaluate(context.Background(), s, &p, safe)
		if err != nil {
			t.Fatal(err)
		}
		total := 0
		for _, c := range cands {
			total += c.Outcomes
		}
		if total > 256 {
			t.Fatalf("seed %d: %d outcomes", seed, total)
		}
	}
}

// Metamorphic: candidate scores transform with the board.
func TestEnvelopeMetamorphic(t *testing.T) {
	p := config.Defaults()
	p.LocalityRadius = 1000
	for seed := int64(1); seed <= 40; seed++ {
		s := testgen.Position(seed, 11, 11, 4, 5+int(seed%30), board.Rules{Name: "standard"})
		if !s.Snakes[0].Alive() {
			continue
		}
		safe := legal.Safe(s, 0)
		if len(safe) == 0 {
			continue
		}
		base, err := Evaluate(context.Background(), s, &p, safe)
		if err != nil {
			t.Fatal(err)
		}
		for _, sym := range []board.Symmetry{board.Rot90, board.ReflectX} {
			ts := s.Apply(sym)
			tsafe := make([]board.Dir, len(safe))
			for i, d := range safe {
				tsafe[i] = sym.Dir(d)
			}
			got, err := Evaluate(context.Background(), ts, &p, tsafe)
			if err != nil {
				t.Fatal(err)
			}
			for i := range base {
				if got[i].Dir != sym.Dir(base[i].Dir) || math.Abs(got[i].Score-base[i].Score) > 1e-9 {
					t.Fatalf("seed %d sym %d cand %d: got %+v want %+v", seed, sym, i, got[i], base[i])
				}
			}
		}
	}
}

func BenchmarkEvaluate4Snakes(b *testing.B) {
	p := config.Defaults()
	s := testgen.Position(11, 11, 11, 4, 20, board.Rules{Name: "standard"})
	safe := legal.Safe(s, 0)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = Evaluate(context.Background(), s, &p, safe)
	}
}
