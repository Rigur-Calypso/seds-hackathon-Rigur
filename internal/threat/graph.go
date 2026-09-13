// Package threat is the two-turn Threat Graph (P1, from the owner's Codex
// review). The loss diagnostics (DEVLOG §9.1) showed the snake never blunders
// on its final move: 70–76 % of losses were sealed one to three turns earlier,
// most of them "sandwiches" — a safe-looking cell after which opponents close
// every exit together.
//
// TVAE resolves the first simultaneous turn exactly. For each resolved outcome
// this package asks one more question, selectively: is there a single joint
// reply by the nearby opponents that makes EVERY next move of ours fatal (or,
// with ThreatTrapRefutes, leaves us without room)? If so the outcome is a
// forced squeeze. The check models simultaneous moves correctly — the opponents
// commit to one reply without seeing our next move — and it only runs where a
// squeeze is plausible.
package threat

import (
	"sort"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/rules"
)

// Budget bounds the resolutions spent across one decision. Zero or less means
// no more checks; an unfinished check never marks an outcome forced.
type Budget struct{ Left int }

// Result of one check.
type Result struct {
	Checked bool // the check ran to completion
	Forced  bool // some joint opponent reply refutes every next move of ours
	Evals   int
}

type near struct{ idx, dist int }

func nearby(s *board.State, p *config.Params) []near {
	head := s.Snakes[0].Head()
	var out []near
	for j := 1; j < len(s.Snakes); j++ {
		o := &s.Snakes[j]
		if !o.Alive() || len(o.Body) == 0 {
			continue
		}
		if d := s.Dist(head, o.Head()); d <= p.ThreatRadius {
			out = append(out, near{j, d})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].dist < out[b].dist })
	return out
}

// Triggered reports whether a squeeze is plausible in s (snake 0 to move): an
// opponent within ThreatRadius, and in addition a second nearby opponent, a
// corridor (at most one safe exit), tight space ahead (every exit leads to
// less than twice our length) or every exit contested by an equal-or-longer head.
func Triggered(s *board.State, p *config.Params) bool {
	if len(s.Snakes) == 0 || !s.Snakes[0].Alive() || len(s.Snakes[0].Body) == 0 {
		return false
	}
	nb := nearby(s, p)
	if len(nb) == 0 {
		return false
	}
	if len(nb) >= 2 {
		return true
	}
	blocked := legal.Blocked(s)
	L := s.Snakes[0].Len()
	safe, good, roomy := 0, 0, 0
	for _, in := range legal.Analyze(s, blocked, 0) {
		if !in.Safe() {
			continue
		}
		safe++
		if !in.H2HLose {
			good++
		}
		if legal.FloodArea(s, blocked, in.Next, 2*L) >= 2*L {
			roomy++
		}
	}
	return safe <= 1 || good == 0 || roomy == 0
}

