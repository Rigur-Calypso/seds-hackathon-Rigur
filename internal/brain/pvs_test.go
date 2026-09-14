package brain

import (
	"math"
	"math/rand"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/testgen"
)

// PVS changes how much of the tree is searched, never the minimax value: at
// a fixed depth the best root value must match plain alpha-beta, and it must
// not need more nodes on average.
func TestPVSKeepsTheValue(t *testing.T) {
	var plainNodes, pvsNodes int64
	checked := 0
	for seed := int64(1); seed <= 40; seed++ {
		rng := rand.New(rand.NewSource(seed))
		s := testgen.Position(seed*7717, 11, 11, 2+rng.Intn(3), 10+rng.Intn(50), board.Rules{Name: "standard", HazardDamage: 14})
		if !s.Snakes[0].Alive() || s.AliveCount() < 2 {
			continue
		}
		p := params()
		p.SearchNodes, p.SearchMaxDepth, p.SearchExtensions = 0, 3, 0
		plain := search(t, s, p)
		p.SearchPVS = true
		pvs := search(t, s, p)
		if plain.Depth != pvs.Depth || math.Abs(plain.Scores[0].Value-pvs.Scores[0].Value) > 1e-6 {
			t.Fatalf("seed %d: plain %+v (depth %d) vs pvs %+v (depth %d)", seed, plain.Scores, plain.Depth, pvs.Scores, pvs.Depth)
		}
		plainNodes += plain.Nodes
		pvsNodes += pvs.Nodes
		checked++
	}
	t.Logf("%d positions: plain %d nodes, pvs %d nodes", checked, plainNodes, pvsNodes)
	if checked < 20 {
		t.Fatalf("only %d positions", checked)
	}
	if pvsNodes > plainNodes*11/10 {
		t.Fatalf("PVS searched more: %d vs %d", pvsNodes, plainNodes)
	}
}
