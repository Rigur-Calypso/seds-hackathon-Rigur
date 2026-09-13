package envelope

import (
	"context"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
)

// Regression (Codex review): EnvelopeShrinkPessimistic was computed in
// Evaluate but never passed to the resolver, so toggling it changed nothing.
// One turn before a shrink, with our head on the board edge, the pessimistic
// ring puts every edge move in the storm, so each candidate must score lower
// than with the ring kept as-is.
func TestEnvelopeShrinkPessimisticReachesResolver(t *testing.T) {
	gs := fixture.MustState(`rules royale
turn 24
shrink 25
damage 14
you 100 0,5 1,5 2,5
snake far 100 9,9 9,8 9,7
food 5,5`)
	s, ok := board.FromAPI(gs)
	if !ok {
		t.Fatal("you not found")
	}
	safe := legal.Safe(s, 0)
	if len(safe) == 0 {
		t.Fatal("no safe moves in fixture")
	}

	keep := config.Defaults()
	pess := config.Defaults()
	pess.EnvelopeShrinkPessimistic = true

	kc, err := Evaluate(context.Background(), s, &keep, safe)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := Evaluate(context.Background(), s, &pess, safe)
	if err != nil {
		t.Fatal(err)
	}
	for i := range kc {
		if kc[i].Dir != pc[i].Dir {
			t.Fatalf("candidate order differs: %v vs %v", kc[i].Dir, pc[i].Dir)
		}
		if !(pc[i].Score < kc[i].Score) {
			t.Fatalf("move %v: pessimistic score %.4f must be below keep score %.4f — option not reaching the resolver",
				kc[i].Dir, pc[i].Score, kc[i].Score)
		}
	}
}
