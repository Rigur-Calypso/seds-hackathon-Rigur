// Package fallback is the guaranteed move: a lexicographic comparator with no
// weights and nothing to tune (pattern from TheApX/hungry, reimplemented). It
// runs in well under a millisecond and cannot fail.
package fallback

import (
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
)

const nKeys = 10

type key [nKeys]int

func less(a, b key) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Best returns snake 0's fallback move. Lower key wins, compared in order:
//
//	never the neck → never off-board → never a body cell → never lethal
//	starvation/hazard → never a losing head-to-head → not trapped (reachable
//	area ≥ length) → not in hazard → more free neighbours → fewer steps to food
//	→ closer to the centre of the safe rectangle.
//
// "Not trapped" and "not in hazard" are additions to the plan's chain: both are
// cheap, structural and weight-free (see DEVLOG).
func Best(s *board.State) board.Dir {
	return BestFor(s, 0)
}

// BestFor returns the fallback move for snake i.
func BestFor(s *board.State, i int) board.Dir {
	sn := &s.Snakes[i]
	if len(sn.Body) == 0 {
		return board.Up
	}
	blocked := legal.Blocked(s)
	info := legal.Analyze(s, blocked, i)
	food := legal.FoodDistances(s, blocked)
	rect := board.SafeRect(s)
	cx2, cy2 := rect.MinX+rect.MaxX, rect.MinY+rect.MaxY
	L := sn.Len()
	const far = 1 << 20

	best := -1
	var bk key
	for idx, in := range info {
		k := key{b2i(in.Neck), b2i(!in.InBounds), b2i(in.Blocked), b2i(in.Lethal), b2i(in.H2HLose), far, far, far, far, far}
		if in.InBounds && !in.Blocked {
			area := legal.FloodArea(s, blocked, in.Next, L+1)
			k[5] = b2i(area < L)
			k[6] = b2i(in.Hazard)
			free := 0
			for _, d := range board.AllDirs {
				if q, ok := s.Step(in.Next, d); ok && !blocked[s.Idx(q)] && q != sn.Head() {
					free++
				}
			}
			k[7] = -free
			if fd := food[s.Idx(in.Next)]; fd >= 0 {
				k[8] = int(fd)
			}
			dx, dy := 2*in.Next.X-cx2, 2*in.Next.Y-cy2
			if dx < 0 {
				dx = -dx
			}
			if dy < 0 {
				dy = -dy
			}
			k[9] = dx + dy
		}
		if best < 0 || less(k, bk) {
			best, bk = idx, k
		}
	}
	return info[best].Dir
}
