package brain

import (
	"context"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/sim"
)

// Constrictor 7×7: our body walls off row 3. Below it, the opponent's body
// walls off column 2 and its head owns the 11 cells to the right, so no one can
// ever reach our space. Up leads to 21 cells; down to a 6-cell pocket that
// fills long before the opponent dies and its body frees: up survives longer.
// (With a smaller opponent pocket the opponent dies first, its body vanishes
// and both sides survive equally — the search models that correctly.)
const sealedConstrictor = `rules constrictor
size 7 7
you 100 0,3 1,3 2,3 3,3 4,3 5,3 6,3 6,3
snake op 100 3,2 2,2 2,1 2,0 2,0`

func searcherFor(t *testing.T, text string) *Searcher {
	t.Helper()
	s := state(t, text)
	p := params()
	p.SearchEndgame = true
	g := sim.NewGame(s, sim.ShrinkPessimistic)
	st, ok := sim.New(s, g)
	if !ok {
		t.Fatal("sim.New failed")
	}
	se := new(Searcher)
	se.reset(context.Background(), p, g, st)
	return se
}

func TestIsolation(t *testing.T) {
	if se := searcherFor(t, sealedConstrictor); !se.fill.Isolated(se.st) {
		t.Fatal("sealed constrictor regions must be isolated")
	}
	if se := searcherFor(t, "you 100 5,5 5,4 5,3\nsnake a 100 1,1 1,2 1,3"); se.fill.Isolated(se.st) {
		t.Fatal("an open board is not isolated")
	}
	// Standard rules: the same walls release (R4), so the regions will touch.
	if se := searcherFor(t, "size 7 7\nyou 100 0,3 1,3 2,3 3,3 4,3 5,3 6,3\nsnake op 100 5,2 4,2 4,1 4,0"); se.fill.Isolated(se.st) {
		t.Fatal("bodies that release are not walls forever")
	}
}

func TestReachCountConstrictor(t *testing.T) {
	se := searcherFor(t, sealedConstrictor)
	if got := se.fill.ReachCount(se.st, 0); got != 27 {
		t.Fatalf("our reachable cells: got %d want 27 (21 above + 6 below)", got)
	}
}

func TestEndgameFilterKeepsLongestSurvival(t *testing.T) {
	se := searcherFor(t, sealedConstrictor)
	kept := se.endgameFilter([]board.Dir{board.Down, board.Up})
	if len(kept) != 1 || kept[0] != board.Up || se.endgameSurvival != 21 {
		t.Fatalf("kept %v survival %d, want [up] 21", kept, se.endgameSurvival)
	}
	s := state(t, sealedConstrictor)
	p := params()
	p.SearchEndgame = true
	res, err := Search(context.Background(), s, p, legal.Safe(s, 0))
	if err != nil || res.Move != board.Up {
		t.Fatalf("search with endgame: %v %v", res.Move, err)
	}
}

// Not isolated: the filter must leave the root untouched — and with
// EndgameAlways every move on an open board survives the horizon.
func TestEndgameFilterOnlyWhenIsolated(t *testing.T) {
	se := searcherFor(t, "you 100 5,5 5,4 5,3\nsnake a 100 1,1 1,2 1,3")
	root := []board.Dir{board.Up, board.Left, board.Right}
	if kept := se.endgameFilter(root); len(kept) != 3 {
		t.Fatalf("open board filtered to %v", kept)
	}
	se.p.EndgameAlways = true
	if kept := se.endgameFilter(root); len(kept) != 3 || se.endgameSurvival < se.p.EndgameHorizon {
		t.Fatalf("always mode on an open board: kept %v survival %d", kept, se.endgameSurvival)
	}
}
