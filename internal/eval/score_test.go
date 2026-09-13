package eval

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

// P3: food we can reach but cannot win (an equal-length snake arrives at the
// same time, R2: both die) may keep hunger urgent, but must earn no growth
// reward. Food we do win is scored the same with the flag on or off.
func TestUnwinnableFoodEarnsNoGrowthReward(t *testing.T) {
	off := config.Defaults()
	on := off
	on.WinnableFood = true

	tie := st(t, "you 100 2,5 1,5 0,5\nsnake peer 100 8,5 9,5 10,5\nfood 5,5")
	if hOn, hOff := Heuristic(tie, tie, &on), Heuristic(tie, tie, &off); hOn >= hOff {
		t.Fatalf("unwinnable food still rewarded as growth: on=%v off=%v", hOn, hOff)
	}

	longer := st(t, "you 100 2,5 1,5 0,5 0,4\nsnake peer 100 8,5 9,5 10,5\nfood 5,5")
	if hOn, hOff := Heuristic(longer, longer, &on), Heuristic(longer, longer, &off); hOn != hOff {
		t.Fatalf("winnable nearest food scored differently: on=%v off=%v", hOn, hOff)
	}
}

// P3 hunger fallback: low health with only unwinnable food keeps the urgency
// of the reachable food (slack health − 3), not the board-span slack, so the
// flag does not make a starving snake more desperate than without it.
func TestUnwinnableFoodKeepsHungerFallback(t *testing.T) {
	off := config.Defaults()
	off.WFood = 0 // isolate hunger from growth
	on := off
	on.WinnableFood = true
	hungry := st(t, "you 10 2,5 1,5 0,5\nsnake peer 100 8,5 9,5 10,5\nfood 5,5")
	if hOn, hOff := Heuristic(hungry, hungry, &on), Heuristic(hungry, hungry, &off); hOn != hOff {
		t.Fatalf("hunger changed by the flag with only unwinnable food: on=%v off=%v", hOn, hOff)
	}
}
