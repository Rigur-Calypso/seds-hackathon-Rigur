package main

import (
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/decide"
)

// storeProbe plays like another policy and records the learner store's peak size.
type storeProbe struct {
	eng  *decide.Engine
	peak *int
	next Policy
}

func (p storeProbe) Move(gs *api.GameState) string {
	if n := p.eng.Models.Len(); n > *p.peak {
		*p.peak = n
	}
	return p.next.Move(gs)
}

// P2: the arena never sends /end, so playGame must drop the engine's per-game
// learner models itself; otherwise every simulated game leaks one.
func TestPlayGameClearsLearners(t *testing.T) {
	cfg := defaultConfig()
	cfg.MaxTurns = 30
	eng, err := loadEngine(&cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	name := stageProfile(&cfg)
	p := *eng.Profiles.Get(name)
	p.LearnOpponents = true
	eng.Profiles.Set(name, &p)

	peak := 0
	seats := []Seat{
		{"us", enginePolicy{eng, 0}},
		{"probe", storeProbe{eng, &peak, zoo["spacegreedy"]}},
		{"foodgreedy", zoo["foodgreedy"]},
		{"random", zoo["random"]},
	}
	for seed := int64(1); seed <= 3; seed++ {
		if _, err := playGame(&cfg, seed, seats); err != nil {
			t.Fatal(err)
		}
		if n := eng.Models.Len(); n != 0 {
			t.Fatalf("seed %d: %d learner models left after the game", seed, n)
		}
	}
	if peak == 0 {
		t.Fatal("learner never created a model: the test is vacuous")
	}
}
