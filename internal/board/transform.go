package board

// Symmetry is a board symmetry for metamorphic tests: rotating or reflecting a
// position must transform every result correspondingly. Catches coordinate-axis
// and index-bias bugs that fixtures miss.
type Symmetry int

const (
	Identity Symmetry = iota
	Rot90             // (x,y) -> (y, W-1-x); W and H swap
	ReflectX          // (x,y) -> (W-1-x, y)
)

// Point maps p under sym for a board of width w.
func (sym Symmetry) Point(p Point, w int) Point {
	switch sym {
	case Rot90:
		return Point{p.Y, w - 1 - p.X}
	case ReflectX:
		return Point{w - 1 - p.X, p.Y}
	}
	return p
}

// Dir maps a direction under sym.
func (sym Symmetry) Dir(d Dir) Dir {
	switch sym {
	case Rot90:
		return [4]Dir{Right, Left, Up, Down}[d&3]
	case ReflectX:
		return [4]Dir{Up, Down, Right, Left}[d&3]
	}
	return d
}

// Apply returns the transformed position.
func (s *State) Apply(sym Symmetry) *State {
	c := s.Clone()
	if sym == Rot90 {
		c.W, c.H = s.H, s.W
	}
	for i := range c.Snakes {
		for k := range c.Snakes[i].Body {
			c.Snakes[i].Body[k] = sym.Point(c.Snakes[i].Body[k], s.W)
		}
	}
	for i := range c.Food {
		c.Food[i] = sym.Point(c.Food[i], s.W)
	}
	if s.Hazard != nil {
		nh := make([]uint8, c.W*c.H)
		for i, v := range s.Hazard {
			q := sym.Point(s.Pt(i), s.W)
			nh[q.Y*c.W+q.X] = v
		}
		c.Hazard = nh
	}
	return c
}

// PermuteOpponents reorders snakes 1..n-1: new index k+1 holds old perm[k]+1.
func (s *State) PermuteOpponents(perm []int) *State {
	c := s.Clone()
	for k, old := range perm {
		c.Snakes[k+1] = s.Clone().Snakes[old+1]
	}
	return c
}
