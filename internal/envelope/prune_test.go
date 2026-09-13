package envelope

import (
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/opponent"
)

// P2 threat-preserving pruning: an opponent collapsed to fit MaxJoint keeps
// every move landing near our head; with the flag off only the likeliest stays.
func TestReduceKeepsNearThreats(t *testing.T) {
	mk := func() []opponent.Choice {
		return []opponent.Choice{{
			Idx:  1,
			Dirs: []board.Dir{board.Up, board.Down, board.Left},
			W:    []float64{0.6, 0.3, 0.1},
			Dist: 3,
			Near: []bool{false, false, true},
		}}
	}
	p := config.Defaults()
	p.MaxJoint = 1

	off := mk()
	Reduce(off, &p)
	if len(off[0].Dirs) != 1 || off[0].Dirs[0] != board.Up {
		t.Fatalf("flag off must collapse to the likeliest move: %v", off[0].Dirs)
	}

	p.ThreatPreservingPrune = true
	on := mk()
	Reduce(on, &p) // must terminate even though the joint count stays above MaxJoint
	if len(on[0].Dirs) != 2 || on[0].Dirs[0] != board.Up || on[0].Dirs[1] != board.Left {
		t.Fatalf("near threat dropped: %v", on[0].Dirs)
	}
	if s := on[0].W[0] + on[0].W[1]; s < 0.999 || s > 1.001 || on[0].W[1] <= 0 {
		t.Fatalf("weights not renormalised: %v", on[0].W)
	}
}
