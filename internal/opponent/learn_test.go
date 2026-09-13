package opponent

import (
	"context"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
)

func state(t *testing.T, turn int, text string) *board.State {
	t.Helper()
	s, ok := board.FromAPI(fixture.MustState(text))
	if !ok {
		t.Fatal("you not found")
	}
	s.Turn = turn
	return s
}

// A snake that keeps taking the food policy's favourite move pulls its
// posterior toward food; the uniform floor survives any amount of evidence.
func TestLearnerConvergesToObservedPolicy(t *testing.T) {
	p := config.Defaults()
	sm := &snakeModel{post: prior(&p)}
	// Two moves: space prefers the second, food the first.
	sm.recDist = [numPolicies][]float64{{0.1, 0.9}, {0.9, 0.1}, {0.5, 0.5}, {0.5, 0.5}}
	for i := 0; i < 20; i++ {
		sm.update(0, &p)
	}
	if sm.post[polFood] < 0.5 || sm.post[polFood] <= sm.post[polSpace] {
		t.Fatalf("posterior did not move toward food: %v", sm.post)
	}
	m := &Model{snakes: map[string]*snakeModel{"x": sm}}
	pr := prior(&p)
	mix, _ := m.mixture("x", &p)
	if mix[polFood] <= pr[polFood] {
		t.Fatalf("mixture not personalised after %d observations: %v", sm.obs, mix)
	}
	if mix[polUniform] < pr[polUniform] {
		t.Fatalf("uniform floor broken: %v < %v", mix[polUniform], pr[polUniform])
	}
}

// One odd move cannot flip the posterior: each update moves at most LearnMaxStep.
func TestLearnerStepIsCapped(t *testing.T) {
	p := config.Defaults()
	sm := &snakeModel{post: prior(&p)}
	sm.recDist = [numPolicies][]float64{{0, 1}, {1, 0}, {0.5, 0.5}, {0.5, 0.5}}
	before := sm.post
	sm.update(1, &p) // only space explains this move
	for i := range sm.post {
		if d := sm.post[i] - before[i]; d > p.LearnMaxStep || d < -p.LearnMaxStep {
			t.Fatalf("policy %d moved %v in one turn", i, d)
		}
	}
}

// Plumbing: Choices records the prediction, the next turn's board reveals the
// move, Observe counts it; every legal move keeps positive weight throughout.
func TestObserveInfersMoveFromBody(t *testing.T) {
	p := config.Defaults()
	p.LearnOpponents = true
	m := NewModels().Get("g", "you")

	s1 := state(t, 5, "you 100 2,5 1,5 0,5\nsnake peer 100 8,5 9,5 10,5\nfood 5,5")
	m.Observe(s1, &p)
	ch := Choices(s1, &p, m)
	if len(ch) != 1 || len(ch[0].Dirs) != 3 {
		t.Fatalf("peer must keep all three legal moves: %+v", ch)
	}
	for _, w := range ch[0].W {
		if w <= 0 {
			t.Fatalf("legal move with zero weight: %v", ch[0].W)
		}
	}

	s2 := state(t, 6, "you 100 3,5 2,5 1,5\nsnake peer 100 7,5 8,5 9,5\nfood 5,5")
	m.Observe(s2, &p)
	sm := m.snakes[s2.Snakes[1].ID]
	if sm == nil || sm.obs != 1 {
		t.Fatalf("move not observed: %+v", sm)
	}

	// A skipped turn (no record for turn 6) must not produce an observation.
	s3 := state(t, 8, "you 100 5,5 4,5 3,5\nsnake peer 100 5,6 6,6 7,6")
	m.Observe(s3, &p)
	if sm.obs != 1 {
		t.Fatalf("observed across a gap: obs=%d", sm.obs)
	}
}

// Nil model reproduces the fixed ensemble exactly (flag-off behaviour).
func TestChoicesNilModelUnchanged(t *testing.T) {
	p := config.Defaults()
	s := state(t, 5, "you 100 2,5 1,5 0,5\nsnake peer 100 8,5 9,5 10,5\nfood 5,5")
	a, b := Choices(s, &p, nil), Choices(s, &p, nil)
	for k := range a[0].W {
		if a[0].W[k] != b[0].W[k] {
			t.Fatal("nil-model choices not deterministic")
		}
	}
	if ModelFrom(context.Background()) != nil {
		t.Fatal("empty context must carry no model")
	}
}

// /end deletes every model of that game and only that game.
func TestModelsEnd(t *testing.T) {
	ms := NewModels()
	ms.Get("g", "a")
	ms.Get("g", "b")
	ms.Get("h", "a")
	ms.End("g")
	if ms.Len() != 1 {
		t.Fatalf("models left: %d", ms.Len())
	}
}
