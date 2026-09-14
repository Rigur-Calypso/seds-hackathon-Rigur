package main

import (
	"fmt"
	"os"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/decide"
)

// --trace SEED plays only the game with that seed and prints every decision
// of seat 0 to stderr: health, length, move, engine reason, search depth and
// nodes, the candidate scores, the nearest food and each opponent. It is how a
// loss pattern from --dump-losses is followed back to where it started.

func iabs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func traceLine(gs *api.GameState, d decide.Decision) {
	you := gs.You
	food := -1
	for _, f := range gs.Board.Food {
		if m := iabs(f.X-you.Head.X) + iabs(f.Y-you.Head.Y); food < 0 || m < food {
			food = m
		}
	}
	opps := ""
	for _, s := range gs.Board.Snakes {
		if s.ID == you.ID {
			continue
		}
		opps += fmt.Sprintf(" opp[len=%d hp=%d dist=%d]", s.Length, s.Health, iabs(s.Head.X-you.Head.X)+iabs(s.Head.Y-you.Head.Y))
	}
	fmt.Fprintf(os.Stderr, "t=%d hp=%d len=%d move=%s reason=%s depth=%d nodes=%d food=%d%s scores=%v\n",
		gs.Turn, you.Health, you.Length, d.Move, d.Reason, d.Depth, d.Nodes, food, opps, d.Scores)
}
