// Package envelope is TVAE, the Temporal Voronoi Action Envelope (CLAUDE.md §6):
// for each of our moves, enumerate every joint action of the other live snakes,
// resolve each exactly with our one-turn resolver, score each outcome with the
// temporal Voronoi evaluator, then aggregate by stage risk posture.
package envelope

import (
	"context"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/eval"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/opponent"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/rules"
)

// Candidate is one of our moves with its aggregated value.
type Candidate struct {
	Dir      board.Dir
	Score    float64
	Mean     float64
	CVaR     float64
	Min      float64
	Outcomes int
}

type outcome struct{ v, w float64 }

// Evaluate scores every move in dirs. On ctx expiry it returns the candidates
// completed so far and ctx's error.
func Evaluate(ctx context.Context, s *board.State, p *config.Params, dirs []board.Dir) ([]Candidate, error) {
	opps := opponent.Choices(s, p)
	Reduce(opps, p)

	moves := make([]board.Dir, len(s.Snakes))
	for i := range s.Snakes {
		if len(s.Snakes[i].Body) > 0 {
			moves[i] = legal.DefaultMove(&s.Snakes[i])
		}
	}
	idx := make([]int, len(opps))
	buf := make([]outcome, 0, 64)
	out := make([]Candidate, 0, len(dirs))
	opt := rules.Options{Shrink: rules.ShrinkKeep}
	if p.EnvelopeShrinkPessimistic {
		opt.Shrink = rules.ShrinkPessimistic
	}
	for _, m := range dirs {
		moves[0] = m
		buf = buf[:0]
		for k := range idx {
			idx[k] = 0
		}
		for {
			w := 1.0
			for k := range opps {
				moves[opps[k].Idx] = opps[k].Dirs[idx[k]]
				w *= opps[k].W[idx[k]]
			}
			next := rules.Resolve(s, moves, rules.Options{Shrink: rules.ShrinkKeep})
			buf = append(buf, outcome{eval.Score(s, next, p), w})
			if err := ctx.Err(); err != nil {
				return out, err
			}
			k := 0
			for ; k < len(opps); k++ {
				idx[k]++
				if idx[k] < len(opps[k].Dirs) {
					break
				}
				idx[k] = 0
			}
			if k == len(opps) {
				break
			}
		}
		out = append(out, Aggregate(m, buf, p))
	}
	return out, nil
}

// Reduce applies locality masking (bookworm / m-schier idea): opponents farther
// than LocalityRadius are forced to their most likely move, then the farthest
// remaining are collapsed until the joint count fits MaxJoint.
func Reduce(opps []opponent.Choice, p *config.Params) {
	for k := range opps {
		if opps[k].Dist > p.LocalityRadius {
			collapse(&opps[k])
		}
	}
	for p.MaxJoint > 0 && joint(opps) > p.MaxJoint {
		far := -1
		for k := range opps {
			if len(opps[k].Dirs) > 1 && (far < 0 || opps[k].Dist > opps[far].Dist) {
				far = k
			}
		}
		if far < 0 {
			return
		}
		collapse(&opps[far])
	}
}

func joint(opps []opponent.Choice) int {
	n := 1
	for _, o := range opps {
		n *= len(o.Dirs)
	}
	return n
}

func collapse(c *opponent.Choice) {
	best := 0
	for i := range c.W {
		if c.W[i] > c.W[best] {
			best = i
		}
	}
	c.Dirs = []board.Dir{c.Dirs[best]}
	c.W = []float64{1}
}
