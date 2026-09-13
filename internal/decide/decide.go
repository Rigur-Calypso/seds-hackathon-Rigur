// Package decide is the single entry point for choosing a move. Both the HTTP
// server and the arena call Engine.Decide; decision logic is never duplicated.
package decide

import (
	"context"
	"fmt"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fallback"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/stage"
)

// Reason says which path produced the move.
type Reason string

const (
	ReasonNoYou     Reason = "no_you"
	ReasonNoLegal   Reason = "no_legal"
	ReasonForced    Reason = "forced"
	ReasonFallback  Reason = "fallback"
	ReasonEvaluated Reason = "tvae"
	ReasonDuel      Reason = "duel"
	ReasonPanic     Reason = "panic"
)

// Score is one candidate's aggregate value.
type Score struct {
	Move  string  `json:"m"`
	Value float64 `json:"v"`
}

// Decision is the move plus everything the structured log needs.
type Decision struct {
	Move    string
	Reason  Reason
	Stage   stage.Stage
	Profile string
	Scores  []Score
	Depth   int
	Err     string
}

// Evaluator picks among safe moves. It must honour ctx cooperatively.
type Evaluator func(ctx context.Context, s *board.State, p *config.Params, safe []board.Dir) (board.Dir, Decision, error)

// Engine holds immutable configuration; safe for concurrent use.
type Engine struct {
	Profiles *config.Profiles
	Eval     Evaluator
}

// New builds an engine. A nil evaluator means fallback-only.
func New(p *config.Profiles, eval Evaluator) *Engine {
	if p == nil {
		p = config.DefaultProfiles()
	}
	return &Engine{Profiles: p, Eval: eval}
}

// Decide never panics and always returns one of the four move strings. The
// fallback move is computed before any evaluation and survives every failure:
// parse gaps, deadline, evaluator error, or panic.
func (e *Engine) Decide(ctx context.Context, gs *api.GameState) (d Decision) {
	d.Move = board.Up.String()
	d.Reason = ReasonNoYou
	defer func() {
		if r := recover(); r != nil {
			d.Reason = ReasonPanic
			d.Err = fmt.Sprint(r)
		}
	}()
	if gs == nil {
		return d
	}
	st := stage.Classify(gs)
	d.Stage = st
	p := e.Profiles.Get(st.Profile())
	d.Profile = p.Name

	s, ok := board.FromAPI(gs)
	if !ok {
		return d
	}
	fb := fallback.Best(s) // ALWAYS first
	d.Move, d.Reason = fb.String(), ReasonFallback

	safe := legal.Safe(s, 0)
	switch len(safe) {
	case 0:
		d.Reason = ReasonNoLegal
		return d
	case 1:
		d.Move, d.Reason = safe[0].String(), ReasonForced
		return d
	}
	if e.Eval == nil {
		return d
	}
	// Put the fallback's choice first so exact ties break toward it.
	for i, m := range safe {
		if m == fb {
			safe[0], safe[i] = safe[i], safe[0]
			break
		}
	}
	// The evaluator owns deadline handling: it returns an error only if it could
	// not produce a complete answer. A result that completed just as the deadline
	// passed (e.g. TVAE done, duel search cut short) is still the better move.
	move, info, err := e.Eval(ctx, s, p, safe)
	if err != nil {
		d.Err = err.Error()
		d.Scores, d.Depth = info.Scores, info.Depth
		return d
	}
	d.Move = move.String()
	d.Reason = info.Reason
	if d.Reason == "" {
		d.Reason = ReasonEvaluated
	}
	d.Scores, d.Depth = info.Scores, info.Depth
	return d
}
