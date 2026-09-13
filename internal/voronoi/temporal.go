// Package voronoi is the temporal Voronoi evaluator: ONE lockstep multi-source
// BFS from every live head that returns everything the scorer needs.
//
//   - Time-aware occupancy (snork): a body segment at index k of a snake of
//     length L frees after L-k turns, so a cell is enterable at BFS time t iff
//     t >= freeAt. A duplicated tail (just ate) naturally frees one turn later
//     (R4). Food eaten along our own path delays our own tail further.
//   - Health carried through the fill: -1 per step, -damage per hazard entry
//     unless the cell holds food, reset to 100 on food (R1, R5, R6). A cell is
//     refused if we would arrive dead.
//   - Lockstep: all heads start at t=0. Equal-arrival cells are Contested, never
//     assigned by index; the unique strictly-longest arriver also scores Attack
//     (R2 — it wins that collision).
//   - Constrictor (R9): bodies never free.
package voronoi

import (
	"math/bits"
	"sync"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
)

// MaxSnakes is the most snakes tracked (bitmask width).
const MaxSnakes = 8

const permanent = int32(1 << 29)

// Result is per-snake board control. Arrays are indexed like State.Snakes.
type Result struct {
	N          int
	FreeCells  int // cells not occupied by any body now
	Guaranteed [MaxSnakes]int
	Contested  [MaxSnakes]int
	Attack     [MaxSnakes]int
	FoodDist   [MaxSnakes]int // -1 if unreachable
	SafeExits  [MaxSnakes]int
	// ExitsUncontested counts next-turn exits no equal-or-longer head can also reach (R2).
	ExitsUncontested [MaxSnakes]int
	Trapped          [MaxSnakes]bool // Guaranteed+Contested < length

	// Focus snake only (cutcells.go).
	Robust   int // reach minus the largest pocket an opponent could seal at one cut cell
	CutCells int
}

// Reach is Guaranteed + Contested.
func (r *Result) Reach(i int) int { return r.Guaranteed[i] + r.Contested[i] }

type node struct {
	c, health, eaten int32
}

type frame struct {
	c int32
	d int8
}

type scratch struct {
	dist, freeAt                []int32
	mask                        []uint8
	bodyOf                      []int8
	food                        []bool
	fr, nx                      [MaxSnakes][]node
	disc, low, sub, sep, parent []int32
	stack                       []frame
}

var pool = sync.Pool{New: func() any { return new(scratch) }}

func fit32(s []int32, n int) []int32 {
	if cap(s) < n {
		return make([]int32, n)
	}
	return s[:n]
}

func (sc *scratch) reset(n int) {
	sc.dist, sc.freeAt = fit32(sc.dist, n), fit32(sc.freeAt, n)
	sc.disc, sc.low, sc.sub, sc.sep, sc.parent = fit32(sc.disc, n), fit32(sc.low, n), fit32(sc.sub, n), fit32(sc.sep, n), fit32(sc.parent, n)
	if cap(sc.mask) < n {
		sc.mask, sc.bodyOf, sc.food = make([]uint8, n), make([]int8, n), make([]bool, n)
	} else {
		sc.mask, sc.bodyOf, sc.food = sc.mask[:n], sc.bodyOf[:n], sc.food[:n]
	}
	for i := 0; i < n; i++ {
		sc.dist[i], sc.freeAt[i], sc.mask[i], sc.bodyOf[i], sc.food[i] = -1, 0, 0, -1, false
	}
}

