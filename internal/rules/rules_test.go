package rules

import (
	"reflect"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
)

const (
	U = board.Up
	D = board.Down
	L = board.Left
	R = board.Right
)

func state(t *testing.T, text string) *board.State {
	t.Helper()
	s, ok := board.FromAPI(fixture.MustState(text))
	if !ok {
		t.Fatalf("you not found in fixture")
	}
	return s
}

func step(s *board.State, moves ...board.Dir) *board.State { return Resolve(s, moves, Options{}) }

// R1: reach condition is health >= distance.
func TestR1_ReachConditionIsHealthGEDistance(t *testing.T) {
	const far = "\nsnake far 100 0,2 0,1 0,0"
	s := state(t, "you 3 5,5 5,4 5,3\nfood 5,8"+far)
	for i := 0; i < 3; i++ {
		s = step(s, U, R)
	}
	if me := s.Snakes[0]; !me.Alive() || me.Health != 100 || me.Len() != 4 {
		t.Fatalf("health 3, food at 3: must eat and live; alive=%v health=%d len=%d", me.Alive(), me.Health, me.Len())
	}
	s = state(t, "you 3 5,5 5,4 5,3\nfood 5,9"+far)
	for i := 0; i < 3; i++ {
		s = step(s, U, R)
	}
	if s.Snakes[0].Cause != board.ByOutOfHealth {
		t.Fatalf("health 3, food at 4: must starve, got %q", s.Snakes[0].Cause)
	}
}

// R2: equal-length head-to-head eliminates both; strictly longer wins.
func TestR2_EqualHeadToHeadEliminatesBoth(t *testing.T) {
	s := step(state(t, "you 90 5,5 5,4 5,3\nsnake opp 90 5,7 5,8 5,9"), U, D)
	if s.Snakes[0].Cause != board.ByHeadToHead || s.Snakes[1].Cause != board.ByHeadToHead {
		t.Fatalf("equal lengths must both die head-to-head, got %q %q", s.Snakes[0].Cause, s.Snakes[1].Cause)
	}
	s = step(state(t, "you 90 5,5 5,4 5,3\nsnake opp 90 5,7 5,8 5,9 5,10"), U, D)
	if s.Snakes[0].Alive() || !s.Snakes[1].Alive() {
		t.Fatal("only the shorter snake dies")
	}
}

// R3: moving onto a shorter snake's head is a head-to-head we win, not a body hit.
func TestR3_HeadsAreNotBodies(t *testing.T) {
	s := step(state(t, "you 90 5,5 5,4 5,3 5,2\nsnake opp 90 5,7 5,8 5,9"), U, D)
	if !s.Snakes[0].Alive() || s.Snakes[1].Cause != board.ByHeadToHead {
		t.Fatalf("longer snake must live; you=%q opp=%q", s.Snakes[0].Cause, s.Snakes[1].Cause)
	}
}

// R4: tails vacate unless duplicated — ours and opponents'.
func TestR4_TailVacatesUnlessDuplicated(t *testing.T) {
	const far = "\nsnake far 100 0,10 1,10 2,10"
	if s := step(state(t, "you 90 5,5 5,4 4,4 4,5"+far), L, D); !s.Snakes[0].Alive() {
		t.Fatalf("own tail must be free, got %q", s.Snakes[0].Cause)
	}
	if s := step(state(t, "you 90 5,5 5,4 4,4 4,5 4,5"+far), L, D); s.Snakes[0].Cause != board.BySelfCollision {
		t.Fatalf("own duplicated tail must kill, got %q", s.Snakes[0].Cause)
	}
	if s := step(state(t, "you 90 5,5 6,5 7,5\nsnake opp 90 3,6 3,5 4,5"), L, U); !s.Snakes[0].Alive() {
		t.Fatalf("opponent tail must be free, got %q", s.Snakes[0].Cause)
	}
	if s := step(state(t, "you 90 5,5 6,5 7,5\nsnake opp 90 3,6 3,5 4,5 4,5"), L, U); s.Snakes[0].Cause != board.ByCollision {
		t.Fatalf("opponent duplicated tail must kill, got %q", s.Snakes[0].Cause)
	}
}

// R5: food on a hazard square cancels that square's damage and restores health.
func TestR5_FoodOnHazardCancelsDamage(t *testing.T) {
	s := step(state(t, "you 10 5,5 5,4 5,3\nsnake far 100 0,10 1,10 2,10\nhazard 5,6\nfood 5,6\ndamage 14"), U, D)
	if me := s.Snakes[0]; !me.Alive() || me.Health != 100 || me.Len() != 4 {
		t.Fatalf("food in hazard must be safe; alive=%v health=%d", me.Alive(), me.Health)
	}
}

// R6: hazard without food applies before feeding and kills inline — even if
// food is one step away elsewhere.
func TestR6_HazardWithoutFoodKills(t *testing.T) {
	s := step(state(t, "you 10 5,5 5,4 5,3\nsnake far 100 0,10 1,10 2,10\nhazard 5,6\nfood 4,5\ndamage 14"), U, D)
	if s.Snakes[0].Cause != board.ByHazard {
		t.Fatalf("want hazard death, got %q", s.Snakes[0].Cause)
	}
	s = step(state(t, "you 16 5,5 5,4 5,3\nsnake far 100 0,10 1,10 2,10\nhazard 5,6\ndamage 14"), U, D)
	if me := s.Snakes[0]; !me.Alive() || me.Health != 1 {
		t.Fatalf("16-1-14 = 1 must survive; alive=%v health=%d", me.Alive(), me.Health)
	}
}

