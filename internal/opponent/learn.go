package opponent

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
)

// Ensemble policies, in mixture order.
const (
	polSpace = iota
	polFood
	polAggro
	polUniform
	numPolicies
)

// Model is the in-game opponent learner for one (game, our snake) pair (P2): a
// posterior over the ensemble policies for each opponent, updated from the moves
// the opponent actually made. Advisory only: it reweights actions and never
// removes one (CLAUDE.md §6), and the uniform policy keeps its profile floor.
type Model struct {
	mu      sync.Mutex
	created time.Time
	turn    int // last turn observed; a lower turn resets the model
	snakes  map[string]*snakeModel
}

type snakeModel struct {
	post [numPolicies]float64
	obs  int
	slow bool // last reported latency near game.timeout (R11)

	// What the ensemble predicted on recTurn, for the next update.
	recTurn int
	recHead board.Point
	recDirs []board.Dir
	recDist [numPolicies][]float64
}

func newModel() *Model {
	return &Model{created: time.Now(), turn: -1, snakes: map[string]*snakeModel{}}
}

// prior is the profile mixture normalised to sum 1 (uniform over policies if empty).
func prior(p *config.Params) [numPolicies]float64 {
	a := [numPolicies]float64{p.EnsSpace, p.EnsFood, p.EnsAggro, p.EnsUniform}
	sum := 0.0
	for _, x := range a {
		sum += x
	}
	for i := range a {
		if sum > 0 {
			a[i] /= sum
		} else {
			a[i] = 1 / float64(numPolicies)
		}
	}
	return a
}

// Observe folds this turn's board into the model: for each opponent whose
// predicted distributions were recorded last turn, infer the move it made from
// its body (Body[1] is last turn's head) and update its policy posterior.
// Keyed by snake id, which is game-scoped (R11) — the model itself is per game.
func (m *Model) Observe(s *board.State, p *config.Params) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.Turn < m.turn {
		m.snakes = map[string]*snakeModel{} // a new game reusing the id
	}
	m.turn = s.Turn
	for j := 1; j < len(s.Snakes); j++ {
		sn := &s.Snakes[j]
		if len(sn.Body) == 0 || sn.ID == "" {
			continue
		}
		sm := m.snakes[sn.ID]
		if sm == nil {
			sm = &snakeModel{post: prior(p), recTurn: -1}
			m.snakes[sn.ID] = sm
		}
		// R11: latency is the opponent's measured response time last turn. A snake
		// near the limit may time out, and the engine then repeats its last move.
		sm.slow = s.Rules.TimeoutMs > 0 && p.LearnSlowFrac > 0 &&
			float64(sn.Latency) >= p.LearnSlowFrac*float64(s.Rules.TimeoutMs)
		if sm.recTurn != s.Turn-1 || len(sm.recDirs) < 2 || len(sn.Body) < 2 || sn.Body[1] != sm.recHead {
			continue
		}
		k := -1
		for i, d := range sm.recDirs {
			if q, ok := s.Step(sm.recHead, d); ok && q == sn.Body[0] {
				k = i
				break
			}
		}
		if k >= 0 {
			sm.update(k, p)
		}
	}
}

// update applies w_i ← w_i·(ε + P_i(actual)), normalised, moving the posterior
// only LearnMaxStep of the way per turn so one odd move cannot flip it.
func (sm *snakeModel) update(k int, p *config.Params) {
	var upd [numPolicies]float64
	sum := 0.0
	for i := range upd {
		pk := 0.0
		if k < len(sm.recDist[i]) {
			pk = sm.recDist[i][k]
		}
		upd[i] = sm.post[i] * (p.LearnEps + pk)
		sum += upd[i]
	}
	if sum <= 0 {
		return
	}
	for i := range upd {
		sm.post[i] += p.LearnMaxStep * (upd[i]/sum - sm.post[i])
	}
	sm.obs++
}

// mixture returns an opponent's policy weights: the profile prior, moved toward
// the posterior as observations accumulate (λ = min(1, obs/LearnMinObs)), with
// the uniform policy never below its prior so every legal threat keeps weight.
func (m *Model) mixture(id string, p *config.Params) (mix [numPolicies]float64, slow bool) {
	pr := prior(p)
	m.mu.Lock()
	defer m.mu.Unlock()
	sm := m.snakes[id]
	if sm == nil {
		return pr, false
	}
	lam := 1.0
	if p.LearnMinObs > 0 {
		lam = math.Min(1, float64(sm.obs)/float64(p.LearnMinObs))
	}
	for i := range mix {
		mix[i] = (1-lam)*pr[i] + lam*sm.post[i]
	}
	if mix[polUniform] < pr[polUniform] {
		mix[polUniform] = pr[polUniform]
	}
	return mix, sm.slow
}

// record stores the per-policy distributions predicted this turn. Only the
// first call per turn counts, so a re-evaluation cannot overwrite them.
func (m *Model) record(id string, turn int, head board.Point, dirs []board.Dir, dist [numPolicies][]float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sm := m.snakes[id]
	if sm == nil || sm.recTurn == turn {
		return
	}
	sm.recTurn, sm.recHead = turn, head
	sm.recDirs = append(sm.recDirs[:0], dirs...)
	sm.recDist = dist
}

// Models is the learner store: keyed by game.id and our snake id, mutex
// guarded, deleted on /end and pruned by age (the arena never sends /end).
type Models struct {
	mu sync.Mutex
	m  map[string]*Model
}

// NewModels returns an empty store.
func NewModels() *Models { return &Models{m: map[string]*Model{}} }

func modelKey(gameID, youID string) string { return gameID + "\x00" + youID }

// Get returns the model for a game and our snake, creating it if missing.
func (ms *Models) Get(gameID, youID string) *Model {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	k := modelKey(gameID, youID)
	if m, ok := ms.m[k]; ok {
		return m
	}
	now := time.Now()
	for key, m := range ms.m {
		if now.Sub(m.created) > MaxGameAge {
			delete(ms.m, key)
		}
	}
	m := newModel()
	ms.m[k] = m
	return m
}

// End deletes every model of a game.
func (ms *Models) End(gameID string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	prefix := gameID + "\x00"
	for k := range ms.m {
		if strings.HasPrefix(k, prefix) {
			delete(ms.m, k)
		}
	}
}

// Len counts stored models.
func (ms *Models) Len() int {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return len(ms.m)
}

type ctxKey struct{}

// WithModel attaches the learner to a decision's context.
func WithModel(ctx context.Context, m *Model) context.Context {
	return context.WithValue(ctx, ctxKey{}, m)
}

// ModelFrom returns the learner in ctx, or nil (fixed ensemble).
func ModelFrom(ctx context.Context) *Model {
	m, _ := ctx.Value(ctxKey{}).(*Model)
	return m
}
