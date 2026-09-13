package fallback

import (
	"fmt"
	"testing"
	"time"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/testgen"
)

func big19() *board.State {
	s := &board.State{W: 19, H: 19, Rules: board.Rules{Name: "royale", Royale: true, HazardDamage: 14, ShrinkEveryN: 25}}
	for k := 0; k < 4; k++ {
		var body []board.Point
		y0 := 1 + k*4
		for x := 17; x >= 1; x-- {
			body = append(body, board.Point{X: x, Y: y0})
		}
		for x := 1; x <= 17; x++ {
			body = append(body, board.Point{X: x, Y: y0 + 1})
		}
		s.Snakes = append(s.Snakes, board.Snake{ID: fmt.Sprint(k), Body: body, Health: 80})
	}
	s.Food = []board.Point{{X: 9, Y: 17}, {X: 0, Y: 0}}
	s.Hazard = board.RingHazards(19, 19, board.Rect{MinX: 1, MinY: 0, MaxX: 18, MaxY: 18})
	return s
}

// Step 3 gate: the fallback must run under 1 ms.
func TestFallbackUnderOneMillisecond(t *testing.T) {
	s := big19()
	const n = 300
	start := time.Now()
	for i := 0; i < n; i++ {
		Best(s)
	}
	if avg := time.Since(start) / n; avg > time.Millisecond {
		t.Fatalf("fallback avg %v > 1ms", avg)
	}
}

// Never off-board, never the neck, never a body cell when a safe move exists.
func TestFallbackNeverSuicidesWhenAvoidable(t *testing.T) {
	for seed := int64(1); seed <= 300; seed++ {
		s := testgen.Position(seed, 11, 11, 4, int(seed%80), board.Rules{Name: "standard"})
		if !s.Snakes[0].Alive() {
			continue
		}
		d := Best(s)
		q, ok := s.Step(s.Snakes[0].Head(), d)
		safeExists := false
		for _, dd := range board.AllDirs {
			if qq, ok2 := s.Step(s.Snakes[0].Head(), dd); ok2 && !blockedAt(s, qq) {
				safeExists = true
			}
		}
		if safeExists && (!ok || blockedAt(s, q)) {
			t.Fatalf("seed %d: fallback chose %v into a blocked/off-board cell", seed, d)
		}
	}
}

func blockedAt(s *board.State, p board.Point) bool {
	for i := range s.Snakes {
		sn := &s.Snakes[i]
		if !sn.Alive() {
			continue
		}
		last := len(sn.Body) - 1
		if !sn.TailVacates() {
			last = len(sn.Body)
		}
		for k := 0; k < last; k++ {
			if sn.Body[k] == p {
				return true
			}
		}
	}
	return false
}

func BenchmarkFallback19(b *testing.B) {
	s := big19()
	for i := 0; i < b.N; i++ {
		Best(s)
	}
}
