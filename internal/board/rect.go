package board

// Rect is an inclusive axis-aligned rectangle. R8: the royale safe region is
// always one of these.
type Rect struct{ MinX, MinY, MaxX, MaxY int }

// Empty reports a rectangle with no cells.
func (r Rect) Empty() bool { return r.MinX > r.MaxX || r.MinY > r.MaxY }

// Contains reports p inside r.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.MinX && p.X <= r.MaxX && p.Y >= r.MinY && p.Y <= r.MaxY
}

// Shrink removes one line from one side, using the engine's side numbering
// (R8): 0 minX++, 1 maxX--, 2 minY++, 3 maxY--.
func (r Rect) Shrink(side int) Rect {
	switch side & 3 {
	case 0:
		r.MinX++
	case 1:
		r.MaxX--
	case 2:
		r.MinY++
	case 3:
		r.MaxY--
	}
	return r
}

// ShrinkAll is the union of the four possible next hazard rings: a cell is
// safe under every outcome only if it is inside this.
func (r Rect) ShrinkAll() Rect { return Rect{r.MinX + 1, r.MinY + 1, r.MaxX - 1, r.MaxY - 1} }

// Centre is the geometric centre.
func (r Rect) Centre() (float64, float64) {
	return float64(r.MinX+r.MaxX) / 2, float64(r.MinY+r.MaxY) / 2
}

// SafeRect reconstructs the safe rectangle from the hazard list (R8: always
// trust the supplied hazards). No hazards means the full board.
func SafeRect(s *State) Rect {
	if s.Hazard == nil {
		return Rect{0, 0, s.W - 1, s.H - 1}
	}
	r := Rect{s.W, s.H, -1, -1}
	for y := 0; y < s.H; y++ {
		for x := 0; x < s.W; x++ {
			if s.Hazard[y*s.W+x] != 0 {
				continue
			}
			if x < r.MinX {
				r.MinX = x
			}
			if x > r.MaxX {
				r.MaxX = x
			}
			if y < r.MinY {
				r.MinY = y
			}
			if y > r.MaxY {
				r.MaxY = y
			}
		}
	}
	return r
}

// RingHazards returns hazard counts for "every cell outside r" (R8).
func RingHazards(w, h int, r Rect) []uint8 {
	out := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !r.Contains(Point{x, y}) {
				out[y*w+x] = 1
			}
		}
	}
	return out
}
