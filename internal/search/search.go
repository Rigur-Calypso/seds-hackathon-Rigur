// Package search glues the evaluators into decide.Evaluator: duel search when
// exactly two snakes live (and the profile enables it), otherwise TVAE.
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
	var info decide.Decision
	if p.DuelEnabled && s.AliveCount() == 2 {
		dir, depth, scores, err := duel.Search(ctx, s, p, safe)
		if err == nil {
			info.Reason, info.Depth = decide.ReasonDuel, depth
			for _, rs := range scores {
				info.Scores = append(info.Scores, decide.Score{Move: rs.Dir.String(), Value: round3(rs.Value)})
			}
			return dir, info, nil
		}
	}
	cands, err := envelope.Evaluate(ctx, s, p, safe)
	info.Reason = decide.ReasonEvaluated
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
	return cands[best].Dir, info, nil
}