// Compute runs the fill. focus (usually 0) also gets cut-cell analysis; pass -1
// to skip it.
func Compute(s *board.State, focus int) Result {
	var r Result
	for i := range r.FoodDist {
		r.FoodDist[i] = -1
	}
	n := s.Cells()
	if n <= 0 {
		return r
	}
	sc := pool.Get().(*scratch)
	defer pool.Put(sc)
	sc.reset(n)

	ns := len(s.Snakes)
	if ns > MaxSnakes {
		ns = MaxSnakes
	}
	r.N = ns
	for _, f := range s.Food {
		if s.InBounds(f) {
			sc.food[s.Idx(f)] = true
		}
	}

	var lens [MaxSnakes]int
	bodyCells := 0
	for j := 0; j < ns; j++ {
		sn := &s.Snakes[j]
		if !sn.Alive() || len(sn.Body) == 0 {
			continue
		}
		L := len(sn.Body)
		lens[j] = L
		for k := L - 1; k >= 0; k-- {
			p := sn.Body[k]
			if !s.InBounds(p) {
				continue
			}
			c := s.Idx(p)
			fa := int32(L - k)
			if s.Rules.Constrictor {
				fa = permanent // R9
			}
			if sc.freeAt[c] == 0 {
				bodyCells++
			}
			if fa > sc.freeAt[c] {
				sc.freeAt[c], sc.bodyOf[c] = fa, int8(j)
			}
		}
	}
	r.FreeCells = n - bodyCells

	for j := 0; j < ns; j++ {
		sc.fr[j] = sc.fr[j][:0]
		sn := &s.Snakes[j]
		if !sn.Alive() || len(sn.Body) == 0 || !s.InBounds(sn.Head()) {
			continue
		}
		c := s.Idx(sn.Head())
		sc.dist[c] = 0
		sc.mask[c] |= 1 << uint(j)
		h := sn.Health
		if h > 100 {
			h = 100
		}
		sc.fr[j] = append(sc.fr[j], node{int32(c), int32(h), 0})
	}

	dmg := int32(s.Rules.HazardDamage)
	for t := int32(1); ; t++ {
		progressed := false
		for j := 0; j < ns; j++ {
			sc.nx[j] = sc.nx[j][:0]
			bit := uint8(1) << uint(j)
			for _, nd := range sc.fr[j] {
				p := s.Pt(int(nd.c))
				for _, d := range board.AllDirs {
					q, ok := s.Step(p, d)
					if !ok {
						continue
					}
					c := s.Idx(q)
					dc := sc.dist[c]
					if dc >= 0 && dc < t {
						continue // claimed strictly earlier
					}
					if dc == t && sc.mask[c]&bit != 0 {
						continue // we already arrived here this step
					}
					if fa := sc.freeAt[c]; fa > 0 {
						need := fa
						if int(sc.bodyOf[c]) == j {
							need += nd.eaten // growth delays our own tail
						}
						if t < need {
							continue
						}
					}
					h := nd.health - 1
					food := sc.food[c]
					if !food && s.Hazard != nil && s.Hazard[c] > 0 {
						h -= dmg * int32(s.Hazard[c])
					}
					switch {
					case food || s.Rules.Constrictor:
						h = 100
					case h <= 0:
						continue
					case h > 100:
						h = 100
					}
					e := nd.eaten
					if food {
						e++
					}
					if dc < 0 {
						sc.dist[c] = t
					}
					sc.mask[c] |= bit
					sc.nx[j] = append(sc.nx[j], node{int32(c), h, e})
					progressed = true
				}
			}
		}
		sc.fr, sc.nx = sc.nx, sc.fr
		if !progressed {
			break
		}
	}

	for c := 0; c < n; c++ {
		m := sc.mask[c]
		if m == 0 || sc.dist[c] == 0 {
			continue
		}
		if bits.OnesCount8(m) == 1 {
			r.Guaranteed[bits.TrailingZeros8(m)]++
		} else {
			best, bestLen, unique := -1, -1, false
			for mm := m; mm != 0; mm &= mm - 1 {
				j := bits.TrailingZeros8(mm)
				r.Contested[j]++
				switch {
				case lens[j] > bestLen:
					best, bestLen, unique = j, lens[j], true
				case lens[j] == bestLen:
					unique = false
				}
			}
			if unique {
				r.Attack[best]++
			}
		}
		if sc.food[c] {
			d := int(sc.dist[c])
			for mm := m; mm != 0; mm &= mm - 1 {
				j := bits.TrailingZeros8(mm)
				if r.FoodDist[j] < 0 || d < r.FoodDist[j] {
					r.FoodDist[j] = d
				}
			}
		}
	}

	for j := 0; j < ns; j++ {
		sn := &s.Snakes[j]
		if !sn.Alive() || len(sn.Body) == 0 || !s.InBounds(sn.Head()) {
			continue
		}
		bit := uint8(1) << uint(j)
		for _, d := range board.AllDirs {
			if q, ok := s.Step(sn.Head(), d); ok {
				c := s.Idx(q)
				if sc.dist[c] == 1 && sc.mask[c]&bit != 0 {
					r.SafeExits[j]++
					threatened := false
					for mm := sc.mask[c] &^ bit; mm != 0; mm &= mm - 1 {
						if lens[bits.TrailingZeros8(mm)] >= lens[j] {
							threatened = true // R2: an equal-or-longer head can arrive too
							break
						}
					}
					if !threatened {
						r.ExitsUncontested[j]++
					}
				}
			}
		}
		r.Trapped[j] = r.Reach(j) < lens[j]
	}

	if focus >= 0 && focus < ns && s.Snakes[focus].Alive() && len(s.Snakes[focus].Body) > 0 && s.InBounds(s.Snakes[focus].Head()) {
		r.Robust, r.CutCells = sc.articulation(s, focus, r.Reach(focus))
	} else {
		r.Robust = r.Reach(0)
	}
	return r
}
