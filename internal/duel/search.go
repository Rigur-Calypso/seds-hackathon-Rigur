// Package duel is the 1v1 search used when exactly two snakes are alive
// (branching ~9, so real depth is reachable). Each turn is modelled as our move
// followed by the opponent's reply with knowledge of it — a paranoid maximin,
// which is exactly the grand-final posture — then resolved simultaneously with
// the exact resolver. Iterative deepening with alpha-beta, previous-iteration
// (PV) move ordering at the root, and a cooperative ctx deadline checked inside
// the loop. Never a detached goroutine.
package duel

import (
	"context"
	"errors"
	"math"
	"sort"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/eval"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/rules"
)

// ErrIncomplete means not even depth 1 finished before the deadline.
var ErrIncomplete = errors.New("duel: no completed depth")

// RootScore is a root move and its value at the deepest completed depth.
type RootScore struct {
	Dir   board.Dir
	Value float64
}

type searcher struct {
	ctx   context.Context
	p     *config.Params
	opp   int
	nodes int
	stop  bool
}

func (se *searcher) expired() bool {
	if se.stop {
		return true
	}
	se.nodes++
	if se.nodes&15 == 0 && se.ctx.Err() != nil {
		se.stop = true
	}
	return se.stop
}

func dirsFor(s *board.State, i int) []board.Dir {
	if d := legal.Safe(s, i); len(d) > 0 {
		return d
	}
	return []board.Dir{legal.DefaultMove(&s.Snakes[i])}
}

// Search returns the best root move among root, the depth completed, and the
// root scores in best-first order.
func Search(ctx context.Context, s *board.State, p *config.Params, root []board.Dir) (board.Dir, int, []RootScore, error) {
	opp := -1
	for i := 1; i < len(s.Snakes); i++ {
		if s.Snakes[i].Alive() {
			opp = i
			break
		}
	}
	if opp < 0 || len(root) == 0 {
		return board.Up, 0, nil, ErrIncomplete
	}
	se := &searcher{ctx: ctx, p: p, opp: opp}
	order := append([]board.Dir(nil), root...)
	var best []RootScore
	done := 0
	maxDepth := p.DuelMaxDepth
	if maxDepth < 1 {
		maxDepth = 1
	}
	for depth := 1; depth <= maxDepth; depth++ {
		scores := make([]RootScore, 0, len(order))
		alpha := math.Inf(-1)
		for _, m := range order {
			v := se.minNode(s, m, depth, alpha, math.Inf(1), 0)
			if se.stop {
				break
			}
			scores = append(scores, RootScore{m, v})
			if v > alpha {
				alpha = v
			}
		}
		if se.stop {
			break
		}
		sort.SliceStable(scores, func(a, b int) bool { return scores[a].Value > scores[b].Value })
		best, done = scores, depth
		for i := range scores {
			order[i] = scores[i].Dir // PV ordering for the next iteration
		}
		if eval.IsWin(scores[0].Value) || eval.IsLoss(scores[0].Value) {
			break // proven either way; deeper search cannot change the sign
		}
	}
	if done == 0 {
		return root[0], 0, nil, ErrIncomplete
	}
	return best[0].Dir, done, best, nil
}

func (se *searcher) minNode(s *board.State, m board.Dir, depth int, alpha, beta float64, ply int) float64 {
	moves := make([]board.Dir, len(s.Snakes))
	for i := range s.Snakes {
		if len(s.Snakes[i].Body) > 0 {
			moves[i] = legal.DefaultMove(&s.Snakes[i])
		}
	}
	moves[0] = m
	worst := math.Inf(1)
	for _, o := range dirsFor(s, se.opp) {
		if se.expired() {
			return 0
		}
		moves[se.opp] = o
		next := rules.Resolve(s, moves, rules.Options{Shrink: rules.ShrinkPessimistic})
		var v float64
		if depth <= 1 || !next.Snakes[0].Alive() || !next.Snakes[se.opp].Alive() {
			v = se.leaf(s, next, ply+1)
		} else {
			v = se.maxNode(next, depth-1, alpha, math.Min(beta, worst), ply+1)
			if se.stop {
				return 0
			}
		}
		if v < worst {
			worst = v
		}
		if worst <= alpha {
			break
		}
	}
	return worst
}

func (se *searcher) maxNode(s *board.State, depth int, alpha, beta float64, ply int) float64 {
	best := math.Inf(-1)
	for _, m := range dirsFor(s, 0) {
		v := se.minNode(s, m, depth, math.Max(alpha, best), beta, ply)
		if se.stop {
			return 0
		}
		if v > best {
			best = v
		}
		if best >= beta {
			break
		}
	}
	return best
}

// leaf: terminal ordering prefers winning sooner and losing later.
func (se *searcher) leaf(prev, next *board.State, ply int) float64 {
	v := eval.Score(prev, next, se.p)
	switch {
	case eval.IsLoss(v):
		v += se.p.PlyStep * float64(ply)
	case eval.IsWin(v):
		v -= se.p.PlyStep * float64(ply)
	}
	return v
}
