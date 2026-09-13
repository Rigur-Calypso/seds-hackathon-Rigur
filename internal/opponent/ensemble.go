package opponent

import (
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
)

// Choice is one opponent's action set in the envelope. Every safe action is
// kept; W only weights them (CLAUDE.md §6: never prune a legal threat).
type Choice struct {
	Idx  int
	Dirs []board.Dir
	W    []float64
	Dist int // distance from our head
	// Near[k]: Dirs[k] lands within PruneKeepRadius of our head, so it can reach
	// or block one of our next cells (threat-preserving pruning keeps it).
	Near []bool
}

// Choices lists each live opponent's safe actions with ensemble weights from
// four cheap policies: survival-space, food-seeking, aggression, uniform.
// A non-nil model (P2) personalises the mixture per opponent; nil keeps the
// profile mixture exactly.
func Choices(s *board.State, p *config.Params, m *Model) []Choice {
	blocked := legal.Blocked(s)
	food := legal.FoodDistances(s, blocked)
	me := &s.Snakes[0]
	out := make([]Choice, 0, len(s.Snakes))
	for j := 1; j < len(s.Snakes); j++ {
		sn := &s.Snakes[j]
		if !sn.Alive() || len(sn.Body) == 0 {
			continue
		}
		dirs := legal.SafeWith(s, blocked, j)
		if len(dirs) == 0 {
			dirs = []board.Dir{legal.DefaultMove(sn)} // doomed; one move suffices
		}
		near := make([]bool, len(dirs))
		for k, d := range dirs {
			if q, ok := s.Step(sn.Head(), d); ok {
				// Within 2 of our head covers every cell adjacent to one of our next cells.
				near[k] = s.Dist(q, me.Head()) <= p.PruneKeepRadius
			}
		}
		out = append(out, Choice{
			Idx:  j,
			Dirs: dirs,
			W:    weights(s, blocked, food, j, dirs, me, p, m),
			Dist: s.Dist(sn.Head(), me.Head()),
			Near: near,
		})
	}
	return out
}

func normalize(v []float64) {
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	if sum <= 0 {
		for i := range v {
			v[i] = 1 / float64(len(v))
		}
		return
	}
	for i := range v {
		v[i] /= sum
	}
}

func weights(s *board.State, blocked []bool, food []int32, j int, dirs []board.Dir, me *board.Snake, p *config.Params, m *Model) []float64 {
	n := len(dirs)
	w := make([]float64, n)
	if n == 1 {
		w[0] = 1
		return w
	}
	sn := &s.Snakes[j]
	space, hungry, aggro := make([]float64, n), make([]float64, n), make([]float64, n)
	for k, d := range dirs {
		q, _ := s.Step(sn.Head(), d)
		space[k] = float64(legal.FloodArea(s, blocked, q, 2*sn.Len()+1)) + 1
		if f := food[s.Idx(q)]; f >= 0 {
			hungry[k] = 1 / (1 + float64(f))
		} else {
			hungry[k] = 0.01
		}
		if sn.Len() > me.Len() {
			aggro[k] = 1 / (1 + float64(s.Dist(q, me.Head())))
		} else {
			aggro[k] = 1
		}
	}
	normalize(space)
	normalize(hungry)
	normalize(aggro)
	a := [numPolicies]float64{p.EnsSpace, p.EnsFood, p.EnsAggro, p.EnsUniform}
	slow := false
	if m != nil {
		a, slow = m.mixture(sn.ID, p)
	}
	straight := legal.DefaultMove(sn)
	for k := range w {
		w[k] = a[polSpace]*space[k] + a[polFood]*hungry[k] + a[polAggro]*aggro[k] + a[polUniform]/float64(n)
		if slow && dirs[k] == straight {
			w[k] += p.LearnSlowWeight // likely timeout: the engine repeats its last move
		}
	}
	normalize(w)
	if m != nil {
		uniform := make([]float64, n)
		for k := range uniform {
			uniform[k] = 1 / float64(n)
		}
		m.record(sn.ID, s.Turn, sn.Head(), dirs, [numPolicies][]float64{space, hungry, aggro, uniform})
	}
	return w
}
