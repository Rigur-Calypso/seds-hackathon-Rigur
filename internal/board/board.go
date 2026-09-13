// Package board is the compact, engine-independent game state used by the
// resolver, evaluator and search. When built from a request, Snakes[0] is
// always "you".
package board

import (
	"strings"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
)

// Point is a cell. (0,0) is bottom-left, Up is +Y.
type Point struct{ X, Y int }

// Dir is a move.
type Dir uint8

const (
	Up Dir = iota
	Down
	Left
	Right
)

// AllDirs in canonical order; ties everywhere break in this order.
var AllDirs = [4]Dir{Up, Down, Left, Right}

var (
	dirNames = [4]string{"up", "down", "left", "right"}
	dirDX    = [4]int{0, 0, -1, 1}
	dirDY    = [4]int{1, -1, 0, 0}
	opposite = [4]Dir{Down, Up, Right, Left}
)

func (d Dir) String() string {
	if d < 4 {
		return dirNames[d]
	}
	return "up"
}

// Opposite returns the reverse direction.
func (d Dir) Opposite() Dir { return opposite[d&3] }

// ParseDir parses "up"/"down"/"left"/"right".
func ParseDir(s string) (Dir, bool) {
	for i, n := range dirNames {
		if n == s {
			return Dir(i), true
		}
	}
	return Up, false
}

// Add moves p one cell (no wrapping, no bounds check).
func (p Point) Add(d Dir) Point { return Point{p.X + dirDX[d&3], p.Y + dirDY[d&3]} }

// Manhattan distance (non-wrapped).
func Manhattan(a, b Point) int {
	dx, dy := a.X-b.X, a.Y-b.Y
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	return dx + dy
}

// Cause is an elimination cause; the zero value means alive.
type Cause uint8

const (
	NotEliminated Cause = iota
	ByCollision
	BySelfCollision
	ByOutOfHealth
	ByHeadToHead
	ByOutOfBounds
	ByHazard
)

var causeNames = [...]string{"", "snake-collision", "snake-self-collision", "out-of-health", "head-collision", "wall-collision", "hazard"}

func (c Cause) String() string {
	if int(c) < len(causeNames) {
		return causeNames[c]
	}
	return "unknown"
}

// CauseFromString maps engine cause strings.
func CauseFromString(s string) Cause {
	for i, n := range causeNames {
		if n == s {
			return Cause(i)
		}
	}
	return ByCollision
}

// Snake is one snake.
type Snake struct {
	ID       string
	Name     string
	Body     []Point // head first
	Health   int
	Cause    Cause
	ElimTurn int
	Latency  int
}

// Alive reports not eliminated.
func (s *Snake) Alive() bool { return s.Cause == NotEliminated }

// Head is Body[0].
func (s *Snake) Head() Point { return s.Body[0] }

// Len is the body length including duplicated segments.
func (s *Snake) Len() int { return len(s.Body) }

// TailVacates is R4: movement pops the tail every turn and growth appends a
// duplicate of the last segment, so the tail cell frees next turn unless the
// last two segments are identical. Applies to every snake.
func (s *Snake) TailVacates() bool {
	n := len(s.Body)
	return n < 2 || s.Body[n-1] != s.Body[n-2]
}

// Rules carries the ruleset flags and settings read from the request (R10).
type Rules struct {
	Name         string
	Royale       bool
	Constrictor  bool
	Wrapped      bool
	HazardDamage int
	ShrinkEveryN int
	TimeoutMs    int
}

// State is a full board position.
type State struct {
	W, H   int
	Turn   int
	Snakes []Snake
	Food   []Point
	// Hazard holds hazard counts per cell (index y*W+x); nil means none. The
	// engine applies damage once per hazard entry, so stacked hazards count.
	// Treated as immutable: Clone shares it, the resolver replaces it wholesale.
	Hazard []uint8
	Rules  Rules
}

// Dist is Manhattan distance, toroidal on wrapped boards (min over ±W, ±H).
func (s *State) Dist(a, b Point) int {
	dx, dy := a.X-b.X, a.Y-b.Y
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if s.Rules.Wrapped {
		if w := s.W - dx; w < dx {
			dx = w
		}
		if h := s.H - dy; h < dy {
			dy = h
		}
	}
	return dx + dy
}

// Idx is the flat cell index.
func (s *State) Idx(p Point) int { return p.Y*s.W + p.X }

// Pt is the inverse of Idx.
func (s *State) Pt(i int) Point { return Point{i % s.W, i / s.W} }

