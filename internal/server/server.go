// Package server is the HTTP layer: four endpoints, recover() middleware, a
// request-derived cooperative deadline, and structured logs. It never returns
// a 5xx and /move always answers one of the four move strings.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/decide"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/opponent"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/stage"
)

const maxBody = 1 << 20

// Server wires the engine to HTTP.
type Server struct {
	eng      *decide.Engine
	log      *slog.Logger
	games    *opponent.Registry
	inflight atomic.Int32
	info     api.InfoResponse
}

// New builds a server.
func New(eng *decide.Engine, log *slog.Logger, version string, reg *opponent.Registry) *Server {
	if reg == nil {
		reg = opponent.NewRegistry("")
	}
	return &Server{
		eng:   eng,
		log:   log,
		games: reg,
		info: api.InfoResponse{
			APIVersion: "1",
			Author:     "Rigur-Calypso",
			Color:      "#19d3a2",
			Head:       "evil",
			Tail:       "bolt",
			Version:    version,
		},
	}
}

// Handler returns the routed, panic-proof handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleInfo)
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/move", s.handleMove)
	mux.HandleFunc("/end", s.handleEnd)
	return s.recoverer(mux)
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "path", r.URL.Path, "err", fmt.Sprint(rec), "stack", string(debug.Stack()))
				if r.URL.Path == "/move" {
					writeJSON(w, api.MoveResponse{Move: "up"})
				} else {
					writeJSON(w, map[string]string{})
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, s.info)
}

func readBody(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(r.Body, maxBody))
	return b
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	gs, err := api.Parse(readBody(r))
	if err != nil {
		s.log.Warn("start_parse", "err", err.Error())
		writeJSON(w, map[string]string{})
		return
	}
	s.games.Get(gs.Game.ID)
	s.games.ObserveOpponents(gs, true)
	set := gs.Game.Ruleset.Settings
	// R10: log every parsed setting once per game.
	s.log.Info("start",
		"game", gs.Game.ID, "ruleset", gs.Game.Ruleset.Name, "map", gs.Game.Map,
		"stage", stage.Classify(gs).String(), "timeout", gs.Game.Timeout,
		"width", gs.Board.Width, "height", gs.Board.Height, "snakes", len(gs.Board.Snakes),
		"hazardDamagePerTurn", set.HazardDamagePerTurn, "shrinkEveryNTurns", set.Royale.ShrinkEveryNTurns,
		"foodSpawnChance", set.FoodSpawnChance, "minimumFood", set.MinimumFood, "source", gs.Game.Source)
	writeJSON(w, map[string]string{})
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	n := int(s.inflight.Add(1))
	defer s.inflight.Add(-1)

	gs, err := api.Parse(readBody(r))
	if err != nil {
		s.log.Warn("move_parse", "err", err.Error())
		writeJSON(w, api.MoveResponse{Move: "up"})
		return
	}
	g := s.games.Get(gs.Game.ID)
	g.Observe(int(gs.You.Latency))
	st := stage.Classify(gs)
	p := s.eng.Profiles.Get(st.Profile())
	budget := Budget(gs.Game.Timeout, p, n, g.OverheadMs())

	ctx, cancel := context.WithTimeout(r.Context(), budget)
	d := s.eng.Decide(ctx, gs)
	cancel()

	elapsed := time.Since(start)
	// Observability without touching the JSON body: clients ignore unknown
	// headers, and `curl -D -` against the live URL shows what the snake did.
	w.Header().Set("X-Snake-Decision", fmt.Sprintf("reason=%s depth=%d budget_ms=%d compute_ms=%d inflight=%d",
		d.Reason, d.Depth, budget.Milliseconds(), elapsed.Milliseconds(), n))
	writeJSON(w, api.MoveResponse{Move: d.Move})
	g.Record(gs.Turn, elapsed, d.Move, fixture.Format(gs))
	if gs.Turn%10 == 0 {
		s.games.ObserveOpponents(gs, false)
	}

	level := slog.LevelInfo
	if d.Reason == decide.ReasonPanic || elapsed > budget+50*time.Millisecond {
		level = slog.LevelWarn
	}
	s.log.Log(context.Background(), level, "move",
		"game", gs.Game.ID, "turn", gs.Turn, "stage", st.String(), "profile", d.Profile,
		"move", d.Move, "reason", string(d.Reason), "depth", d.Depth, "scores", d.Scores,
		"elapsed_ms", elapsed.Milliseconds(), "budget_ms", budget.Milliseconds(),
		"inflight", n, "overhead_ms", g.OverheadMs(), "err", d.Err)
}

func (s *Server) handleEnd(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{})
	gs, err := api.Parse(readBody(r))
	if err != nil {
		s.log.Warn("end_parse", "err", err.Error())
		return
	}
	g := s.games.End(gs.Game.ID)
	s.eng.EndGame(gs.Game.ID) // P2: drop this game's opponent learners
	alive := false
	for _, sn := range gs.Board.Snakes {
		if sn.ID == gs.You.ID {
			alive = true
		}
	}
	result := "loss"
	switch {
	case alive && len(gs.Board.Snakes) == 1:
		result = "win"
	case alive:
		result = "alive_at_end"
	case len(gs.Board.Snakes) == 0:
		result = "draw"
	}
	attrs := []any{"game", gs.Game.ID, "turn", gs.Turn, "result", result,
		"stage", stage.Classify(gs).String(), "survivors", len(gs.Board.Snakes)}
	if g != nil {
		lastTurn, moves, maxMs, lastBoard, lastMove := g.Snapshot()
		attrs = append(attrs, "moves", moves, "max_elapsed_ms", maxMs)
		if result != "win" && lastBoard != "" {
			// Fatal board in fixture DSL: paste into testdata/fixtures, add the
			// correct "expect" line, never delete it.
			attrs = append(attrs, "fatal_turn", lastTurn, "fatal_move", lastMove, "fatal_board", lastBoard)
		}
	}
	s.log.Info("end", attrs...)
	_ = s.games.Save()
}

// Warmup runs one decision so the first real /move pays no first-use cost.
func (s *Server) Warmup() {
	gs := fixture.MustState("you 100 5,5 5,4 5,3\nsnake a 100 1,1 1,2 1,3\nsnake b 100 9,9 9,8 9,7\nfood 3,3")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	s.eng.Decide(ctx, gs)
}
