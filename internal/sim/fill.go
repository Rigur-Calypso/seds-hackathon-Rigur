package sim

import "math/bits"

// Result is per-snake board control from one temporal fill. Fields mean
// exactly what internal/voronoi.Result means; the tests check both agree.
type Result struct {
	N                int
	FreeCells        int
	Guaranteed       [MaxSnakes]int
	Contested        [MaxSnakes]int
	Attack           [MaxSnakes]int
	FoodDist         [MaxSnakes]int // -1 if unreachable
	// FoodHealth is the most health the snake can still have when it first
	// reaches a food (before eating, no food eaten on the way), -1 if none.
	// Without hazards it is health - FoodDist; with them it charges the storm
	// crossed on the way (R6), which a distance cannot.
	FoodHealth       [MaxSnakes]int
	SafeExits        [MaxSnakes]int
	ExitsUncontested [MaxSnakes]int
	Trapped          [MaxSnakes]bool
	Robust           int // focus only: reach minus the largest sealable pocket
	CutCells         int
}

// Reach is Guaranteed + Contested.
func (r *Result) Reach(i int) int { return r.Guaranteed[i] + r.Contested[i] }

const permanent = int32(1 << 29)

type node struct{ c, health, eaten int32 }

type frame struct {
	c int32
	d int8
}

// Fill is reusable scratch for Compute. Not safe for concurrent use: give
// each search its own.
type Fill struct {
	dist, freeAt                []int32
	mask                        []uint8
	bodyOf                      []int8
	fr, nx                      [MaxSnakes][]node
	disc, low, sub, sep, parent []int32
	stack                       []frame
}

func fit32(s []int32, n int) []int32 {
	if cap(s) < n {
		return make([]int32, n)
	}
	return s[:n]
}

func (f *Fill) reset(n int) {
	f.dist, f.freeAt = fit32(f.dist, n), fit32(f.freeAt, n)
	if cap(f.mask) < n {
		f.mask, f.bodyOf = make([]uint8, n), make([]int8, n)
	} else {
		f.mask, f.bodyOf = f.mask[:n], f.bodyOf[:n]
	}
	for i := 0; i < n; i++ {
		f.dist[i], f.freeAt[i], f.mask[i], f.bodyOf[i] = -1, 0, 0, -1
	}
}

