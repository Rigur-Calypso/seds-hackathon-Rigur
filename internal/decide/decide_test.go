package decide_test

import (
	"context"
	"errors"
	"testing"
	"time"

	seds "github.com/Rigur-Calypso/seds-hackathon-Rigur"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/decide"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/search"
)

func profiles(t *testing.T) *config.Profiles {
	t.Helper()
	ps, err := config.Load(seds.ConfigFS)
	if err != nil {
		t.Fatalf("shipped configs must load strictly: %v", err)
	}
	return ps
}

func valid(m string) bool { _, ok := board.ParseDir(m); return ok }

// Every fixture asserts its move, for both the fallback-only engine and the
// full engine. Fixtures are the specification: never delete one.
func TestFixtures(t *testing.T) {
	fx, err := fixture.LoadDir("../../testdata/fixtures")
	if err != nil {
		t.Fatal(err)
	}
	if len(fx) == 0 {
		t.Fatal("no fixtures")
	}
	ps := profiles(t)
	engines := []struct {
		name string
		eng  *decide.Engine
	}{
		{"fallback", decide.New(ps, nil)},
		{"full", decide.New(ps, search.Evaluate)},
	}
	for _, f := range fx {
		for _, e := range engines {
			f, e := f, e
			t.Run(f.Name+"/"+e.name, func(t *testing.T) {
				d := e.eng.Decide(context.Background(), f.State)
				ok := valid(d.Move)
				if len(f.Expect) > 0 {
					match := false
					for _, m := range f.Expect {
						match = match || m == d.Move
					}
					ok = ok && match
				}
				for _, m := range f.Reject {
					ok = ok && m != d.Move
				}
				if !ok {
					t.Fatalf("got %s (%s scores=%v err=%s) expect=%v reject=%v\n%s",
						d.Move, d.Reason, d.Scores, d.Err, f.Expect, f.Reject, fixture.Format(f.State))
				}
			})
		}
	}
}

func TestPanickingEvaluatorReturnsFallback(t *testing.T) {
	eng := decide.New(nil, func(context.Context, *board.State, *config.Params, []board.Dir) (board.Dir, decide.Decision, error) {
		panic("boom")
	})
	d := eng.Decide(context.Background(), fixture.MustState("you 100 5,5 5,4 5,3\nsnake far 100 9,9 9,8 9,7\nfood 5,8"))
	if d.Reason != decide.ReasonPanic || d.Move != "up" {
		t.Fatalf("got %+v", d)
	}
}

func TestErroringEvaluatorReturnsFallback(t *testing.T) {
	eng := decide.New(nil, func(context.Context, *board.State, *config.Params, []board.Dir) (board.Dir, decide.Decision, error) {
		return board.Down, decide.Decision{}, errors.New("nope")
	})
	d := eng.Decide(context.Background(), fixture.MustState("you 100 5,5 5,4 5,3\nsnake far 100 9,9 9,8 9,7\nfood 5,8"))
	if d.Reason != decide.ReasonFallback || d.Move != "up" {
		t.Fatalf("got %+v", d)
	}
}

func TestNilAndGarbageStates(t *testing.T) {
	eng := decide.New(profiles(t), search.Evaluate)
	if d := eng.Decide(context.Background(), nil); !valid(d.Move) {
		t.Fatal("nil state")
	}
	if d := eng.Decide(context.Background(), fixture.MustState("size 1 1\nyou 100 0,0")); !valid(d.Move) {
		t.Fatal("1x1 board")
	}
}

// The cooperative deadline is honoured on the heaviest realistic board.
func TestDeadlineRespected(t *testing.T) {
	eng := decide.New(profiles(t), search.Evaluate)
	gs := fixture.MustState(`rules royale
size 19 19
turn 80
safe 2 2 16 16
you 90 9,9 9,8 9,7 9,6 9,5 8,5 7,5 6,5
snake a 90 5,12 5,13 5,14 5,15 6,15 7,15
food 3,3 15,15 9,14`)
	for _, budget := range []time.Duration{5 * time.Millisecond, 40 * time.Millisecond, 150 * time.Millisecond} {
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		start := time.Now()
		d := eng.Decide(ctx, gs)
		el := time.Since(start)
		cancel()
		if !valid(d.Move) || el > budget+30*time.Millisecond {
			t.Fatalf("budget %v: elapsed %v move %s reason %s", budget, el, d.Move, d.Reason)
		}
	}
}