// Cells is W*H.
func (s *State) Cells() int { return s.W * s.H }

// InBounds reports whether p is on the board.
func (s *State) InBounds(p Point) bool { return p.X >= 0 && p.Y >= 0 && p.X < s.W && p.Y < s.H }

// Step moves p one cell in d, wrapping on wrapped boards; ok=false if off-board.
func (s *State) Step(p Point, d Dir) (Point, bool) {
	q := p.Add(d)
	if s.Rules.Wrapped {
		q.X = ((q.X % s.W) + s.W) % s.W
		q.Y = ((q.Y % s.H) + s.H) % s.H
		return q, true
	}
	return q, s.InBounds(q)
}

// HazardAt is the hazard stack count at p.
func (s *State) HazardAt(p Point) int {
	if s.Hazard == nil || !s.InBounds(p) {
		return 0
	}
	return int(s.Hazard[s.Idx(p)])
}

// HasFood reports food at p.
func (s *State) HasFood(p Point) bool {
	for _, f := range s.Food {
		if f == p {
			return true
		}
	}
	return false
}

// AliveCount counts live snakes.
func (s *State) AliveCount() int {
	n := 0
	for i := range s.Snakes {
		if s.Snakes[i].Alive() {
			n++
		}
	}
	return n
}

// Clone deep-copies snakes and food with one allocation for all bodies. Body
// slices are capacity-capped so a later append can never overwrite a
// neighbouring snake in the shared backing array.
func (s *State) Clone() *State {
	c := *s
	total := 0
	for i := range s.Snakes {
		total += len(s.Snakes[i].Body)
	}
	backing := make([]Point, total)
	c.Snakes = make([]Snake, len(s.Snakes))
	off := 0
	for i := range s.Snakes {
		c.Snakes[i] = s.Snakes[i]
		n := len(s.Snakes[i].Body)
		copy(backing[off:off+n], s.Snakes[i].Body)
		c.Snakes[i].Body = backing[off : off+n : off+n]
		off += n
	}
	if len(s.Food) > 0 {
		c.Food = append(make([]Point, 0, len(s.Food)), s.Food...)
	} else {
		c.Food = nil
	}
	return &c
}

// RulesFromAPI extracts ruleset flags.
func RulesFromAPI(gs *api.GameState) Rules {
	name := strings.ToLower(gs.Game.Ruleset.Name)
	set := gs.Game.Ruleset.Settings
	return Rules{
		Name:         name,
		Royale:       name == "royale" || strings.EqualFold(gs.Game.Map, "royale"),
		Constrictor:  strings.Contains(name, "constrictor"),
		Wrapped:      strings.Contains(name, "wrapped"),
		HazardDamage: set.HazardDamagePerTurn,
		ShrinkEveryN: set.Royale.ShrinkEveryNTurns,
		TimeoutMs:    gs.Game.Timeout,
	}
}

// FromAPI builds a State with "you" at index 0. ok is false when you are not
// among the live snakes (dead, or malformed request).
func FromAPI(gs *api.GameState) (*State, bool) {
	b := &gs.Board
	s := &State{W: b.Width, H: b.Height, Turn: gs.Turn, Rules: RulesFromAPI(gs)}
	if s.W <= 0 || s.H <= 0 {
		return s, false
	}
	add := func(a *api.Snake) {
		body := make([]Point, len(a.Body))
		for i, c := range a.Body {
			body[i] = Point{c.X, c.Y}
		}
		s.Snakes = append(s.Snakes, Snake{ID: a.ID, Name: a.Name, Body: body, Health: a.Health, Latency: int(a.Latency)})
	}
	you := -1
	for i := range b.Snakes {
		if b.Snakes[i].ID == gs.You.ID && len(b.Snakes[i].Body) > 0 {
			you = i
			break
		}
	}
	if you < 0 {
		return s, false
	}
	add(&b.Snakes[you])
	for i := range b.Snakes {
		if i != you && len(b.Snakes[i].Body) > 0 {
			add(&b.Snakes[i])
		}
	}
	for _, c := range b.Food {
		p := Point{c.X, c.Y}
		if s.InBounds(p) {
			s.Food = append(s.Food, p)
		}
	}
	if len(b.Hazards) > 0 {
		s.Hazard = make([]uint8, s.W*s.H)
		for _, c := range b.Hazards {
			p := Point{c.X, c.Y}
			if s.InBounds(p) && s.Hazard[s.Idx(p)] < 255 {
				s.Hazard[s.Idx(p)]++
			}
		}
	}
	return s, true
}
