package brain

import (
	"math"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/royale"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/sim"
)

// Terminal bands, identical to internal/eval so logs compare across engines:
// every loss < every heuristic value < every win (coreyja).
const (
	loseBase = -3.0
	winBase  = 2.0
)

// IsLoss / IsWin classify a (possibly ply-adjusted) value.
func IsLoss(v float64) bool { return v < -1.5 }
func IsWin(v float64) bool  { return v > 1.5 }

// lossValue orders deaths by placement (opponents eliminated since the root —
// if dying, take someone along) and then by time (later is better).
func (se *Searcher) lossValue(ply int) float64 {
	frac := 0.0
	if se.rootOpp > 0 {
		killed := 0
		for j := 1; j < se.st.N; j++ {
			if se.rootAlive[j] && !se.st.S[j].Alive {
				killed++
			}
		}
		frac = float64(killed) / float64(se.rootOpp)
	}
	return loseBase + 0.6*frac + se.p.PlyStep*float64(ply)
}

// winValue prefers sooner wins with more health.
func (se *Searcher) winValue(ply int) float64 {
	return winBase + 0.5*float64(se.st.S[0].Health)/100 - se.p.PlyStep*float64(ply)
}

func (se *Searcher) leaf() float64 {
	r := se.fill.Compute(se.st, 0, se.p.WRobust != 0)
	return se.heuristic(&r)
}

// heuristic is the non-terminal value in (-1, 1). It is internal/eval's
// Heuristic term for term (the same tuned weights), measured against the search
// root instead of the previous turn: kills and growth count since the root.
func (se *Searcher) heuristic(v *sim.Result) float64 {
	st, g, p := se.st, se.g, se.p
	me := &st.S[0]
	L := int(me.Len)
	free := float64(v.FreeCells)
	if free < 1 {
		free = 1
	}
	span := float64(g.W + g.H)
	area := func(j int) float64 {
		a := float64(v.Attack[j])
		return (float64(v.Guaranteed[j]) + p.AttackCellWeight*a + p.ContestedWeight*(float64(v.Contested[j])-a)) / free
	}

	h := 0.0
	bestOpp, maxOppLen := 0.0, 0
	for j := 1; j < st.N; j++ {
		o := &st.S[j]
		if !o.Alive {
			if se.rootAlive[j] {
				h += p.WKill
			}
			continue
		}
		if a := area(j); a > bestOpp {
			bestOpp = a
		}
		if int(o.Len) > maxOppLen {
			maxOppLen = int(o.Len)
		}
		if v.Trapped[j] {
			h += p.WOppTrapped
		}
	}

	h += p.WArea * (area(0) - bestOpp)
	// Self-coiling: with the cliff at exactly our length, a long snake with
	// "just enough" room feels nothing, folds into its territory and is sealed
	// a few turns later. SpaceFactor > 1 starts the penalty earlier and ramps it
	// smoothly; 1 is v1's term.
	need := float64(L) * p.SpaceFactor
	if reach := float64(v.Reach(0)); reach < need {
		h -= p.WTrapped * (1 - reach/need)
	}
	if rb := float64(v.Robust); p.WRobust != 0 && rb < need {
		h -= p.WRobust * (1 - rb/need)
	}
	// Room that is not our own body waiting to free: a coil survives only by
	// following its tail exactly, which one opponent or one meal breaks.
	if p.WSelfReliance != 0 {
		if fresh := float64(v.Reach(0) - v.OwnReleased[0]); fresh < float64(L) {
			h -= p.WSelfReliance * (1 - fresh/float64(L))
		}
	}
	exits := v.SafeExits[0]
	if exits > 2 {
		exits = 2
	}
	h += p.WExits * float64(exits) / 2
	if v.SafeExits[0] > 0 && v.ExitsUncontested[0] == 0 {
		h -= p.WNoSafeExit
	}
	h += p.WAttack * float64(v.Attack[0]) / free

	if !g.Constrictor {
		health := int(me.Health)
		fd := v.FoodDist[0]
		slack := health - fd
		if fd < 0 {
			slack = health - int(span)
		}
		if p.HazardHunger && g.Hazards(st.Turn) != nil && v.FoodHealth[0] >= 0 {
			slack = v.FoodHealth[0] // storm crossed on the way is charged (R6)
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
			want *= math.Max(p.FoodFloor, 1-float64(st.Turn)/float64(p.FoodDecayTurns))
		}
		if L > maxOppLen+p.LengthLead {
			want *= p.SatiatedFoodScale
		}
		if maxOppLen > 0 && L <= maxOppLen {
			// Not strictly longest: every head-to-head is lost or traded (R2), and
			// a longer opponent can shadow us indefinitely. Growth must outweigh
			// the noise of a deep paranoid search, which v1's one-ply choice never had.
			want *= p.FoodDeficitScale
		}
		switch {
		case me.Len > se.rootLen:
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

	if g.Royale || g.Hazards(st.Turn) != nil {
		h += se.storm(span)
	}
	return math.Tanh(h)
}

// storm is the royale layer, as in internal/eval.
func (se *Searcher) storm(span float64) float64 {
	st, g, p := se.st, se.g, se.p
	me := &st.S[0]
	hd := me.Head()
	pt := board.Point{X: int(hd) % g.W, Y: int(hd) / g.W}
	dmg := int(g.Damage)
	hz := g.Hazards(st.Turn)
	h := 0.0
	if g.Royale {
		if r := g.SafeRect(st.Turn); !r.Empty() {
			cx, cy := r.Centre()
			d := (math.Abs(float64(pt.X)-cx) + math.Abs(float64(pt.Y)-cy)) / span
			ramp := 1.0
			if p.CentreRampTurns > 0 {
				ramp = math.Min(1, float64(st.Turn)/float64(p.CentreRampTurns))
			}
			h -= p.WCentre * ramp * d
			if royale.TurnsToShrink(int(st.Turn), g.ShrinkEvery) <= p.ShrinkLookahead && !royale.ShrinkRobust(pt, r) {
				h -= p.WShrinkRobust
			}
		}
	}
	if hz == nil {
		return h
	}
	if hz[hd] > 0 && !st.Food(hd) {
		h -= p.WInHazard
		if royale.HazardBudget(int(me.Health), dmg) <= 1 {
			h -= p.WInHazard
		}
	}
	for j := 1; j < st.N; j++ {
		if o := &st.S[j]; o.Alive && hz[o.Head()] > 0 && royale.HazardBudget(int(o.Health), dmg) <= 1 {
			h += p.WHazardWeapon
		}
	}
	return h
}
