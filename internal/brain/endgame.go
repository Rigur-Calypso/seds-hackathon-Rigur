package brain

import (
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/sim"
)

// maxSurvival bounds the survival search depth (its own undo stack).
const maxSurvival = 64

// endgameFilter handles a sealed region (P4). When no opponent can ever reach
// our space, the game for us is a single-player problem — survive longest —
// and a heuristic area count is the wrong tool: space can be wasted by
// parity, dead-end pockets and our own growth. For each root move it runs a
// depth-first survival search (fewest onward exits first, Warnsdorff's rule)
// up to EndgameHorizon turns and keeps only the moves that survive longest.
// If any search ran out of budget the answer is unreliable and every move is
// kept, so the filter never removes a move it has not proven worse.
func (se *Searcher) endgameFilter(root []board.Dir) []board.Dir {
	p := se.p
	if len(root) < 2 || p.EndgameHorizon <= 0 || (!p.EndgameAlways && !se.fill.Isolated(se.st)) {
		return root
	}
	limit := p.EndgameHorizon
	if limit > maxSurvival-1 {
		limit = maxSurvival - 1
	}
	surv := make([]int, len(root))
	best := 0
	for i, m := range root {
		budget := p.EndgameNodes / len(root)
		s, complete := se.surviveAfter(m, 0, limit, &budget)
		if !complete {
			return root
		}
		surv[i] = s
		if s > best {
			best = s
		}
	}
	se.endgameSurvival = best
	kept := root[:0:0]
	for i, m := range root {
		if surv[i] >= best {
			kept = append(kept, m)
		}
	}
	return kept
}

// surviveAfter plays our move m (opponents predicted) and returns how many
// turns we stay alive, capped at limit. complete is false when the node budget
// ran out before the answer was known.
func (se *Searcher) surviveAfter(m board.Dir, ply, limit int, budget *int) (int, bool) {
	if *budget <= 0 {
		return 0, false
	}
	*budget--
	// Cooperative deadline (CLAUDE.md §3): an unfinished proof is discarded.
	if *budget&63 == 0 && se.ctx.Err() != nil {
		*budget = 0
		return 0, false
	}
	st := se.st
	var mv sim.Moves
	mv[0] = m
	for j := 1; j < st.N; j++ {
		if st.S[j].Alive {
			mv[j] = se.predict(j)
		}
	}
	u := &se.endUndo[ply]
	st.Make(&mv, u)
	defer st.Unmake(u)
	if !st.S[0].Alive {
		return 0, true
	}
	if limit <= 1 || ply+1 >= maxSurvival {
		return 1, true
	}
	// Further turns are capped by the horizon and, in constrictor (R9), by the
	// cells still reachable: every move consumes a new one.
	want := limit - 1
	if st.G.Constrictor {
		if r := se.fill.ReachCount(st, 0); r < want {
			want = r
		}
	}
	if want == 0 {
		return 1, true
	}
	var moves [4]board.Dir
	n := st.SafeMoves(0, &moves)
	g := st.G
	head := st.S[0].Head()
	onward := func(d board.Dir) int {
		c := g.Nbr[head][d]
		k := 0
		for dd := 0; dd < 4; dd++ {
			if q := g.Nbr[c][dd]; q >= 0 && q != head && !st.BlockedNext(q) {
				k++
			}
		}
		return k
	}
	for a := 0; a < n; a++ {
		b := a
		for c := a + 1; c < n; c++ {
			if onward(moves[c]) < onward(moves[b]) {
				b = c
			}
		}
		moves[a], moves[b] = moves[b], moves[a]
	}
	best := 0
	for k := 0; k < n; k++ {
		s, ok := se.surviveAfter(moves[k], ply+1, limit-1, budget)
		if !ok {
			return 1 + best, false
		}
		if s > best {
			best = s
		}
		if best >= want {
			break
		}
	}
	return 1 + best, true
}