// Compute is the temporal Voronoi fill (snork's time-aware occupancy): one
// lockstep BFS from every live head.
//
//   - A segment at index k of a length-L snake frees after L-k turns; a
//     duplicated tail frees one turn later (R4). Food eaten on our own path
//     delays our own tail.
//   - Health is carried: -1 per step, hazard damage unless the cell has food,
//     reset on food (R1, R5, R6). A cell is refused if we would arrive dead.
//   - Equal-arrival cells are Contested, never assigned by index; a unique
//     strictly-longest arriver also scores Attack (R2).
//   - Constrictor: bodies never free (R9).
//
// With cuts, the focus snake also gets articulation-point analysis (Robust).
func (f *Fill) Compute(st *State, focus int, cuts bool) Result {
	var r Result
	for i := range r.FoodDist {
		r.FoodDist[i], r.FoodHealth[i] = -1, -1
	}
	g := st.G
	n := g.Cells
	f.reset(n)
	ns := st.N
	r.N = ns

	var lens [MaxSnakes]int32
	bodyCells := 0
	for j := 0; j < ns; j++ {
		sn := &st.S[j]
		if !sn.Alive {
			continue
		}
		L := sn.Len
		lens[j] = L
		for k := L - 1; k >= 0; k-- {
			c := sn.Seg(k)
			fa := L - k
			if g.Constrictor {
				fa = permanent
			}
			if f.freeAt[c] == 0 {
				bodyCells++
			}
			if fa > f.freeAt[c] {
				f.freeAt[c], f.bodyOf[c] = fa, int8(j)
			}
		}
	}
	r.FreeCells = n - bodyCells

	for j := 0; j < ns; j++ {
		f.fr[j] = f.fr[j][:0]
		sn := &st.S[j]
		if !sn.Alive {
			continue
		}
		c := sn.Head()
		f.dist[c] = 0
		f.mask[c] |= 1 << uint(j)
		h := sn.Health
		if h > MaxHealth {
			h = MaxHealth
		}
		f.fr[j] = append(f.fr[j], node{int32(c), h, 0})
	}

	hz := g.Hazards(st.Turn)
	dmg := g.Damage
	for t := int32(1); ; t++ {
		progressed := false
		for j := 0; j < ns; j++ {
			f.nx[j] = f.nx[j][:0]
			bit := uint8(1) << uint(j)
			for _, nd := range f.fr[j] {
				nb := &g.Nbr[nd.c]
				for d := 0; d < 4; d++ {
					c := int32(nb[d])
					if c < 0 {
						continue
					}
					dc := f.dist[c]
					if dc >= 0 && dc < t {
						continue // claimed strictly earlier
					}
					food := st.food[c]
					if dc == t && f.mask[c]&bit != 0 {
						// Already arrived this step; a healthier equal-length path
						// to a first food still improves FoodHealth.
						if food && nd.eaten == 0 && int(nd.health-1) > r.FoodHealth[j] {
							r.FoodHealth[j] = int(nd.health - 1)
						}
						continue
					}
					if fa := f.freeAt[c]; fa > 0 {
						need := fa
						if int(f.bodyOf[c]) == j {
							need += nd.eaten // growth delays our own tail
						}
						if t < need {
							continue
						}
					}
					h := nd.health - 1
					if food && nd.eaten == 0 && int(h) > r.FoodHealth[j] {
						r.FoodHealth[j] = int(h) // R1/R5: food cancels damage and feeds at 0 health
					}
					if !food && hz != nil && hz[c] > 0 {
						h -= dmg * int32(hz[c])
					}
					switch {
					case food || g.Constrictor:
						h = MaxHealth
					case h <= 0:
						continue
					case h > MaxHealth:
						h = MaxHealth
					}
					e := nd.eaten
					if food {
						e++
					}
					if dc < 0 {
						f.dist[c] = t
					}
					f.mask[c] |= bit
					f.nx[j] = append(f.nx[j], node{c, h, e})
					progressed = true
				}
			}
		}
		f.fr, f.nx = f.nx, f.fr
		if !progressed {
			break
		}
	}

	for c := 0; c < n; c++ {
		m := f.mask[c]
		if m == 0 || f.dist[c] == 0 {
			continue
		}
		if bits.OnesCount8(m) == 1 {
			r.Guaranteed[bits.TrailingZeros8(m)]++
		} else {
			best, bestLen, unique := -1, int32(-1), false
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
		if st.food[c] {
			d := int(f.dist[c])
			for mm := m; mm != 0; mm &= mm - 1 {
				j := bits.TrailingZeros8(mm)
				if r.FoodDist[j] < 0 || d < r.FoodDist[j] {
					r.FoodDist[j] = d
				}
			}
		}
	}

	for j := 0; j < ns; j++ {
		sn := &st.S[j]
		if !sn.Alive {
			continue
		}
		bit := uint8(1) << uint(j)
		nb := &g.Nbr[sn.Head()]
		for d := 0; d < 4; d++ {
			c := nb[d]
			if c < 0 || f.dist[c] != 1 || f.mask[c]&bit == 0 {
				continue
			}
			r.SafeExits[j]++
			threatened := false
			for mm := f.mask[c] &^ bit; mm != 0; mm &= mm - 1 {
				if lens[bits.TrailingZeros8(mm)] >= lens[j] {
					threatened = true // R2: an equal-or-longer head can arrive too
					break
				}
			}
			if !threatened {
				r.ExitsUncontested[j]++
			}
		}
		r.Trapped[j] = r.Reach(j) < int(lens[j])
	}

	if cuts && focus >= 0 && focus < ns && st.S[focus].Alive {
		r.Robust, r.CutCells = f.articulation(st, focus, r.Reach(focus))
	} else {
		r.Robust = r.Reach(0)
	}
	return r
}

// articulation is an iterative Tarjan DFS over the focus snake's reachable
// region: cut cells are cells whose removal disconnects part of the region.
// Space behind a single narrow entrance another snake can reach is easily
// sealed, so the largest such pocket is discounted from reach.
func (f *Fill) articulation(st *State, focus, reach int) (robust, cuts int) {
	g := st.G
	n := g.Cells
	f.disc, f.low, f.sub, f.sep, f.parent = fit32(f.disc, n), fit32(f.low, n), fit32(f.sub, n), fit32(f.sep, n), fit32(f.parent, n)
	bit := uint8(1) << uint(focus)
	root := int32(st.S[focus].Head())
	in := func(c int32) bool { return c == root || (f.mask[c]&bit != 0 && f.dist[c] > 0) }
	for i := 0; i < n; i++ {
		f.disc[i], f.sep[i] = 0, 0
	}
	timer := int32(1)
	f.disc[root], f.low[root], f.sub[root], f.parent[root] = 1, 1, 1, -1
	stack := append(f.stack[:0], frame{root, 0})
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.d < 4 {
			nb := int32(g.Nbr[top.c][top.d])
			top.d++
			if nb < 0 || !in(nb) {
				continue
			}
			if f.disc[nb] == 0 {
				timer++
				f.disc[nb], f.low[nb], f.sub[nb], f.parent[nb] = timer, timer, 1, top.c
				stack = append(stack, frame{nb, 0})
			} else if nb != f.parent[top.c] && f.disc[nb] < f.low[top.c] {
				f.low[top.c] = f.disc[nb]
			}
			continue
		}
		c := top.c
		stack = stack[:len(stack)-1]
		par := f.parent[c]
		if par < 0 {
			continue
		}
		if f.low[c] < f.low[par] {
			f.low[par] = f.low[c]
		}
		f.sub[par] += f.sub[c]
		if par != root && f.low[c] >= f.disc[par] {
			f.sep[par] += f.sub[c]
		}
	}
	f.stack = stack

	worst := 0
	for c := 0; c < n; c++ {
		if f.sep[c] == 0 {
			continue
		}
		cuts++
		if pocket := int(f.sep[c]) + 1; pocket > worst && f.sealable(g, c, bit) {
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
func (f *Fill) sealable(g *Game, c int, bit uint8) bool {
	if f.mask[c]&^bit != 0 {
		return true
	}
	for d := 0; d < 4; d++ {
		if q := g.Nbr[c][d]; q >= 0 && f.mask[q]&^bit != 0 {
			return true
		}
	}
	return false
}
