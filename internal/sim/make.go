package sim

import "github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"

// Moves is one move per snake slot (ignored for eliminated snakes).
type Moves [MaxSnakes]board.Dir

type snakeUndo struct {
	start, len, health int32
	tail               int16
	alive              bool
	moved              bool // head advanced, tail popped
	grew               bool // ate: duplicated tail appended
	constricted        bool // R9 growth appended
	removed            bool // eliminated this turn: body taken out of occ
}

// Undo holds what Unmake needs. Reuse one per search ply: it allocates only
// the first time food is eaten at that ply.
type Undo struct {
	turn  int32
	hash  uint64
	sn    [MaxSnakes]snakeUndo
	eaten []int16
}

// Make applies one simultaneous turn in place, in the engine's order
// (CLAUDE.md §2):
//
//	Movement → Starvation → HazardDamage → Feed → Elimination → [Constrictor]
//
// Royale rings are not regenerated here: the hazard layer is a function of the
// turn (Game.Hazards), which is how search models shrinks it cannot see (R8).
func (st *State) Make(mv *Moves, u *Undo) {
	g := st.G
	u.turn, u.hash = st.Turn, st.Hash
	u.eaten = u.eaten[:0]
	hz := g.Hazards(st.Turn)
	capB := g.capBody
	n := st.N
	hash := st.Hash ^ keyShrink(g.layer(st.Turn)) ^ keyShrink(g.layer(st.Turn+1))
	var dead [MaxSnakes]bool

	// Movement (prepend head, pop tail — R4 falls out of this) and starvation (R1).
	for i := 0; i < n; i++ {
		sn := &st.S[i]
		us := &u.sn[i]
		*us = snakeUndo{start: sn.start, len: sn.Len, health: sn.Health, alive: sn.Alive}
		if !sn.Alive {
			continue
		}
		hash ^= keyHead(i, sn.Head()) ^ keyLen(i, sn.Len) ^ keyHealth(i, sn.Health)
		sn.Health--
		h := g.Nbr[sn.Head()][mv[i]&3]
		if h < 0 {
			dead[i] = true // out of bounds: the body never moves
			continue
		}
		tail := sn.Seg(sn.Len - 1)
		us.tail, us.moved = tail, true
		st.occ[tail]--
		sn.start--
		if sn.start < 0 {
			sn.start += capB
		}
		sn.body[sn.start] = h
		st.occ[h]++
		hash ^= keySeg(i, tail) ^ keySeg(i, h)
	}

	// Hazard damage (R5, R6): no damage on a square with food; otherwise once per
	// stacked hazard, clamped to [0, 100], eliminating inline before feeding.
	if hz != nil {
		for i := 0; i < n; i++ {
			sn := &st.S[i]
			if !sn.Alive || dead[i] {
				continue
			}
			h := sn.Head()
			cnt := int(hz[h])
			if cnt == 0 || st.food[h] {
				continue
			}
			for k := 0; k < cnt; k++ {
				sn.Health -= g.Damage
				if sn.Health < 0 {
					sn.Health = 0
				}
				if sn.Health > MaxHealth {
					sn.Health = MaxHealth
				}
				if sn.Health <= 0 {
					dead[i] = true
				}
			}
		}
	}

	// Feeding: every snake still in play whose head is on food grows by a
	// duplicated tail and resets to full health (R1). Food is shared, then consumed.
	for i := 0; i < n; i++ {
		sn := &st.S[i]
		if !sn.Alive || dead[i] || !u.sn[i].moved {
			continue
		}
		h := sn.Head()
		if !st.food[h] {
			continue
		}
		st.grow(i, &hash)
		sn.Health = MaxHealth
		u.sn[i].grew = true
		seen := false
		for _, c := range u.eaten {
			seen = seen || c == h
		}
		if !seen {
			u.eaten = append(u.eaten, h)
		}
	}
	for _, c := range u.eaten {
		st.food[c] = false
		hash ^= keyFood(c)
	}

	// Elimination phase 1: out of health, out of bounds. R12: these snakes do not
	// block anyone this tick, so their bodies leave occ before collisions.
	for i := 0; i < n; i++ {
		sn := &st.S[i]
		if sn.Alive && (dead[i] || sn.Health <= 0) {
			dead[i] = true
			st.removeBody(i, &hash)
			u.sn[i].removed = true
		}
	}

	// Phase 2: collisions against everyone still in play, applied together (R7).
	// A head's cell holds body segments when occ exceeds the heads standing there
	// (R3: heads are not bodies). R2: len(me) <= len(them) loses a head-to-head.
	var pend [MaxSnakes]bool
	for i := 0; i < n; i++ {
		sn := &st.S[i]
		if !sn.Alive || dead[i] {
			continue
		}
		h := sn.Head()
		heads := 0
		for j := 0; j < n; j++ {
			if st.S[j].Alive && !dead[j] && st.S[j].Head() == h {
				heads++
			}
		}
		if int(st.occ[h]) > heads {
			pend[i] = true
			continue
		}
		if heads > 1 {
			for j := 0; j < n; j++ {
				o := &st.S[j]
				if j != i && o.Alive && !dead[j] && o.Head() == h && sn.Len <= o.Len {
					pend[i] = true
					break
				}
			}
		}
	}
	for i := 0; i < n; i++ {
		if pend[i] {
			dead[i] = true
			st.removeBody(i, &hash)
			u.sn[i].removed = true
		}
	}
	for i := 0; i < n; i++ {
		if dead[i] {
			st.S[i].Alive = false
		}
	}

	// R9 constrictor: food cleared, health pinned, every snake grows unless its
	// tail is already duplicated.
	if g.Constrictor {
		for _, c := range st.foods {
			if st.food[c] {
				st.food[c] = false
				hash ^= keyFood(c)
				u.eaten = append(u.eaten, c)
			}
		}
		for i := 0; i < n; i++ {
			sn := &st.S[i]
			if !sn.Alive {
				continue
			}
			sn.Health = MaxHealth
			if sn.Len >= 2 && sn.Seg(sn.Len-1) != sn.Seg(sn.Len-2) {
				st.grow(i, &hash)
				u.sn[i].constricted = true
			}
		}
	}

	st.Turn++
	for i := 0; i < n; i++ {
		if !u.sn[i].alive {
			continue
		}
		if sn := &st.S[i]; sn.Alive {
			hash ^= keyHead(i, sn.Head()) ^ keyLen(i, sn.Len) ^ keyHealth(i, sn.Health)
		} else {
			hash ^= keyDead(i)
		}
	}
	st.Hash = hash
}

