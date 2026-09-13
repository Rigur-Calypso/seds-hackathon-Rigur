// Package eval scores a resolved position from snake 0's perspective.
//
// Terminal scores are ordered bands, not magic numbers (coreyja): every loss <
// every heuristic value < every win. Losses are ordered by how many opponents
// are still alive (placement), then by death cause (bookworm: head-to-head >
// starvation > body > self).
package eval

import (
	"math"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/royale"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/voronoi"
)

// Score bands.
const (
	LoseBase = -3.0 // losses occupy [-3, -2.1]
	WinBase  = 2.0  // wins occupy [2, 2.5]
)

// IsLoss / IsWin classify a (possibly ply-adjusted) score.
func IsLoss(v float64) bool { return v < -1.5 }
func IsWin(v float64) bool  { return v > 1.5 }

func causeRank(c board.Cause) float64 {
	switch c {
	case board.ByHeadToHead:
		return 1
	case board.ByOutOfHealth, board.ByHazard:
		return 0.5
	case board.ByCollision:
		return 0.3
	}
	return 0
}

// Score values next (reached from prev by one simultaneous turn).
func Score(prev, next *board.State, p *config.Params) float64 {
	me := &next.Snakes[0]
	prevOpp, oppAlive := 0, 0
	for i := 1; i < len(next.Snakes); i++ {
		if i < len(prev.Snakes) && prev.Snakes[i].Alive() {
			prevOpp++
		}
		if next.Snakes[i].Alive() {
			oppAlive++
		}
	}
	if !me.Alive() {
		frac := 0.0
		if prevOpp > 0 {
			frac = 1 - float64(oppAlive)/float64(prevOpp)
		}
		return LoseBase + 0.6*frac + 0.3*causeRank(me.Cause)
	}
	if oppAlive == 0 && prevOpp > 0 {
		return WinBase + 0.5*float64(me.Health)/100
	}
	return Heuristic(prev, next, p)
}

// Heuristic is the non-terminal value in (-1, 1): separate, ratio-based terms
// (area as a ratio of free cells, never a raw count) squashed with tanh.
func Heuristic(prev, next *board.State, p *config.Params) float64 {
	v := voronoi.Compute(next, 0)
	me := &next.Snakes[0]
	L := me.Len()
	free := float64(v.FreeCells)
	if free < 1 {
		free = 1
	}
	span := float64(next.W + next.H)

	area := func(j int) float64 {
		a := float64(v.Attack[j])
		return (float64(v.Guaranteed[j]) + p.AttackCellWeight*a + p.ContestedWeight*(float64(v.Contested[j])-a)) / free
	}

	h := 0.0
	bestOpp, maxOppLen := 0.0, 0
	for j := 1; j < v.N; j++ {
		o := &next.Snakes[j]
		if !o.Alive() {
			continue
		}
		if a := area(j); a > bestOpp {
			bestOpp = a
		}
		if o.Len() > maxOppLen {
			maxOppLen = o.Len()
		}
		if v.Trapped[j] {
			h += p.WOppTrapped // forced elimination pending
		}
	}
	for j := 1; j < len(prev.Snakes) && j < len(next.Snakes); j++ {
		if prev.Snakes[j].Alive() && !next.Snakes[j].Alive() {
			h += p.WKill
		}
	}

	h += p.WArea * (area(0) - bestOpp)
	if reach := v.Reach(0); reach < L {
		h -= p.WTrapped * (1 - float64(reach)/float64(L))
	}
	if rb := v.Robust; rb < L {
		h -= p.WRobust * (1 - float64(rb)/float64(L))
	}
	exits := v.SafeExits[0]
	if exits > 2 {
		exits = 2
	}
	h += p.WExits * float64(exits) / 2
	// Two-ply head-to-head awareness: TVAE resolves one ply exactly, but a
	// position where every exit next turn can be contested by an equal-or-longer
	// head is a forced coin-flip we must not walk into (found in a CLI game where
	// four snakes converged on the centre and all died head-to-head).
	if v.SafeExits[0] > 0 && v.ExitsUncontested[0] == 0 {
		h -= p.WNoSafeExit
	}
	h += p.WAttack * float64(v.Attack[0]) / free

	if !next.Rules.Constrictor {
		health := me.Health
		fd, grow := v.FoodDist[0], v.FoodDist[0]
		if p.WinnableFood {
			// P3: plan hunger and growth around food we win and can leave, not food
			// an equal-or-longer snake reaches first or that seals us in. Growth only
			// counts winnable food. Hunger falls back to the nearest reachable food
			// when none is winnable: a contested meal still beats starving, and the
			// span slack (health - (W+H)) would under-state the urgency.
			grow = v.WinFoodDist[0]
			if grow >= 0 {
				fd = grow
			}
		}
		slack := health - fd
		if fd < 0 {
			slack = health - int(span)
		}
		if p.HungerMargin > 0 && slack < p.HungerMargin {
			u := float64(p.HungerMargin-slack) / float64(p.HungerMargin)
			if u > 2 {
				u = 2
			}
			h -= p.WHunger * u
		}
		want := p.WFood
		if p.FoodDecayTurns > 0 {
			want *= math.Max(p.FoodFloor, 1-float64(next.Turn)/float64(p.FoodDecayTurns))
		}
		if L > maxOppLen+p.LengthLead {
			want *= p.SatiatedFoodScale
		}
		ate := len(prev.Snakes) > 0 && L > prev.Snakes[0].Len()
		switch {
		case ate:
			h += want
		case fd >= 0:
			h += want * (1 - float64(fd)/span)
		}
		if p.LengthScale > 0 {
			h += p.WLength * math.Tanh(float64(L-maxOppLen)/p.LengthScale)
		}
		if health > 0 {
			h += p.WHealth * math.Sqrt(float64(health)/100)
		}
	}

	if next.Rules.Royale || next.Hazard != nil {
		h += storm(next, p, span)
	}
	return math.Tanh(h)
}

// storm is the royale layer (Step 7).
func storm(s *board.State, p *config.Params, span float64) float64 {
	me := &s.Snakes[0]
	hd := me.Head()
	dmg := s.Rules.HazardDamage
	h := 0.0
	if s.Rules.Royale {
		r := board.SafeRect(s)
		if !r.Empty() {
			cx, cy := r.Centre()
			d := (math.Abs(float64(hd.X)-cx) + math.Abs(float64(hd.Y)-cy)) / span
			ramp := 1.0
			if p.CentreRampTurns > 0 {
				ramp = math.Min(1, float64(s.Turn)/float64(p.CentreRampTurns))
			}
			h -= p.WCentre * ramp * d
			if royale.TurnsToShrink(s.Turn, s.Rules.ShrinkEveryN) <= p.ShrinkLookahead && !royale.ShrinkRobust(hd, r) {
				h -= p.WShrinkRobust
			}
		}
	}
	if s.HazardAt(hd) > 0 && !s.HasFood(hd) {
		h -= p.WInHazard
		if royale.HazardBudget(me.Health, dmg) <= 1 {
			h -= p.WInHazard
		}
	}
	// Hazard as a weapon: an opponent standing in the storm that cannot afford it.
	for j := 1; j < len(s.Snakes); j++ {
		o := &s.Snakes[j]
		if o.Alive() && s.HazardAt(o.Head()) > 0 && royale.HazardBudget(o.Health, dmg) <= 1 {
			h += p.WHazardWeapon
		}
	}
	return h
}
