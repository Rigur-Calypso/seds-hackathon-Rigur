package envelope

import (
	"context"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
)

// One turn before the diagnosed sandwich: stepping down toward the wall looks
// safe to one-turn lookahead, but if both longer neighbours also step down every
// exit dies. With the Threat Graph on, that candidate must lose value and carry
// forced outcomes; with it off, nothing is marked.
func TestThreatGraphMarksTheSandwichMove(t *testing.T) {
	gs := fixture.MustState(`size 11 11
turn 140
you 58 5,1 5,2 5,3 5,4 5,5
snake s1 91 6,2 6,3 6,4 6,5 7,5 8,5 8,4 8,3 8,2
snake s2 69 4,2 4,3 4,4 4,5 4,6 4,7
food 9,7 1,0 3,8`)
	s, ok := board.FromAPI(gs)
	if !ok {
		t.Fatal("you not found")
	}
	safe := legal.Safe(s, 0)

	off := config.Defaults()
	on := config.Defaults()
	on.ThreatGraph = true

	co, err := Evaluate(context.Background(), s, &off, safe)
	if err != nil {
		t.Fatal(err)
	}
	cn, err := Evaluate(context.Background(), s, &on, safe)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range co {
		if co[i].Forced != 0 {
			t.Fatalf("threat graph off must not mark outcomes: %+v", co[i])
		}
		if cn[i].Dir == board.Down {
			found = true
			if cn[i].Forced == 0 || !(cn[i].Score < co[i].Score) {
				t.Fatalf("down: forced=%d score on=%.4f off=%.4f — sandwich not penalised", cn[i].Forced, cn[i].Score, co[i].Score)
			}
		}
	}
	if !found {
		t.Fatalf("down should be a candidate; safe=%v", safe)
	}
}
