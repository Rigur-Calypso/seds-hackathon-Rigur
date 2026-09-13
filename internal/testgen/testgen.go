// Package testgen generates realistic random positions for property and
// metamorphic tests by playing random safe moves with the exact resolver.
package testgen

import (
	"fmt"
	"math/rand"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/rules"
)

func freeCell(rng *rand.Rand, s *board.State) (board.Point, bool) {
	occ := make([]bool, s.Cells())
	for i := range s.Snakes {
		for _, p := range s.Snakes[i].Body {
			if s.InBounds(p) {
				occ[s.Idx(p)] = true
			}
		}
	}
	for _, f := range s.Food {
		occ[s.Idx(f)] = true
	}
	for tries := 0; tries < 200; tries++ {
		p := board.Point{X: rng.Intn(s.W), Y: rng.Intn(s.H)}
		if !occ[s.Idx(p)] {
			return p, true
		}
	}
	return board.Point{}, false
}

// Start places n stacked length-3 snakes and some food.
func Start(rng *rand.Rand, w, h, n int, rs board.Rules) *board.State {
	s := &board.State{W: w, H: h, Rules: rs}
	for k := 0; k < n; k++ {
		p, _ := freeCell(rng, s)
		s.Snakes = append(s.Snakes, board.Snake{ID: fmt.Sprintf("s%d", k), Name: fmt.Sprintf("s%d", k), Body: []board.Point{p, p, p}, Health: 100})
	}
	for k := 0; k < n; k++ {
		if p, ok := freeCell(rng, s); ok {
			s.Food = append(s.Food, p)
		}
	}
	return s
}

// Play advances s by random safe moves while at least two snakes live.
func Play(rng *rand.Rand, s *board.State, turns int) *board.State {
	for t := 0; t < turns && s.AliveCount() >= 2; t++ {
		moves := make([]board.Dir, len(s.Snakes))
		blocked := legal.Blocked(s)
		for i := range s.Snakes {
			if !s.Snakes[i].Alive() {
				continue
			}
			if safe := legal.SafeWith(s, blocked, i); len(safe) > 0 {
				moves[i] = safe[rng.Intn(len(safe))]
			} else {
				moves[i] = board.Dir(rng.Intn(4))
			}
		}
		s = rules.Resolve(s, moves, rules.Options{})
		if len(s.Food) < 3 && rng.Intn(3) == 0 {
			if p, ok := freeCell(rng, s); ok {
				s.Food = append(s.Food, p)
			}
		}
	}
	return s
}

// Position is a seeded random mid-game position.
func Position(seed int64, w, h, n, turns int, rs board.Rules) *board.State {
	rng := rand.New(rand.NewSource(seed))
	return Play(rng, Start(rng, w, h, n, rs), turns)
}
