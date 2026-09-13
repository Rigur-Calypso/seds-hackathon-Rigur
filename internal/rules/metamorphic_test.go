package rules_test

import (
	"reflect"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/rules"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/testgen"
)

// Metamorphic: resolving a rotated/reflected board equals rotating the result.
func TestResolveMetamorphic(t *testing.T) {
	for seed := int64(1); seed <= 60; seed++ {
		s := testgen.Position(seed, 11, 11, 4, int(seed%40), board.Rules{Name: "standard"})
		moves := make([]board.Dir, len(s.Snakes))
		for i := range moves {
			moves[i] = board.Dir((seed + int64(i)*7) % 4)
		}
		want := rules.Resolve(s, moves, rules.Options{})
		for _, sym := range []board.Symmetry{board.Rot90, board.ReflectX} {
			tm := make([]board.Dir, len(moves))
			for i, m := range moves {
				tm[i] = sym.Dir(m)
			}
			got := rules.Resolve(s.Apply(sym), tm, rules.Options{})
			exp := want.Apply(sym)
			for i := range got.Snakes {
				if !reflect.DeepEqual(got.Snakes[i], exp.Snakes[i]) {
					t.Fatalf("seed %d sym %d snake %d: got %+v want %+v", seed, sym, i, got.Snakes[i], exp.Snakes[i])
				}
			}
			if !reflect.DeepEqual(got.Food, exp.Food) {
				t.Fatalf("seed %d sym %d food mismatch", seed, sym)
			}
		}
	}
}

func BenchmarkResolve4Snakes(b *testing.B) {
	s := testgen.Position(7, 11, 11, 4, 30, board.Rules{Name: "standard"})
	moves := []board.Dir{board.Up, board.Down, board.Left, board.Right}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = rules.Resolve(s, moves, rules.Options{})
	}
}