// grow appends a duplicate of the last segment.
func (st *State) grow(i int, hash *uint64) {
	sn := &st.S[i]
	capB := st.G.capBody
	last := sn.Seg(sn.Len - 1)
	idx := sn.start + sn.Len
	if idx >= capB {
		idx -= capB
	}
	sn.body[idx] = last
	sn.Len++
	st.occ[last]++
	*hash ^= keySeg(i, last)
}

func (st *State) removeBody(i int, hash *uint64) {
	sn := &st.S[i]
	for k := int32(0); k < sn.Len; k++ {
		c := sn.Seg(k)
		st.occ[c]--
		*hash ^= keySeg(i, c)
	}
}

// Unmake reverts the Make that filled u, restoring the position exactly.
func (st *State) Unmake(u *Undo) {
	capB := st.G.capBody
	n := st.N
	for i := 0; i < n; i++ {
		sn := &st.S[i]
		us := &u.sn[i]
		if us.constricted {
			sn.Len--
			st.occ[sn.Seg(sn.Len)]--
		}
		if us.removed {
			for k := int32(0); k < sn.Len; k++ {
				st.occ[sn.Seg(k)]++
			}
		}
	}
	for _, c := range u.eaten {
		st.food[c] = true
	}
	for i := 0; i < n; i++ {
		sn := &st.S[i]
		us := &u.sn[i]
		if us.grew {
			sn.Len--
			st.occ[sn.Seg(sn.Len)]--
		}
		if us.moved {
			st.occ[sn.Head()]--
			sn.start++
			if sn.start >= capB {
				sn.start -= capB
			}
			idx := sn.start + sn.Len - 1
			if idx >= capB {
				idx -= capB
			}
			sn.body[idx] = us.tail
			st.occ[us.tail]++
		}
		sn.start, sn.Len, sn.Health, sn.Alive = us.start, us.len, us.health, us.alive
	}
	st.Turn, st.Hash = u.turn, u.hash
}

// BlockedNext reports whether a head entering c next turn certainly hits a
// body: some live segment stays there after every tail that vacates (R4) pops.
func (st *State) BlockedNext(c int16) bool {
	o := int(st.occ[c])
	if o == 0 {
		return false
	}
	for j := 0; j < st.N; j++ {
		sn := &st.S[j]
		if sn.Alive && sn.Tail() == c && sn.TailVacates() {
			o--
		}
	}
	return o > 0
}

// SafeMoves writes snake i's moves that are not certain death on their own —
// off the board, into a body that stays, or lethal starvation or hazard damage
// (R1, R5, R6) — and returns how many. Head-to-heads are left to the search.
func (st *State) SafeMoves(i int, out *[4]board.Dir) int {
	sn := &st.S[i]
	if !sn.Alive {
		return 0
	}
	g := st.G
	hz := g.Hazards(st.Turn)
	nb := &g.Nbr[sn.Head()]
	k := 0
	for d := 0; d < 4; d++ {
		c := nb[d]
		if c < 0 || st.BlockedNext(c) {
			continue
		}
		if !st.food[c] {
			h := sn.Health - 1
			if hz != nil {
				h -= g.Damage * int32(hz[c])
			}
			if h <= 0 {
				continue
			}
		}
		out[k] = board.Dir(d)
		k++
	}
	return k
}
