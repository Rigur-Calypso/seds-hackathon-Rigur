package voronoi

import "github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"

// articulation runs an iterative Tarjan DFS over the focus snake's reachable
// region (rooted at its head) and finds cut cells: cells whose removal
// disconnects part of the region from the head. Space behind a single narrow
// entrance is easily sealed, so when an opponent can reach (or stand next to)
// such a cell, that pocket is discounted.
//
// Returns robust = reach - (largest sealable pocket + the cut cell itself), and
// the number of cut cells.
func (sc *scratch) articulation(s *board.State, focus, reach int) (robust, cuts int) {
	n := s.Cells()
	bit := uint8(1) << uint(focus)
	root := int32(s.Idx(s.Snakes[focus].Head()))
	in := func(c int32) bool {
		return c == root || (sc.mask[c]&bit != 0 && sc.dist[c] > 0)
	}
	for i := 0; i < n; i++ {
		sc.disc[i], sc.sep[i] = 0, 0
	}
	timer := int32(1)
	sc.disc[root], sc.low[root], sc.sub[root], sc.parent[root] = 1, 1, 1, -1
	stack := append(sc.stack[:0], frame{root, 0})
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.d < 4 {
			d := board.Dir(top.d)
			top.d++
			q, ok := s.Step(s.Pt(int(top.c)), d)
			if !ok {
				continue
			}
			nb := int32(s.Idx(q))
			if !in(nb) {
				continue
			}
			if sc.disc[nb] == 0 {
				timer++
				sc.disc[nb], sc.low[nb], sc.sub[nb], sc.parent[nb] = timer, timer, 1, top.c
				stack = append(stack, frame{nb, 0})
			} else if nb != sc.parent[top.c] && sc.disc[nb] < sc.low[top.c] {
				sc.low[top.c] = sc.disc[nb]
			}
			continue
		}
		c := top.c
		stack = stack[:len(stack)-1]
		par := sc.parent[c]
		if par < 0 {
			continue
		}
		if sc.low[c] < sc.low[par] {
			sc.low[par] = sc.low[c]
		}
		sc.sub[par] += sc.sub[c]
		if par != root && sc.low[c] >= sc.disc[par] {
			sc.sep[par] += sc.sub[c]
		}
	}
	sc.stack = stack

	worst := 0
	for c := 0; c < n; c++ {
		if sc.sep[c] == 0 {
			continue
		}
		cuts++
		if pocket := int(sc.sep[c]) + 1; pocket > worst && sc.sealable(s, c, bit) {
			worst = pocket
		}
	}
	robust = reach - worst
	if robust < 0 {
		robust = 0
	}
	return robust, cuts
}

// sealable: some other snake arrives at the cell or a neighbour.
func (sc *scratch) sealable(s *board.State, c int, bit uint8) bool {
	if sc.mask[c]&^bit != 0 {
		return true
	}
	p := s.Pt(c)
	for _, d := range board.AllDirs {
		if q, ok := s.Step(p, d); ok && sc.mask[s.Idx(q)]&^bit != 0 {
			return true
		}
	}
	return false
}
