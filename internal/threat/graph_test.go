package threat

import (
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
)

func st(t *testing.T, text string) *board.State {
	t.Helper()
	s, ok := board.FromAPI(fixture.MustState(text))
	if !ok {
		t.Fatal("you not found")
	}
	return s
}

func params() *config.Params {
	p := config.Defaults()
	p.ThreatGraph, p.ThreatRadius, p.ThreatMaxOpponents, p.ThreatMaxEvals = true, 4, 2, 1500
	return &p
}

// A real sandwich from the loss diagnostics (qualifying seed 5000131, turn 141):
// our head sits between two longer heads at the bottom wall. One joint reply —
// both opponents step down — kills every exit.
const sandwich = `size 11 11
you 57 5,0 5,1 5,2 5,3 5,4
snake s1 90 6,1 6,2 6,3 6,4 6,5 7,5 8,5 8,4 8,3
snake s2 68 4,1 4,2 4,3 4,4 4,5 4,6
food 9,7 1,0 3,8`

func TestSandwichIsForced(t *testing.T) {
	s := st(t, sandwich)
	p := params()
	if !Triggered(s, p) {
		t.Fatal("two nearby opponents must trigger the check")
	}
	b := Budget{Left: p.ThreatMaxEvals}
	if r := Check(s, p, &b); !r.Checked || !r.Forced {
		t.Fatalf("sandwich not detected: %+v", r)
	}
}

// Same shape, but one side is guarded by a SHORTER snake: stepping into it wins
// the head-to-head (R2), so that exit survives and nothing is forced.
func TestShorterSideIsNotForced(t *testing.T) {
	s := st(t, `size 11 11
you 57 5,0 5,1 5,2 5,3 5,4
snake s1 90 6,1 6,2 6,3 6,4 6,5 7,5 8,5 8,4 8,3
snake small 68 4,1 4,2 4,3
food 9,7 1,0 3,8`)
	p := params()
	b := Budget{Left: p.ThreatMaxEvals}
	if r := Check(s, p, &b); !r.Checked || r.Forced {
		t.Fatalf("an exit guarded only by a shorter head is not a squeeze: %+v", r)
	}
}

func TestBudgetExhaustionIsNeverForced(t *testing.T) {
	s := st(t, sandwich)
	p := params()
	b := Budget{Left: 1}
	if r := Check(s, p, &b); r.Checked || r.Forced {
		t.Fatalf("an unfinished check must not mark forced: %+v", r)
	}
}

func TestOpenBoardDoesNotTrigger(t *testing.T) {
	s := st(t, "you 100 5,5 5,4 5,3\nsnake far 100 0,10 1,10 2,10\nfood 9,1")
	if Triggered(s, params()) {
		t.Fatal("no opponent within the radius: no check")
	}
}

// One nearby longer head at the mouth of a one-cell corridor triggers even
// without a second opponent.
func TestCorridorWithOneOpponentTriggers(t *testing.T) {
	s := st(t, `size 11 11
you 90 0,5 0,4 0,3 0,2
snake wall 90 1,6 1,5 1,4 1,3 1,2 1,1 1,0
snake hunter 90 0,8 0,9 0,10 1,10 2,10
food 9,9`)
	if !Triggered(s, params()) {
		t.Fatal("single-exit corridor with a nearby head must trigger")
	}
}

// room: space a shorter snake reaches first is still ours (it loses the
// head-to-head), while an equal-length head takes contested cells.
func TestRoomIgnoresShorterClaims(t *testing.T) {
	short := st(t, "size 7 7\nyou 90 0,0 1,0 2,0 3,0\nsnake small 90 0,2 0,3 0,4\nfood 6,6")
	equal := st(t, "size 7 7\nyou 90 0,0 1,0 2,0 3,0\nsnake peer 90 0,2 0,3 0,4 0,5\nfood 6,6")
	rs, re := room(short, 100), room(equal, 100)
	if !(rs > re) {
		t.Fatalf("a shorter snake must not take space from us: room with shorter=%d, with equal=%d", rs, re)
	}
	if got := room(short, 3); got != 3 {
		t.Fatalf("room must stop at its limit, got %d", got)
	}
}

// With ThreatTrapRefutes off, only death refutes a move: walking into a sealed
// pocket (alive, but no room for our length) no longer counts.
func TestTrapRefutationSwitch(t *testing.T) {
	s := st(t, `size 7 7
you 90 0,5 0,4 0,3 0,2 0,1 0,0
snake wall 90 1,0 1,1 1,2 1,3 1,4 1,5 1,6 2,6 3,6 4,6 5,6 6,6 6,5 6,4
food 4,2`)
	moves := []board.Dir{board.Up, board.Right}
	p := params()
	p.ThreatTrapRefutes = true
	if !refuted(s, moves, p) {
		t.Fatal("alive in a sealed pocket must refute when trap refutation is on")
	}
	p.ThreatTrapRefutes = false
	if refuted(s, moves, p) {
		t.Fatal("with trap refutation off, surviving the move is not a refutation")
	}
}

// With ThreatLongerOnly, a neighbourhood of only shorter snakes enumerates no
// replies at all: nothing is forced and no budget is spent.
func TestLongerOnlyIgnoresShorterOpponents(t *testing.T) {
	s := st(t, "size 11 11\nyou 90 0,5 0,4 0,3 0,2\nsnake small 90 1,6 2,6\nfood 9,9")
	p := params()
	p.ThreatLongerOnly = true
	b := Budget{Left: p.ThreatMaxEvals}
	if r := Check(s, p, &b); !r.Checked || r.Forced || r.Evals != 0 || b.Left != p.ThreatMaxEvals {
		t.Fatalf("shorter-only neighbourhood: %+v budget left %d", r, b.Left)
	}
	p.ThreatLongerOnly = false
	b = Budget{Left: p.ThreatMaxEvals}
	if r := Check(s, p, &b); r.Evals == 0 {
		t.Fatalf("without the switch the shorter snake's replies are enumerated: %+v", r)
	}
}
