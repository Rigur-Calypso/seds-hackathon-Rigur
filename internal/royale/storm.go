// Package royale is the storm geometry (R8). The shrink direction is never in
// the request and is never predicted: every helper reasons over all four
// possible outcomes.
package royale

import "github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"

// SafeRect reconstructs the safe rectangle from the supplied hazard list.
func SafeRect(s *board.State) board.Rect { return board.SafeRect(s) }

// PossibleNext is exactly the four rectangles one shrink can produce.
func PossibleNext(r board.Rect) [4]board.Rect {
	return [4]board.Rect{r.Shrink(0), r.Shrink(1), r.Shrink(2), r.Shrink(3)}
}

// ShrinkRobust: p is safe under ALL four next outcomes.
func ShrinkRobust(p board.Point, r board.Rect) bool { return r.ShrinkAll().Contains(p) }

// TurnsToShrink is the number of moves until a state with a new hazard line
// exists, for a request at `turn`. The engine regenerates with
// numShrinks = newTurn / n, so the ring grows when (turn+k) % n == 0. New
// hazards damage on the move after they appear.
func TurnsToShrink(turn, n int) int {
	if n <= 0 {
		return 1 << 30
	}
	return n - turn%n
}

// HazardBudget is how many consecutive hazard steps (no food) a snake survives:
// each costs damage+1 and health must stay > 0. 100 health, 14 damage → 6.
func HazardBudget(health, damage int) int {
	cost := damage + 1
	if cost <= 0 {
		return 1 << 30
	}
	if health <= 0 {
		return 0
	}
	return (health - 1) / cost
}
