package envelope

import (
	"math"
	"sort"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
)

// Aggregate folds one candidate's outcome envelope into a score:
//
//	score = (RiskMean·E[v] + RiskCVaR·CVaR_α[v] + RiskMin·min v) / (sum of risk weights)
//
// CVaR_α is the weighted mean of the worst α of outcome mass. Qualifying is
// CVaR-dominated (a forced-4th path loses to a safe alternative even if its
// average is better), the bracket blends in expectation, the final leans on
// the minimum. Profiles choose the blend; nothing here is stage-specific.
func Aggregate(m board.Dir, os []outcome, p *config.Params) Candidate {
	c := Candidate{Dir: m, Outcomes: len(os)}
	if len(os) == 0 {
		c.Score = math.Inf(-1)
		return c
	}
	sort.SliceStable(os, func(a, b int) bool { return os[a].v < os[b].v })
	tw, mean := 0.0, 0.0
	for _, o := range os {
		tw += o.w
		mean += o.w * o.v
	}
	if tw <= 0 {
		tw = float64(len(os))
		mean = 0
		for i := range os {
			os[i].w = 1
			mean += os[i].v
		}
	}
	mean /= tw

	alpha := p.CVaRAlpha
	if alpha <= 0 || alpha > 1 {
		alpha = 1
	}
	tail := alpha * tw
	acc, cv := 0.0, 0.0
	for _, o := range os {
		take := math.Min(o.w, tail-acc)
		if take <= 0 {
			break
		}
		cv += take * o.v
		acc += take
	}
	cvar := os[0].v
	if acc > 0 {
		cvar = cv / acc
	}

	c.Mean, c.CVaR, c.Min = mean, cvar, os[0].v
	sum := p.RiskMean + p.RiskCVaR + p.RiskMin
	if sum <= 0 {
		c.Score = mean
		return c
	}
	c.Score = (p.RiskMean*mean + p.RiskCVaR*cvar + p.RiskMin*c.Min) / sum
	return c
}
