package sim

// ReachCount counts the cells snake j could ever enter from its head (head
// excluded) under time-aware body release, ignoring every other head. In
// constrictor (R9: nothing frees) each move needs a new cell, so this bounds
// how many more turns the snake can live.
func (f *Fill) ReachCount(st *State, j int) int {
	g := st.G
	f.reset(g.Cells)
	f.bodies(st)
	sn := &st.S[j]
	if !sn.Alive {
		return 0
	}
	fr := append(f.fr[0][:0], node{c: int32(sn.Head())})
	f.mask[sn.Head()] = 1
	nx := f.nx[0][:0]
	count := 0
	for t := int32(1); len(fr) > 0; t++ {
		nx = nx[:0]
		for _, nd := range fr {
			nb := &g.Nbr[nd.c]
			for d := 0; d < 4; d++ {
				c := nb[d]
				if c < 0 || f.mask[c] != 0 || f.freeAt[c] > t {
					continue
				}
				f.mask[c] = 1
				count++
				nx = append(nx, node{c: int32(c)})
			}
		}
		fr, nx = nx, fr
	}
	f.fr[0], f.nx[0] = fr, nx
	return count
}

// bodies fills freeAt from every live body (R4 release times; R9 permanent).
func (f *Fill) bodies(st *State) {
	g := st.G
	for j := 0; j < st.N; j++ {
		sn := &st.S[j]
		if !sn.Alive {
			continue
		}
		L := sn.Len
		for k := L - 1; k >= 0; k-- {
			c := sn.Seg(k)
			fa := L - k
			if g.Constrictor {
				fa = permanent
			}
			if fa > f.freeAt[c] {
				f.freeAt[c] = fa
			}
		}
	}
}

// Isolated reports whether no other live snake can ever reach a cell snake 0
// can reach, under time-aware body release (R4; never in constrictor, R9) and
// ignoring who arrives first. Health is ignored, so "isolated" is conservative
// only about starvation. Two lockstep floods: every opponent head together,
// then ours; any overlap means the regions touch.
func (f *Fill) Isolated(st *State) bool {
	g := st.G
	f.reset(g.Cells)
	f.bodies(st)
	const oppBit, ourBit = 1, 2
	flood := func(bit uint8, start func(add func(c int16))) bool {
		fr := f.fr[0][:0]
		start(func(c int16) {
			if f.mask[c]&bit == 0 {
				f.mask[c] |= bit
				fr = append(fr, node{c: int32(c)})
			}
		})
		nx := f.nx[0][:0]
		for t := int32(1); len(fr) > 0; t++ {
			nx = nx[:0]
			for _, nd := range fr {
				nb := &g.Nbr[nd.c]
				for d := 0; d < 4; d++ {
					c := nb[d]
					if c < 0 || f.mask[c]&bit != 0 || f.freeAt[c] > t {
						continue
					}
					if bit == ourBit && f.mask[c]&oppBit != 0 {
						f.fr[0], f.nx[0] = fr, nx
						return true
					}
					f.mask[c] |= bit
					nx = append(nx, node{c: int32(c)})
				}
			}
			fr, nx = nx, fr
		}
		f.fr[0], f.nx[0] = fr, nx
		return false
	}
	flood(oppBit, func(add func(int16)) {
		for j := 1; j < st.N; j++ {
			if st.S[j].Alive {
				add(st.S[j].Head())
			}
		}
	})
	touched := flood(ourBit, func(add func(int16)) { add(st.S[0].Head()) })
	return !touched
}
