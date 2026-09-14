package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BattlesnakeOfficial/rules"
	"github.com/BattlesnakeOfficial/rules/maps"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/decide"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
)

// Policy chooses a move for gs.You.
type Policy interface {
	Move(gs *api.GameState) string
}

// deciding is implemented by policies that can report a full decision (our
// engine), which the diagnostics use.
type deciding interface {
	Decide(gs *api.GameState) decide.Decision
}

// enginePolicy is our decide.Engine. budget 0 = deterministic (no deadline;
// search depth capped by the profile), so paired runs are reproducible.
type enginePolicy struct {
	eng    *decide.Engine
	budget time.Duration
}

func (e enginePolicy) Decide(gs *api.GameState) decide.Decision {
	ctx := context.Background()
	if e.budget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.budget)
		defer cancel()
	}
	return e.eng.Decide(ctx, gs)
}

func (e enginePolicy) Move(gs *api.GameState) string { return e.Decide(gs).Move }

// Seat is one snake in a game.
type Seat struct {
	Name   string
	Policy Policy
}

// GameResult is everything recorded about one game, from seat 0's view.
type GameResult struct {
	Seed       int64
	Turns      int
	Points     float64
	Place      float64
	Won        bool
	Survived   int
	Cause      string
	Opponents  []string
	Decisions  []time.Duration
	Timeouts   int
	Kills      int
	FatalBoard string
	Diag       *LossDiag
}

var pointsTable = []float64{10, 6, 3, 1}

const survived = 1 << 30

// engineParams are the official ruleset settings for one game, taken from the
// run configuration rather than assumed.
func engineParams(cfg *Config) map[string]string {
	return map[string]string{
		rules.ParamFoodSpawnChance:     strconv.Itoa(cfg.FoodSpawnChance),
		rules.ParamMinimumFood:         strconv.Itoa(cfg.MinimumFood),
		rules.ParamHazardDamagePerTurn: strconv.Itoa(cfg.HazardDamage),
		rules.ParamShrinkEveryNTurns:   strconv.Itoa(cfg.ShrinkEveryN),
	}
}

// playGame runs one full game with the official rules and map, in-process.
func playGame(cfg *Config, seed int64, seats []Seat) (GameResult, error) {
	res := GameResult{Seed: seed}
	n := len(seats)
	ids := make([]string, n)
	for i := range seats {
		ids[i] = fmt.Sprintf("s%d", i)
		if i > 0 {
			res.Opponents = append(res.Opponents, seats[i].Name)
		}
	}
	rs := rules.NewRulesetBuilder().WithSeed(seed).WithParams(engineParams(cfg)).WithSolo(n < 2).NamedRuleset(cfg.Rules)
	gm, err := maps.GetMap(cfg.mapID())
	if err != nil {
		return res, err
	}
	bs, err := maps.SetupBoard(gm.ID(), rs.Settings(), cfg.Width, cfg.Height, ids)
	if err != nil {
		return res, err
	}
	_, bs, err = rs.Execute(bs, nil)
	if err != nil {
		return res, err
	}
	gameID := fmt.Sprintf("arena-%s-%d", cfg.Rules, seed)
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	var lastYou *api.GameState
	var trace []decisionRec

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		if alive(bs) == 0 || (n > 1 && alive(bs) <= 1) {
			break
		}
		if bs, err = maps.PreUpdateBoard(gm, bs, rs.Settings()); err != nil {
			return res, err
		}
		moves := make([]rules.SnakeMove, 0, n)
		for i := range bs.Snakes {
			sn := &bs.Snakes[i]
			if sn.EliminatedCause != rules.NotEliminated {
				continue
			}
			gs := toAPI(cfg, gameID, bs, i)
			var mv string
			if i == 0 {
				dp, canDiagnose := seats[0].Policy.(deciding)
				tracing := canDiagnose && cfg.TraceSeed != 0 && seed == cfg.TraceSeed
				start := time.Now()
				var d decide.Decision
				if (cfg.Diagnose || tracing) && canDiagnose {
					d = dp.Decide(gs)
					mv = d.Move
				} else {
					mv = seats[0].Policy.Move(gs)
				}
				el := time.Since(start)
				res.Decisions = append(res.Decisions, el)
				if timeout > 0 && el > timeout {
					res.Timeouts++
				}
				if cfg.Diagnose && canDiagnose {
					trace = append(trace, recordDecision(gs, d))
				}
				if tracing {
					traceLine(gs, d)
				}
				lastYou = gs
			} else {
				mv = seats[i].Policy.Move(gs)
			}
			moves = append(moves, rules.SnakeMove{ID: sn.ID, Move: mv})
		}
		over, next, err := rs.Execute(bs, moves)
		if err != nil {
			return res, err
		}
		if over {
			break
		}
		if bs, err = maps.PostUpdateBoard(gm, next, rs.Settings()); err != nil {
			return res, err
		}
		bs.Turn++
	}
	res.Turns = bs.Turn

	for i := range bs.Snakes {
		if bs.Snakes[i].EliminatedBy == "s0" && i != 0 {
			res.Kills++
		}
	}
	pl := placements(bs, cfg.TieBreak)
	res.Points, res.Place, res.Won = pl[0].Points, pl[0].Place, pl[0].Won

	me := &bs.Snakes[0]
	if me.EliminatedCause == rules.NotEliminated {
		res.Survived = bs.Turn
	} else {
		res.Survived = me.EliminatedOnTurn
		res.Cause = me.EliminatedCause
		if cfg.DumpLosses && lastYou != nil {
			res.FatalBoard = fixture.Format(lastYou)
		}
		if cfg.Diagnose {
			res.Diag = classifyLoss(trace, me.EliminatedCause, lastYou)
		}
	}
	return res, nil
}

