package main

import (
	"hash/fnv"
	"math/rand"
	"strconv"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fallback"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
)

// The opponent zoo: scripted styles so we never train only against ourselves.
// Each is deterministic given (game id, turn, snake id).

type scripted struct {
	name string
	pick func(s *board.State, rng *rand.Rand) board.Dir
}

func (b scripted) Move(gs *api.GameState) string {
	s, ok := board.FromAPI(gs)
	if !ok {
		return "up"
	}
	h := fnv.New64a()
	h.Write([]byte(gs.Game.ID + "/" + gs.You.ID + "/" + strconv.Itoa(gs.Turn)))
	rng := rand.New(rand.NewSource(int64(h.Sum64())))
	return b.pick(s, rng).String()
}

// candidates: safe and not a losing head-to-head; else safe; else on-board.
func candidates(s *board.State) ([]legal.Info, []bool) {
	blocked := legal.Blocked(s)
	info := legal.Analyze(s, blocked, 0)
	var good, safe, onboard []legal.Info
	for _, in := range info {
		if in.InBounds && !in.Neck {
			onboard = append(onboard, in)
		}
		if in.Safe() {
			safe = append(safe, in)
			if !in.H2HLose {
				good = append(good, in)
			}
		}
	}
	switch {
	case len(good) > 0:
		return good, blocked
	case len(safe) > 0:
		return safe, blocked
	case len(onboard) > 0:
		return onboard, blocked
	}
	return []legal.Info{info[board.Up]}, blocked
}

func argbest(c []legal.Info, score func(legal.Info) float64) board.Dir {
	best, bv := 0, score(c[0])
	for i := 1; i < len(c); i++ {
		if v := score(c[i]); v > bv {
			best, bv = i, v
		}
	}
	return c[best].Dir
}

func area(s *board.State, blocked []bool, in legal.Info) float64 {
	if !in.InBounds {
		return 0
	}
	return float64(legal.FloodArea(s, blocked, in.Next, s.Cells()))
}

var zoo = map[string]Policy{
	"random": scripted{"random", func(s *board.State, rng *rand.Rand) board.Dir {
		c, _ := candidates(s)
		return c[rng.Intn(len(c))].Dir
	}},
	"foodgreedy": scripted{"foodgreedy", func(s *board.State, _ *rand.Rand) board.Dir {
		c, blocked := candidates(s)
		fd := legal.FoodDistances(s, blocked)
		L := float64(s.Snakes[0].Len())
		return argbest(c, func(in legal.Info) float64 {
			a := area(s, blocked, in)
			v := -1000.0
			if in.InBounds {
				if d := fd[s.Idx(in.Next)]; d >= 0 {
					v = -float64(d)
				}
			}
			if a < L {
				v -= 500
			}
			return v + a*1e-3
		})
	}},
	"spacegreedy": scripted{"spacegreedy", func(s *board.State, _ *rand.Rand) board.Dir {
		c, blocked := candidates(s)
		fd := legal.FoodDistances(s, blocked)
		return argbest(c, func(in legal.Info) float64 {
			v := area(s, blocked, in)
			if in.InBounds && s.Snakes[0].Health < 30 {
				if d := fd[s.Idx(in.Next)]; d >= 0 {
					v -= float64(d) * 5
				}
			}
			return v
		})
	}},
	"headhunter": scripted{"headhunter", func(s *board.State, _ *rand.Rand) board.Dir {
		c, blocked := candidates(s)
		me := &s.Snakes[0]
		fd := legal.FoodDistances(s, blocked)
		return argbest(c, func(in legal.Info) float64 {
			a := area(s, blocked, in)
			v := 0.0
			if a < float64(me.Len()) {
				v -= 1000
			}
			if in.H2HWin {
				v += 50
			}
			best := -1
			for j := 1; j < len(s.Snakes); j++ {
				o := &s.Snakes[j]
				if o.Alive() && o.Len() < me.Len() {
					if d := s.Dist(in.Next, o.Head()); best < 0 || d < best {
						best = d
					}
				}
			}
			if best >= 0 {
				v -= float64(best) * 3
			} else if in.InBounds {
				if d := fd[s.Idx(in.Next)]; d >= 0 {
					v -= float64(d) * 3
				}
			}
			return v + a*1e-3
		})
	}},
	"hazardcoward": scripted{"hazardcoward", func(s *board.State, _ *rand.Rand) board.Dir {
		c, blocked := candidates(s)
		fd := legal.FoodDistances(s, blocked)
		r := board.SafeRect(s)
		cx, cy := r.Centre()
		return argbest(c, func(in legal.Info) float64 {
			v := area(s, blocked, in) * 0.1
			if in.Hazard {
				v -= 1000
			}
			v -= (abs(float64(in.Next.X)-cx) + abs(float64(in.Next.Y)-cy)) * 2
			if in.InBounds && s.Snakes[0].Health < 40 {
				if d := fd[s.Idx(in.Next)]; d >= 0 {
					v -= float64(d) * 4
				}
			}
			return v
		})
	}},
	"fallback": scripted{"fallback", func(s *board.State, _ *rand.Rand) board.Dir { return fallback.Best(s) }},
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// zooOrder is the rotation used for "--opponents zoo".
var zooOrder = []string{"foodgreedy", "spacegreedy", "headhunter", "hazardcoward", "fallback", "random"}