// Check runs the squeeze test on s. It spends at most b.Left resolutions.
func Check(s *board.State, p *config.Params, b *Budget) Result {
	var r Result
	if len(s.Snakes) == 0 || !s.Snakes[0].Alive() {
		return r
	}
	ours := legal.Safe(s, 0)
	if len(ours) == 0 {
		return Result{Checked: true, Forced: true}
	}
	nb := nearby(s, p)
	if p.ThreatLongerOnly {
		// A shorter head loses any head-to-head (R2); enumerating its replies
		// mostly manufactures squeezes it cannot enforce.
		L := s.Snakes[0].Len()
		kept := nb[:0]
		for _, n := range nb {
			if s.Snakes[n.idx].Len() >= L {
				kept = append(kept, n)
			}
		}
		nb = kept
	}
	if len(nb) > p.ThreatMaxOpponents {
		nb = nb[:p.ThreatMaxOpponents]
	}
	if len(nb) == 0 {
		return Result{Checked: true}
	}

	moves := make([]board.Dir, len(s.Snakes))
	for i := range s.Snakes {
		if len(s.Snakes[i].Body) > 0 {
			moves[i] = legal.DefaultMove(&s.Snakes[i])
		}
	}
	replies := make([][]board.Dir, len(nb))
	for k, n := range nb {
		replies[k] = legal.Safe(s, n.idx)
		if len(replies[k]) == 0 {
			replies[k] = []board.Dir{legal.DefaultMove(&s.Snakes[n.idx])}
		}
	}

	idx := make([]int, len(nb))
	for {
		for k, n := range nb {
			moves[n.idx] = replies[k][idx[k]]
		}
		covers := true
		for _, d := range ours {
			if b.Left <= 0 {
				r.Checked = false
				return r
			}
			b.Left--
			r.Evals++
			moves[0] = d
			if !refuted(s, moves, p) {
				covers = false
				break
			}
		}
		if covers {
			r.Checked, r.Forced = true, true
			return r
		}
		k := 0
		for ; k < len(nb); k++ {
			idx[k]++
			if idx[k] < len(replies[k]) {
				break
			}
			idx[k] = 0
		}
		if k == len(nb) {
			break
		}
	}
	r.Checked = true
	return r
}

// refuted: after this joint move we are dead — or, with ThreatTrapRefutes,
// alive with less room than our own length.
func refuted(s *board.State, moves []board.Dir, p *config.Params) bool {
	n := rules.Resolve(s, moves, rules.Options{})
	me := &n.Snakes[0]
	if !me.Alive() {
		return true
	}
	if !p.ThreatTrapRefutes {
		return false
	}
	return room(n, me.Len()) < me.Len()
}

// room counts, up to limit, the cells snake 0 can reach strictly before every
// equal-or-longer opponent head (ties go to them). A shorter snake does not
// claim space against us — it loses any head-to-head (R2) — but every body,
// ours included, blocks a cell until its segment is released (R4; never in
// constrictor, R9). This is deliberately not the evaluator's Voronoi fill,
// which lets a shorter snake that arrives first own space it cannot defend and
// so over-reports squeezes.
func room(s *board.State, limit int) int {
	n := s.Cells()
	me := &s.Snakes[0]
	L := me.Len()
	freeAt := make([]int32, n)
	for j := range s.Snakes {
		sn := &s.Snakes[j]
		if !sn.Alive() {
			continue
		}
		lj := len(sn.Body)
		for k := lj - 1; k >= 0; k-- {
			p := sn.Body[k]
			if !s.InBounds(p) {
				continue
			}
			fa := int32(lj - k)
			if s.Rules.Constrictor {
				fa = 1 << 29
			}
			if c := s.Idx(p); fa > freeAt[c] {
				freeAt[c] = fa
			}
		}
	}
	seen := make([]bool, n)
	var ours, theirs []int32
	h := s.Idx(me.Head())
	seen[h] = true
	ours = append(ours, int32(h))
	for j := 1; j < len(s.Snakes); j++ {
		o := &s.Snakes[j]
		if !o.Alive() || len(o.Body) == 0 || o.Len() < L || !s.InBounds(o.Head()) {
			continue
		}
		c := s.Idx(o.Head())
		seen[c] = true
		theirs = append(theirs, int32(c))
	}
	expand := func(front []int32, t int32, onClaim func()) []int32 {
		var next []int32
		for _, c := range front {
			p := s.Pt(int(c))
			for _, d := range board.AllDirs {
				q, ok := s.Step(p, d)
				if !ok {
					continue
				}
				x := s.Idx(q)
				if seen[x] || freeAt[x] > t {
					continue
				}
				seen[x] = true
				next = append(next, int32(x))
				if onClaim != nil {
					onClaim()
				}
			}
		}
		return next
	}
	count := 0
	for t := int32(1); len(ours) > 0 && count < limit; t++ {
		theirs = expand(theirs, t, nil) // opponents first: ties go to them
		ours = expand(ours, t, func() { count++ })
	}
	if count > limit {
		count = limit
	}
	return count
}
