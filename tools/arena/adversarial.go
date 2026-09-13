package main

import (
	"math/rand"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
)

// Adversarial zoo (Codex review): scripted styles aimed at the failure modes
// the diagnostics found — sandwiches, food traps, being pushed into walls and
// being cut off by the storm. They play as snake 0 of their own view and never
// take a move into a certain death when a safe one exists (candidates()).

func init() {
	zoo["pincer"] = scripted{"pincer", pincer}
	zoo["foodbait"] = scripted{"foodbait", foodBait}
	zoo["edgeherder"] = scripted{"edgeherder", edgeHerder}
	zoo["stormtrapper"] = scripted{"stormtrapper", stormTrapper}
}

// adversarialOrder is the rotation used for "--opponents adversarial". It is a
// separate set so the original zoo baselines stay comparable.
var adversarialOrder = []string{"pincer", "foodbait", "edgeherder", "stormtrapper"}

// nearestOther is the closest live snake to this policy's head.
func nearestOther(s *board.State) int {
	best, bd := -1, 1<<30
	for j := 1; j < len(s.Snakes); j++ {
		o := &s.Snakes[j]
		if !o.Alive() || len(o.Body) == 0 {
			continue
		}
		if d := s.Dist(s.Snakes[0].Head(), o.Head()); d < bd {
			best, bd = j, d
		}
	}
	return best
}

// freeAround counts on-board, unblocked cells around p, treating extra cells as blocked.
func freeAround(s *board.State, blocked []bool, p board.Point, extra ...board.Point) int {
	n := 0
	for _, d := range board.AllDirs {
		q, ok := s.Step(p, d)
		if !ok || blocked[s.Idx(q)] {
			continue
		}
		taken := false
		for _, e := range extra {
			taken = taken || e == q
		}
		if !taken {
			n++
		}
	}
	return n
}

func trapped(s *board.State, blocked []bool, in legal.Info) bool {
	return area(s, blocked, in) < float64(s.Snakes[0].Len())
}

func eatWhenHungry(s *board.State, blocked []bool, fd []int32, in legal.Info) (float64, bool) {
	if s.Snakes[0].Health >= 35 || !in.InBounds {
		return 0, false
	}
	if d := fd[s.Idx(in.Next)]; d >= 0 {
		return -5 * float64(d), true
	}
	return -500, true
}

// pincer squeezes the nearest snake: it stands where the target's head is left
// with the fewest free neighbours — the second half of a sandwich.
func pincer(s *board.State, _ *rand.Rand) board.Dir {
	c, blocked := candidates(s)
	fd := legal.FoodDistances(s, blocked)
	me := &s.Snakes[0]
	t := nearestOther(s)
	return argbest(c, func(in legal.Info) float64 {
		if !in.InBounds {
			return -1e9
		}
		v := 0.0
		if trapped(s, blocked, in) {
			v -= 1000
		}
		if hv, hungry := eatWhenHungry(s, blocked, fd, in); hungry {
			return v + hv
		}
		if t >= 0 {
			th := s.Snakes[t].Head()
			v -= 10 * float64(freeAround(s, blocked, th, in.Next, me.Head()))
			v -= float64(s.Dist(in.Next, th))
		}
		return v
	})
}

// foodBait guards food from one step away and takes head-to-heads it wins
// against snakes that come to eat.
func foodBait(s *board.State, _ *rand.Rand) board.Dir {
	c, blocked := candidates(s)
	fd := legal.FoodDistances(s, blocked)
	return argbest(c, func(in legal.Info) float64 {
		if !in.InBounds {
			return -1e9
		}
		v := 0.0
		if trapped(s, blocked, in) {
			v -= 1000
		}
		if hv, hungry := eatWhenHungry(s, blocked, fd, in); hungry {
			return v + hv
		}
		switch d := fd[s.Idx(in.Next)]; {
		case d == 1:
			v += 30
		case d == 0:
			v -= 10
		case d > 1:
			v -= float64(d)
		}
		if in.H2HWin {
			v += 20
		}
		return v
	})
}

// herdToward stands on the side of the target facing (cx, cy), so the target's
// open side is away from it — toward a wall or into the storm.
func herdToward(s *board.State, blocked []bool, fd []int32, c []legal.Info, cx, cy float64, avoidHazard bool) board.Dir {
	t := nearestOther(s)
	return argbest(c, func(in legal.Info) float64 {
		if !in.InBounds {
			return -1e9
		}
		v := 0.0
		if trapped(s, blocked, in) {
			v -= 1000
		}
		if avoidHazard && in.Hazard {
			v -= 1000
		}
		if hv, hungry := eatWhenHungry(s, blocked, fd, in); hungry || t < 0 {
			return v + hv
		}
		th := s.Snakes[t].Head()
		dx, dy := cx-float64(th.X), cy-float64(th.Y)
		norm := abs(dx) + abs(dy)
		if norm == 0 {
			norm = 1
		}
		gx, gy := float64(th.X)+2*dx/norm, float64(th.Y)+2*dy/norm
		return v - (abs(float64(in.Next.X)-gx) + abs(float64(in.Next.Y)-gy))
	})
}

// edgeHerder pushes the nearest snake toward the nearest wall.
func edgeHerder(s *board.State, _ *rand.Rand) board.Dir {
	c, blocked := candidates(s)
	fd := legal.FoodDistances(s, blocked)
	return herdToward(s, blocked, fd, c, float64(s.W-1)/2, float64(s.H-1)/2, false)
}

// stormTrapper sits between the nearest snake and the centre of the safe
// rectangle, leaving it the storm side, while staying out of the storm itself.
func stormTrapper(s *board.State, _ *rand.Rand) board.Dir {
	c, blocked := candidates(s)
	fd := legal.FoodDistances(s, blocked)
	r := board.SafeRect(s)
	cx, cy := float64(s.W-1)/2, float64(s.H-1)/2
	if !r.Empty() {
		cx, cy = r.Centre()
	}
	return herdToward(s, blocked, fd, c, cx, cy, true)
}
