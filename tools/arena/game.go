package main

import (
	"context"
	"fmt"
	"sort"
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

// enginePolicy is our decide.Engine. budget 0 = deterministic (no deadline;
// search depth capped by the profile), so paired runs are reproducible.
type enginePolicy struct {
	eng    *decide.Engine
	budget time.Duration
}

func (e enginePolicy) Move(gs *api.GameState) string {
	ctx := context.Background()
	if e.budget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.budget)
		defer cancel()
	}
	return e.eng.Decide(ctx, gs).Move
}

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
}

// Settings used for every arena game (engine CLI defaults).
var gameParams = map[string]string{
	rules.ParamFoodSpawnChance:     "15",
	rules.ParamMinimumFood:         "1",
	rules.ParamHazardDamagePerTurn: "14",
	rules.ParamShrinkEveryNTurns:   "25",
}

var pointsTable = []float64{10, 6, 3, 1}

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
	rs := rules.NewRulesetBuilder().WithSeed(seed).WithParams(gameParams).WithSolo(n < 2).NamedRuleset(cfg.Rules)
	gm, err := maps.GetMap("standard")
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
			start := time.Now()
			mv := seats[i].Policy.Move(gs)
			if i == 0 {
				el := time.Since(start)
				res.Decisions = append(res.Decisions, el)
				if timeout > 0 && el > timeout {
					res.Timeouts++
				}
				lastYou = gs
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

	// Placement: later elimination is better; survivors rank first (by length
	// if the turn cap was hit); ties share the average of their points.
	type rank struct {
		i         int
		elim, len int
	}
	ranks := make([]rank, n)
	for i := range bs.Snakes {
		sn := &bs.Snakes[i]
		e := sn.EliminatedOnTurn
		if sn.EliminatedCause == rules.NotEliminated {
			e = 1 << 30
		}
		ranks[i] = rank{i, e, len(sn.Body)}
		if sn.EliminatedBy == "s0" && i != 0 {
			res.Kills++
		}
	}
	sort.SliceStable(ranks, func(a, b int) bool {
		if ranks[a].elim != ranks[b].elim {
			return ranks[a].elim > ranks[b].elim
		}
		return ranks[a].elim == 1<<30 && ranks[a].len > ranks[b].len
	})
	for pos := 0; pos < n; {
		end := pos + 1
		for end < n && ranks[end].elim == ranks[pos].elim && (ranks[pos].elim != 1<<30 || ranks[end].len == ranks[pos].len) {
			end++
		}
		pts, place := 0.0, 0.0
		for k := pos; k < end; k++ {
			if k < len(pointsTable) {
				pts += pointsTable[k]
			}
			place += float64(k + 1)
		}
		for k := pos; k < end; k++ {
			if ranks[k].i == 0 {
				res.Points = pts / float64(end-pos)
				res.Place = place / float64(end-pos)
				res.Won = pos == 0 && end-pos == 1
			}
		}
		pos = end
	}
	me := &bs.Snakes[0]
	if me.EliminatedCause == rules.NotEliminated {
		res.Survived = bs.Turn
	} else {
		res.Survived = me.EliminatedOnTurn
		res.Cause = me.EliminatedCause
		if cfg.DumpLosses && lastYou != nil {
			res.FatalBoard = fixture.Format(lastYou)
		}
	}
	return res, nil
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

// toAPI builds the request the engine would send to snake i.
func toAPI(cfg *Config, gameID string, bs *rules.BoardState, you int) *api.GameState {
	gs := &api.GameState{Turn: bs.Turn}
	gs.Game = api.Game{ID: gameID, Map: "standard", Timeout: cfg.TimeoutMs, Ruleset: api.Ruleset{
		Name: cfg.Rules, Version: "arena",
		Settings: api.RulesetSettings{FoodSpawnChance: 15, MinimumFood: 1, HazardDamagePerTurn: 14,
			Royale: api.RoyaleSettings{ShrinkEveryNTurns: 25}},
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
	gs.Game.Ruleset.Name = strings.ToLower(gs.Game.Ruleset.Name)
	return gs
}
