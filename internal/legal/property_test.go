package legal_test

import (
	"math/rand"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/rules"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/testgen"
)

// Property (R4, Codex review): a duplicated tail never vacates. On random
// positions, give a random live snake a duplicated tail: the legal layer must
// block that cell, and after any simultaneous turn in which the snake survives
// the cell must still be part of its body.
func TestDuplicatedTailNeverVacates(t *testing.T) {
	checked := 0
	for seed := int64(1); seed <= 400; seed++ {
		s := testgen.Position(seed, 11, 11, 4, int(seed%60), board.Rules{Name: "standard"})
		rng := rand.New(rand.NewSource(seed))
		var live []int
		for i := range s.Snakes {
			if s.Snakes[i].Alive() && len(s.Snakes[i].Body) >= 2 {
				live = append(live, i)
			}
		}
		if len(live) == 0 {
			continue
		}
		k := live[rng.Intn(len(live))]
		sn := &s.Snakes[k]
		tail := sn.Body[len(sn.Body)-1]
		if sn.TailVacates() {
			sn.Body = append(sn.Body, tail)
		}
		if legal.Blocked(s)[s.Idx(tail)] == false {
			t.Fatalf("seed %d: duplicated tail of snake %d not blocked", seed, k)
		}
		moves := make([]board.Dir, len(s.Snakes))
		for i := range moves {
			moves[i] = board.Dir(rng.Intn(4))
		}
		next := rules.Resolve(s, moves, rules.Options{})
		if !next.Snakes[k].Alive() {
			continue
		}
		found := false
		for _, p := range next.Snakes[k].Body {
			found = found || p == tail
		}
		if !found {
			t.Fatalf("seed %d: snake %d's duplicated tail vacated after one move", seed, k)
		}
		checked++
	}
	if checked < 50 {
		t.Fatalf("property exercised on only %d positions", checked)
	}
}

// Property: a move the legal layer calls safe never ends in an immediate wall,
// body or starvation death when resolved against opponents that stand still in
// a way that cannot reach our cell (R3/R4/R1/R6 consistency with the resolver).
func TestSafeMovesSurviveWhenUncontested(t *testing.T) {
	for seed := int64(1); seed <= 300; seed++ {
		s := testgen.Position(seed, 11, 11, 4, int(seed%70), board.Rules{Name: "standard"})
		if !s.Snakes[0].Alive() {
			continue
		}
		blocked := legal.Blocked(s)
		for _, in := range legal.Analyze(s, blocked, 0) {
			if !in.Safe() || in.H2HLose || in.H2HWin {
				continue
			}
			moves := make([]board.Dir, len(s.Snakes))
			for i := range s.Snakes {
				if len(s.Snakes[i].Body) > 0 {
					moves[i] = legal.DefaultMove(&s.Snakes[i])
				}
			}
			moves[0] = in.Dir
			next := rules.Resolve(s, moves, rules.Options{})
			c := next.Snakes[0].Cause
			if c == board.ByOutOfBounds || c == board.BySelfCollision || c == board.ByOutOfHealth || c == board.ByHazard {
				t.Fatalf("seed %d: safe move %v died by %v", seed, in.Dir, c)
			}
		}
	}
}