// Engine detail: damage applies once per hazard entry (stacked hazards).
func TestHazardStacksApplyPerEntry(t *testing.T) {
	s := step(state(t, "you 30 5,5 5,4 5,3\nsnake far 100 0,10 1,10 2,10\nhazard 5,6 5,6\ndamage 14"), U, D)
	if me := s.Snakes[0]; !me.Alive() || me.Health != 1 {
		t.Fatalf("30-1-28 = 1; alive=%v health=%d", me.Alive(), me.Health)
	}
	s = step(state(t, "you 29 5,5 5,4 5,3\nsnake far 100 0,10 1,10 2,10\nhazard 5,6 5,6\ndamage 14"), U, D)
	if s.Snakes[0].Alive() {
		t.Fatal("29-1-28 = 0 must die")
	}
}

// R7: three-way collision — only the unique longest survives; all equal all die.
func TestR7_MultiCollision(t *testing.T) {
	const others = "\nsnake b 90 4,5 3,5 2,5 1,5\nsnake c 90 6,5 7,5 8,5 9,5"
	s := step(state(t, "you 90 5,4 5,3 5,2 5,1 5,0"+others), U, R, L)
	if !s.Snakes[0].Alive() || s.Snakes[1].Alive() || s.Snakes[2].Alive() {
		t.Fatalf("longest must survive alone: %q %q %q", s.Snakes[0].Cause, s.Snakes[1].Cause, s.Snakes[2].Cause)
	}
	s = step(state(t, "you 90 5,4 5,3 5,2 5,1"+others), U, R, L)
	if s.AliveCount() != 0 {
		t.Fatalf("equal three-way must kill all, alive=%d", s.AliveCount())
	}
}

// R8: royale ring geometry across shrink turns.
func TestR8_RoyaleRing(t *testing.T) {
	const snakes = "you 100 5,5 5,4 5,3\nsnake o 100 1,1 1,2 1,3\n"
	if n := step(state(t, "rules royale\nturn 3\nshrink 25\n"+snakes), U, D); n.Hazard != nil {
		t.Fatal("no hazards before the first shrink")
	}
	s := state(t, "rules royale\nturn 24\nshrink 25\n"+snakes)
	n := Resolve(s, []board.Dir{U, D}, Options{Shrink: ShrinkExact, Side: 0})
	if r := board.SafeRect(n); r != (board.Rect{MinX: 1, MinY: 0, MaxX: 10, MaxY: 10}) {
		t.Fatalf("turn 25 shrink side 0: got %+v", r)
	}
	n2 := Resolve(n, []board.Dir{U, R}, Options{Shrink: ShrinkPessimistic})
	if board.SafeRect(n2) != board.SafeRect(n) {
		t.Fatal("ring must only change on shrink turns")
	}
	s = state(t, "rules royale\nturn 49\nshrink 25\nsafe 1 0 10 10\n"+snakes)
	n = Resolve(s, []board.Dir{U, D}, Options{Shrink: ShrinkPessimistic})
	if r := board.SafeRect(n); r != (board.Rect{MinX: 2, MinY: 1, MaxX: 9, MaxY: 9}) {
		t.Fatalf("pessimistic shrink must union all four outcomes: got %+v", r)
	}
}

// R9: constrictor — food cleared, health pinned, every snake grows, no tail vacates.
func TestR9_Constrictor(t *testing.T) {
	n := step(state(t, "rules constrictor\nyou 100 5,5 5,4 5,3\nsnake o 100 1,1 1,2 1,3\nfood 9,9"), U, D)
	if len(n.Food) != 0 {
		t.Fatal("constrictor clears food")
	}
	for i := range n.Snakes {
		sn := &n.Snakes[i]
		if sn.Len() != 4 || sn.Health != 100 || sn.TailVacates() {
			t.Fatalf("snake %d: len=%d health=%d vacates=%v", i, sn.Len(), sn.Health, sn.TailVacates())
		}
	}
}

// R12 (engine source): snakes eliminated earlier in the same tick do not block.
func TestR12_SameTickEliminationsDoNotBlock(t *testing.T) {
	s := step(state(t, "you 90 5,5 5,4 5,3\nsnake starving 1 4,6 5,6 6,6"), U, U)
	if !s.Snakes[0].Alive() || s.Snakes[1].Cause != board.ByOutOfHealth {
		t.Fatalf("starved snake must not block: you=%q opp=%q", s.Snakes[0].Cause, s.Snakes[1].Cause)
	}
	s = step(state(t, "you 90 5,5 5,4 5,3\nsnake fed 50 4,6 5,6 6,6"), U, U)
	if s.Snakes[0].Cause != board.ByCollision {
		t.Fatalf("control: healthy body must block, got %q", s.Snakes[0].Cause)
	}
}

func TestResolveDoesNotMutateInput(t *testing.T) {
	s := state(t, "you 90 5,5 5,4 5,3\nsnake o 90 1,1 1,2 1,3\nfood 5,6 9,9")
	before := s.Clone()
	_ = step(s, U, D)
	if !reflect.DeepEqual(before, s) {
		t.Fatal("Resolve mutated its input")
	}
}
