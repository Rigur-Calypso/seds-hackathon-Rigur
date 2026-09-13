// Package opponent keeps per-game runtime state (keyed by game.id, mutex
// guarded, deleted in /end) and advisory cross-game opponent profiles keyed by
// snake NAME (R11: ids are game-scoped).
package opponent

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
)

// MaxGameAge prunes games whose /end never arrived.
const MaxGameAge = 2 * time.Hour

// Game is one game's runtime state.
type Game struct {
	mu            sync.Mutex
	ID            string
	Created       time.Time
	LastTurn      int
	LastElapsedMs int
	overheadMs    float64
	overheadN     int
	LastBoard     string // fixture DSL of the last /move we answered
	LastMove      string
	MaxElapsedMs  int
	Moves         int
}

// Observe folds the engine-measured latency of our previous response (R11
// applies to "you" too) into a network/queue overhead estimate: what the
// engine saw minus what we spent computing.
func (g *Game) Observe(youLatencyMs int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if youLatencyMs <= 0 || g.LastElapsedMs <= 0 {
		return
	}
	over := float64(youLatencyMs - g.LastElapsedMs)
	if over < 0 {
		over = 0
	}
	if g.overheadN == 0 {
		g.overheadMs = over
	} else {
		g.overheadMs = 0.7*g.overheadMs + 0.3*over
	}
	g.overheadN++
}

// OverheadMs is the current overhead estimate (0 until observed).
func (g *Game) OverheadMs() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return int(g.overheadMs + 0.5)
}

// Record stores what we just answered.
func (g *Game) Record(turn int, elapsed time.Duration, move, boardDSL string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ms := int(elapsed.Milliseconds())
	if ms < 1 {
		ms = 1
	}
	g.LastTurn, g.LastElapsedMs, g.LastMove, g.LastBoard = turn, ms, move, boardDSL
	if ms > g.MaxElapsedMs {
		g.MaxElapsedMs = ms
	}
	g.Moves++
}

// Snapshot returns a copy of the loggable fields.
func (g *Game) Snapshot() (lastTurn, moves, maxMs int, lastBoard, lastMove string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.LastTurn, g.Moves, g.MaxElapsedMs, g.LastBoard, g.LastMove
}

// Profile is an advisory cross-game record for one opponent name.
type Profile struct {
	Name         string `json:"name"`
	Games        int    `json:"games"`
	Samples      int    `json:"samples"`
	LatencySumMs int    `json:"latencySumMs"`
	MaxLatencyMs int    `json:"maxLatencyMs"`
}

// MeanLatencyMs is the average observed latency.
func (p *Profile) MeanLatencyMs() int {
	if p.Samples == 0 {
		return 0
	}
	return p.LatencySumMs / p.Samples
}

// Registry holds all games and profiles.
type Registry struct {
	mu       sync.Mutex
	games    map[string]*Game
	profiles map[string]*Profile
	path     string
}

// NewRegistry loads profiles from path if it exists ("" disables persistence).
func NewRegistry(path string) *Registry {
	r := &Registry{games: map[string]*Game{}, profiles: map[string]*Profile{}, path: path}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			var list []*Profile
			if json.Unmarshal(b, &list) == nil {
				for _, p := range list {
					if p != nil && p.Name != "" {
						r.profiles[p.Name] = p
					}
				}
			}
		}
	}
	return r
}

// Get returns the game, creating it if missing (a restart mid-game must work).
func (r *Registry) Get(id string) *Game {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.games[id]; ok {
		return g
	}
	now := time.Now()
	for k, g := range r.games {
		if now.Sub(g.Created) > MaxGameAge {
			delete(r.games, k)
		}
	}
	g := &Game{ID: id, Created: now}
	r.games[id] = g
	return g
}

// End removes and returns the game (nil if unknown).
func (r *Registry) End(id string) *Game {
	r.mu.Lock()
	defer r.mu.Unlock()
	g := r.games[id]
	delete(r.games, id)
	return g
}

// Active counts live games.
func (r *Registry) Active() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.games)
}

// ObserveOpponents records opponent latencies by name. newGame bumps Games.
func (r *Registry) ObserveOpponents(gs *api.GameState, newGame bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, sn := range gs.Board.Snakes {
		if sn.ID == gs.You.ID || sn.Name == "" {
			continue
		}
		p := r.profiles[sn.Name]
		if p == nil {
			p = &Profile{Name: sn.Name}
			r.profiles[sn.Name] = p
		}
		if newGame {
			p.Games++
		}
		if l := int(sn.Latency); l > 0 {
			p.Samples++
			p.LatencySumMs += l
			if l > p.MaxLatencyMs {
				p.MaxLatencyMs = l
			}
		}
	}
}

// Profile returns a copy of a profile.
func (r *Registry) Profile(name string) (Profile, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.profiles[name]
	if !ok {
		return Profile{}, false
	}
	return *p, true
}

// Save persists profiles (best effort).
func (r *Registry) Save() error {
	if r.path == "" {
		return nil
	}
	r.mu.Lock()
	list := make([]*Profile, 0, len(r.profiles))
	for _, p := range r.profiles {
		c := *p
		list = append(list, &c)
	}
	r.mu.Unlock()
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}
