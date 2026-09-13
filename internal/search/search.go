// Package search glues the evaluators into decide.Evaluator.
//
// TVAE always runs first: it costs well under a millisecond and always
// completes, so a valid evaluated move exists before any deep search starts.
// When exactly two snakes live and the profile enables it, duel search then
// uses the remaining time, and its move replaces TVAE's only if it completed at
// least DuelMinDepth (arena, 19×19 head-to-head: depth-3 search loses to TVAE,
// depth 4 beats it). On a slow CPU this degrades to TVAE, never to fallback.
package search

import (
	"context"
	"math"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/decide"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/duel"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/envelope"
)

func round3(v float64) float64 {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return -9
	}
	return math.Round(v*1000) / 1000
}

// Evaluate implements decide.Evaluator.
func Evaluate(ctx context.Context, s *board.State, p *config.Params, safe []board.Dir) (board.Dir, decide.Decision, error) {
	info := decide.Decision{Reason: decide.ReasonEvaluated}
	cands, err := envelope.Evaluate(ctx, s, p, safe)
	for _, c := range cands {
		info.Scores = append(info.Scores, decide.Score{Move: c.Dir.String(), Value: round3(c.Score)})
	}
	if err != nil {
		return safe[0], info, err
	}
	best := 0
	for i := range cands {
		if cands[i].Score > cands[best].Score {
			best = i
		}
	}
	move := cands[best].Dir

	if p.DuelEnabled && s.AliveCount() == 2 {
		dir, depth, scores, derr := duel.Search(ctx, s, p, safe)
		info.Depth = depth
		if derr == nil && depth >= p.DuelMinDepth {
			info.Reason = decide.ReasonDuel
			info.Scores = info.Scores[:0]
			for _, rs := range scores {
				info.Scores = append(info.Scores, decide.Score{Move: rs.Dir.String(), Value: round3(rs.Value)})
			}
			return dir, info, nil
		}
	}
	return move, info, nil
}