// placement is one snake's tournament result.
type placement struct {
	Points float64
	Place  float64
	Won    bool
}

// placements ranks every snake: a later elimination ranks higher; snakes still
// alive at the turn cap are ordered by length ("length") or all tie ("draw");
// tied snakes share the average of their points and places (10/6/3/1).
func placements(bs *rules.BoardState, tieBreak string) []placement {
	n := len(bs.Snakes)
	type rank struct{ i, elim, length int }
	ranks := make([]rank, n)
	for i := range bs.Snakes {
		sn := &bs.Snakes[i]
		e := sn.EliminatedOnTurn
		if sn.EliminatedCause == rules.NotEliminated {
			e = survived
		}
		ranks[i] = rank{i, e, len(sn.Body)}
	}
	byLength := tieBreak != "draw"
	sort.SliceStable(ranks, func(a, b int) bool {
		if ranks[a].elim != ranks[b].elim {
			return ranks[a].elim > ranks[b].elim
		}
		return byLength && ranks[a].elim == survived && ranks[a].length > ranks[b].length
	})
	sameTier := func(a, b rank) bool {
		if a.elim != b.elim {
			return false
		}
		if a.elim == survived && byLength {
			return a.length == b.length
		}
		return true
	}
	out := make([]placement, n)
	for pos := 0; pos < n; {
		end := pos + 1
		for end < n && sameTier(ranks[pos], ranks[end]) {
			end++
		}
		pts, place := 0.0, 0.0
		for k := pos; k < end; k++ {
			if k < len(pointsTable) {
				pts += pointsTable[k]
			}
			place += float64(k + 1)
		}
		size := float64(end - pos)
		for k := pos; k < end; k++ {
			out[ranks[k].i] = placement{Points: pts / size, Place: place / size, Won: pos == 0 && end-pos == 1}
		}
		pos = end
	}
	return out
}

func alive(bs *rules.BoardState) int {
	c := 0
	for i := range bs.Snakes {
		if bs.Snakes[i].EliminatedCause == rules.NotEliminated {
			c++
		}
	}
	return c
}

func coords(ps []rules.Point) []api.Coord {
	out := make([]api.Coord, len(ps))
	for i, p := range ps {
		out[i] = api.Coord{X: p.X, Y: p.Y}
	}
	return out
}

// toAPI builds the request the engine would send to snake i, with the same
// settings the game is actually played under.
func toAPI(cfg *Config, gameID string, bs *rules.BoardState, you int) *api.GameState {
	gs := &api.GameState{Turn: bs.Turn}
	gs.Game = api.Game{ID: gameID, Map: cfg.mapID(), Timeout: cfg.TimeoutMs, Ruleset: api.Ruleset{
		Name: strings.ToLower(cfg.Rules), Version: "arena",
		Settings: api.RulesetSettings{
			FoodSpawnChance:     cfg.FoodSpawnChance,
			MinimumFood:         cfg.MinimumFood,
			HazardDamagePerTurn: cfg.HazardDamage,
			Royale:              api.RoyaleSettings{ShrinkEveryNTurns: cfg.ShrinkEveryN},
		},
	}}
	gs.Board.Width, gs.Board.Height = bs.Width, bs.Height
	gs.Board.Food = coords(bs.Food)
	gs.Board.Hazards = coords(bs.Hazards)
	for j := range bs.Snakes {
		sn := &bs.Snakes[j]
		if sn.EliminatedCause != rules.NotEliminated {
			continue
		}
		body := coords(sn.Body)
		a := api.Snake{ID: sn.ID, Name: sn.ID, Health: sn.Health, Body: body, Head: body[0], Length: len(body), Latency: 1}
		gs.Board.Snakes = append(gs.Board.Snakes, a)
		if j == you {
			gs.You = a
		}
	}
	return gs
}
