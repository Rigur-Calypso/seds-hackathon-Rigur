package main

import (
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/decide"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
)

// Decision-level loss diagnostics (DEVLOG §9.1). For every decision of our
// snake we record how many options were viable — safe, not a losing
// head-to-head, and not into a dead end smaller than our length. A loss is then
// classified by whether the fatal move was avoidable and how many turns before
// death the snake last had a real choice (two or more viable options).

type decisionRec struct {
	Turn, Safe, Good, Viable int
	Reason                   string
	Best                     float64
}

func recordDecision(gs *api.GameState, d decide.Decision) decisionRec {
	rec := decisionRec{Turn: gs.Turn, Reason: string(d.Reason), Best: -9}
	if s, ok := board.FromAPI(gs); ok {
		blocked := legal.Blocked(s)
		L := s.Snakes[0].Len()
		for _, in := range legal.Analyze(s, blocked, 0) {
			if !in.Safe() {
				continue
			}
			rec.Safe++
			if in.H2HLose {
				continue
			}
			rec.Good++
			if legal.FloodArea(s, blocked, in.Next, L) >= L {
				rec.Viable++
			}
		}
	}
	for _, sc := range d.Scores {
		if sc.Value > rec.Best {
			rec.Best = sc.Value
		}
	}
	return rec
}

// LossDiag classifies one loss.
type LossDiag struct {
	Cause          string
	ViableAtFatal  int
	Horizon        int // turns since the last decision with >= 2 viable options; -1 if never
	LastChoiceBest float64
	Board          string
}

func classifyLoss(trace []decisionRec, cause string, last *api.GameState) *LossDiag {
	d := &LossDiag{Cause: cause, Horizon: -1, LastChoiceBest: -9}
	if len(trace) == 0 {
		return d
	}
	fatal := trace[len(trace)-1]
	d.ViableAtFatal = fatal.Viable
	for k := len(trace) - 1; k >= 0; k-- {
		if trace[k].Viable >= 2 {
			d.Horizon = fatal.Turn - trace[k].Turn
			d.LastChoiceBest = trace[k].Best
			break
		}
	}
	if last != nil {
		d.Board = fixture.Format(last)
	}
	return d
}

func horizonBucket(h int) string {
	switch {
	case h < 0:
		return "never"
	case h == 0:
		return "0"
	case h <= 3:
		return "1-3"
	case h <= 10:
		return "4-10"
	}
	return ">10"
}

// Diagnosis aggregates the loss classifications of a run.
type Diagnosis struct {
	Losses               int                       `json:"losses"`
	FatalMoveAvoidable   int                       `json:"fatalMoveAvoidable"`
	EvaluatorSawItComing int                       `json:"evaluatorSawLossAtLastChoice"`
	Horizon              map[string]int            `json:"lastRealChoiceTurnsBeforeDeath"`
	HorizonByCause       map[string]map[string]int `json:"horizonByCause"`
}

func diagnose(rs []GameResult) *Diagnosis {
	dg := &Diagnosis{Horizon: map[string]int{}, HorizonByCause: map[string]map[string]int{}}
	for _, r := range rs {
		l := r.Diag
		if l == nil {
			continue
		}
		dg.Losses++
		if l.ViableAtFatal > 0 {
			dg.FatalMoveAvoidable++
		}
		if l.Horizon >= 0 && l.LastChoiceBest < -1.5 {
			dg.EvaluatorSawItComing++
		}
		b := horizonBucket(l.Horizon)
		dg.Horizon[b]++
		if dg.HorizonByCause[l.Cause] == nil {
			dg.HorizonByCause[l.Cause] = map[string]int{}
		}
		dg.HorizonByCause[l.Cause][b]++
	}
	return dg
}
