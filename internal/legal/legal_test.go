package legal

import (
	"reflect"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
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

func has(ds []board.Dir, d board.Dir) bool {
	for _, x := range ds {
		if x == d {
			return true
		}
	}
	return false
}

func TestCornerOnlyUp(t *testing.T) {
	got := Safe(st(t, "you 100 0,0 1,0 2,0\nsnake far 100 9,9 9,8 9,7"), 0)
	if !reflect.DeepEqual(got, []board.Dir{board.Up}) {
		t.Fatalf("got %v", got)
	}
}

func TestR4_TailLayer(t *testing.T) {
	const far = "\nsnake far 100 0,10 1,10 2,10"
	if !has(Safe(st(t, "you 100 5,5 5,4 4,4 4,5"+far), 0), board.Left) {
		t.Fatal("vacating tail must be legal")
	}
	if has(Safe(st(t, "you 100 5,5 5,4 4,4 4,5 4,5"+far), 0), board.Left) {
		t.Fatal("duplicated tail must be blocked")
	}
}

// R3: head threats live in their own layer; they flag but never block.
func TestR3_HeadLayerSeparate(t *testing.T) {
	s := st(t, "you 100 5,5 5,4 5,3\nsnake rival 100 5,7 5,8 5,9\nsnake small 100 7,5 8,5")
	info := Analyze(s, Blocked(s), 0)
	if !info[board.Up].H2HLose || info[board.Up].Blocked {
		t.Fatalf("up: %+v", info[board.Up])
	}
	if !info[board.Right].H2HWin || info[board.Right].H2HLose {
		t.Fatalf("right: %+v", info[board.Right])
	}
	if !has(Safe(s, 0), board.Up) {
		t.Fatal("a head-to-head risk is not a blocked cell")
	}
}

func TestCurrentHeadsBecomeNecks(t *testing.T) {
	s := st(t, "you 100 5,5 5,4 5,3\nsnake o 100 5,6 6,6 7,6")
	if has(Safe(s, 0), board.Up) {
		t.Fatal("an opponent's current head cell is its neck next turn")
	}
}

func TestLethalHazardExcluded(t *testing.T) {
	s := st(t, "you 10 5,5 5,4 5,3\nsnake far 100 0,10 1,10 2,10\nhazard 5,6")
	if has(Safe(s, 0), board.Up) {
		t.Fatal("lethal hazard move must be excluded")
	}
	s = st(t, "you 10 5,5 5,4 5,3\nsnake far 100 0,10 1,10 2,10\nhazard 5,6\nfood 5,6")
	if !has(Safe(s, 0), board.Up) {
		t.Fatal("hazard with food is safe (R5)")
	}
}

func TestWrappedMoves(t *testing.T) {
	s := st(t, "rules wrapped\nyou 100 0,5 1,5 2,5\nsnake a 100 0,6 0,7 0,8\nsnake b 100 0,4 0,3 0,2")
	if got := Safe(s, 0); !reflect.DeepEqual(got, []board.Dir{board.Left}) {
		t.Fatalf("wrapped: got %v", got)
	}
}

func TestFoodDistancesSinglePass(t *testing.T) {
	s := st(t, "you 100 5,5 5,4 5,3\nsnake far 100 0,10 1,10 2,10\nfood 5,8 0,0")
	d := FoodDistances(s, Blocked(s))
	if got := d[s.Idx(board.Point{X: 5, Y: 6})]; got != 2 {
		t.Fatalf("dist from 5,6 = %d, want 2", got)
	}
	if got := d[s.Idx(board.Point{X: 1, Y: 1})]; got != 2 {
		t.Fatalf("dist from 1,1 = %d, want 2", got)
	}
}
