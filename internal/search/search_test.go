package search

import (
	"context"
	"testing"
	"time"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/decide"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
)

const duelBoard = `rules royale
size 19 19
turn 80
safe 2 2 16 16
you 90 9,9 9,8 9,7 9,6 9,5 8,5 7,5 6,5
snake a 90 5,12 5,13 5,14 5,15 6,15 7,15
food 3,3 15,15 9,14`

func duelState(t *testing.T) *board.State {
	t.Helper()
	s, ok := board.FromAPI(fixture.MustState(duelBoard))
	if !ok {
		t.Fatal("you not found")
	}
	return s
}

func TestDuelUsedOnlyAtMinDepth(t *testing.T) {
	s := duelState(t)
	safe := legal.Safe(s, 0)
	p := config.Defaults()
	p.DuelEnabled, p.DuelMaxDepth = true, 2

	p.DuelMinDepth = 1
	if _, info, err := Evaluate(context.Background(), s, &p, safe); err != nil || info.Reason != decide.ReasonDuel || info.Depth != 2 {
		t.Fatalf("min depth reachable: want duel at depth 2, got %+v err=%v", info, err)
	}
	p.DuelMinDepth = 3
	if _, info, err := Evaluate(context.Background(), s, &p, safe); err != nil || info.Reason != decide.ReasonEvaluated {
		t.Fatalf("min depth unreachable: want TVAE move, got %+v err=%v", info, err)
	}
}

// A deadline that cuts the duel search short must yield the TVAE move, not an error.
func TestShortDeadlineKeepsTVAEMove(t *testing.T) {
	s := duelState(t)
	safe := legal.Safe(s, 0)
	p := config.Defaults()
	p.DuelEnabled, p.DuelMaxDepth, p.DuelMinDepth = true, 12, 12
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, info, err := Evaluate(ctx, s, &p, safe)
	if err != nil || info.Reason != decide.ReasonEvaluated {
		t.Fatalf("got %+v err=%v", info, err)
	}
	if el := time.Since(start); el > 80*time.Millisecond {
		t.Fatalf("deadline overrun: %v", el)
	}
}
