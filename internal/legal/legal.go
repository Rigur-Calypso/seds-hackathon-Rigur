// Package legal computes the legal action mask using three occupancy layers
// that are never merged (BUILD_PLAN Step 3):
//
//  1. Blocked — body cells still occupied after everyone moves. Every current
//     head becomes a neck and IS blocked; a tail is free unless duplicated (R4).
//  2. Head threats — cells an opponent head could enter next turn, resolved by
//     length (R2), computed on demand, never written into Blocked (R3).
//  3. Hazard — costly, not blocked (R5/R6); only lethal damage excludes a move.
package legal

import "github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"

// Blocked returns layer 1 for the next tick.
func Blocked(s *board.State) []bool {
	b := make([]bool, s.Cells())
	for i := range s.Snakes {
		sn := &s.Snakes[i]
		if !sn.Alive() || len(sn.Body) == 0 {
			continue
		}
		last := len(sn.Body) - 1 // exclusive: the tail vacates...
		if !sn.TailVacates() {
			last = len(sn.Body) // ...unless duplicated (R4)
		}
		for k := 0; k < last; k++ {
			if p := sn.Body[k]; s.InBounds(p) {
				b[s.Idx(p)] = true
			}
		}
	}
	return b
}

// Info describes one candidate move for one snake.
type Info struct {
	Dir      board.Dir
	Next     board.Point
	InBounds bool
	Neck     bool
	Blocked  bool
	Lethal   bool // starvation or hazard damage kills this turn (R1, R5, R6)
	H2HLose  bool // an opponent of >= length could arrive simultaneously (R2)
	H2HWin   bool // only strictly shorter opponents could arrive
	Hazard   bool // hazard square without food
}

// Safe reports a move that is not certain death on its own.
func (in Info) Safe() bool { return in.InBounds && !in.Blocked && !in.Lethal }

// Analyze returns Info for all four directions of snake i.
func Analyze(s *board.State, blocked []bool, i int) [4]Info {
	var out [4]Info
	sn := &s.Snakes[i]
	if len(sn.Body) == 0 {
		return out
	}
	head := sn.Head()
	for _, d := range board.AllDirs {
		in := &out[d]
		in.Dir = d
		q, ok := s.Step(head, d)
		in.Next, in.InBounds = q, ok
		if len(sn.Body) > 1 && sn.Body[1] != head && q == sn.Body[1] {
			in.Neck = true
		}
		if !ok {
			continue
		}
		in.Blocked = blocked[s.Idx(q)]
		food := s.HasFood(q)
		h := sn.Health - 1
		if !food {
			if hz := s.HazardAt(q); hz > 0 {
				in.Hazard = true
				h -= s.Rules.HazardDamage * hz
			}
		}
		in.Lethal = !food && h <= 0
		for j := range s.Snakes {
			o := &s.Snakes[j]
			if j == i || !o.Alive() || len(o.Body) == 0 || !Adjacent(s, o.Head(), q) {
				continue
			}
			if o.Len() >= sn.Len() {
				in.H2HLose = true
			} else {
				in.H2HWin = true
			}
		}
	}
	return out
}

// Adjacent reports whether b is one step from a (wrap-aware).
func Adjacent(s *board.State, a, b board.Point) bool {
	for _, d := range board.AllDirs {
		if q, ok := s.Step(a, d); ok && q == b {
			return true
		}
	}
	return false
}

// SafeWith lists snake i's safe moves given a precomputed Blocked layer.
func SafeWith(s *board.State, blocked []bool, i int) []board.Dir {
	info := Analyze(s, blocked, i)
	out := make([]board.Dir, 0, 4)
	for _, in := range info {
		if in.Safe() {
			out = append(out, in.Dir)
		}
	}
	return out
}

// Safe lists snake i's safe moves.
func Safe(s *board.State, i int) []board.Dir { return SafeWith(s, Blocked(s), i) }

// DefaultMove is what the engine applies to a snake that sends no valid move:
// continue in the direction of travel (head relative to neck), else up.
func DefaultMove(sn *board.Snake) board.Dir {
	if len(sn.Body) >= 2 {
		h, n := sn.Body[0], sn.Body[1]
		switch {
		case h.X == n.X+1:
			return board.Right
		case h.X == n.X-1:
			return board.Left
		case h.Y == n.Y+1:
			return board.Up
		case h.Y == n.Y-1:
			return board.Down
		}
	}
	return board.Up
}

// FloodArea counts free cells reachable from start (inclusive), stopping at
// limit. It ignores time; the temporal Voronoi handles tail release properly.
func FloodArea(s *board.State, blocked []bool, start board.Point, limit int) int {
	if !s.InBounds(start) || blocked[s.Idx(start)] || limit <= 0 {
		return 0
	}
	seen := make([]bool, len(blocked))
	queue := make([]int, 0, 64)
	c0 := s.Idx(start)
	seen[c0] = true
	queue = append(queue, c0)
	for h := 0; h < len(queue) && len(queue) < limit; h++ {
		p := s.Pt(queue[h])
		for _, d := range board.AllDirs {
			q, ok := s.Step(p, d)
			if !ok {
				continue
			}
			c := s.Idx(q)
			if seen[c] || blocked[c] {
				continue
			}
			seen[c] = true
			queue = append(queue, c)
		}
	}
	if len(queue) > limit {
		return limit
	}
	return len(queue)
}

// FoodDistances is one BFS outward from all food at once (TheApX/hungry idea):
// dist[c] is steps from c to the nearest food through unblocked cells, -1 if
// unreachable. Scores all four moves in one pass.
func FoodDistances(s *board.State, blocked []bool) []int32 {
	dist := make([]int32, s.Cells())
	for i := range dist {
		dist[i] = -1
	}
	queue := make([]int, 0, 64)
	for _, f := range s.Food {
		if !s.InBounds(f) {
			continue
		}
		c := s.Idx(f)
		if dist[c] < 0 {
			dist[c] = 0
			queue = append(queue, c)
		}
	}
	for h := 0; h < len(queue); h++ {
		p := s.Pt(queue[h])
		for _, d := range board.AllDirs {
			q, ok := s.Step(p, d)
			if !ok {
				continue
			}
			c := s.Idx(q)
			if dist[c] >= 0 || blocked[c] {
				continue
			}
			dist[c] = dist[queue[h]] + 1
			queue = append(queue, c)
		}
	}
	return dist
}
