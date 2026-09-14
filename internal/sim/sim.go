// Package sim is the search-speed game state. It holds one position in flat,
// allocation-free arrays and applies or reverts one simultaneous turn in place
// (Make / Unmake), so a search visits hundreds of thousands of positions per
// second without cloning. It reproduces internal/rules exactly — differential-
// tested against that resolver here, and against the official engine in
// tools/arena — and never imports the AGPL engine (CLAUDE.md §8).
package sim

import (
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
)

// MaxSnakes is the most snakes the fast state holds; larger games use v1.
const MaxSnakes = 8

// MaxHealth is the engine health cap.
const MaxHealth = 100

// ShrinkMode is how search models royale rings it cannot see yet (R8).
type ShrinkMode uint8

const (
	// ShrinkKeep keeps the supplied hazards for the whole search (optimistic).
	ShrinkKeep ShrinkMode = iota
	// ShrinkPessimistic hazards, from each future shrink turn on, the union of
	// the four possible next rings. The direction is never predicted (R8).
	ShrinkPessimistic
)

// maxLayers bounds how many future shrinks are modelled (a search never sees more).
const maxLayers = 12

// Game is everything that stays fixed during one search.
type Game struct {
	W, H, Cells int
	// Nbr[c][d] is the cell one step from c in direction d (board.AllDirs order),
	// wrapping on wrapped boards, -1 off the board.
	Nbr         [][4]int16
	Wrapped     bool
	Constrictor bool
	Royale      bool
	ShrinkEvery int
	Damage      int32
	layers      [][]uint8    // layers[k]: hazard counts after k more shrinks; [0] is the request (R8: truth)
	rects       []board.Rect // safe rectangle of each layer
	shrinkN     int32
	baseShrinks int32
	capBody     int32
}

// NewGame precomputes neighbours and hazard layers for bs.
func NewGame(bs *board.State, mode ShrinkMode) *Game {
	g := &Game{
		W: bs.W, H: bs.H, Cells: bs.W * bs.H,
		Wrapped: bs.Rules.Wrapped, Constrictor: bs.Rules.Constrictor,
		Royale: bs.Rules.Royale, ShrinkEvery: bs.Rules.ShrinkEveryN,
		Damage: int32(bs.Rules.HazardDamage),
	}
	defer func() {
		g.rects = make([]board.Rect, len(g.layers))
		for k, l := range g.layers {
			g.rects[k] = board.SafeRect(&board.State{W: g.W, H: g.H, Hazard: l})
		}
	}()
	g.capBody = int32(g.Cells) + 8
	g.Nbr = make([][4]int16, g.Cells)
	for c := 0; c < g.Cells; c++ {
		p := bs.Pt(c)
		for _, d := range board.AllDirs {
			if q, ok := bs.Step(p, d); ok {
				g.Nbr[c][d] = int16(bs.Idx(q))
			} else {
				g.Nbr[c][d] = -1
			}
		}
	}
	g.layers = [][]uint8{bs.Hazard}
	if mode == ShrinkPessimistic && bs.Rules.Royale && bs.Rules.ShrinkEveryN > 0 {
		r := board.SafeRect(bs)
		if isRing(bs, r) {
			g.shrinkN = int32(bs.Rules.ShrinkEveryN)
			g.baseShrinks = int32(bs.Turn) / g.shrinkN
			for k := 1; k < maxLayers; k++ {
				rk := board.Rect{MinX: r.MinX + k, MinY: r.MinY + k, MaxX: r.MaxX - k, MaxY: r.MaxY - k}
				g.layers = append(g.layers, board.RingHazards(bs.W, bs.H, rk))
				if rk.Empty() {
					break
				}
			}
		}
	}
	return g
}

// isRing reports whether the supplied hazards are exactly the royale ring
// around r (other maps place hazards freely and are kept static).
func isRing(bs *board.State, r board.Rect) bool {
	if bs.Hazard == nil {
		return true
	}
	ring := board.RingHazards(bs.W, bs.H, r)
	for i := range ring {
		if ring[i] != bs.Hazard[i] {
			return false
		}
	}
	return true
}

// layer is the hazard layer in force for a state at turn. R8: a state at turn
// t carries t/N shrinks, and its hazards damage the move that leaves it.
func (g *Game) layer(turn int32) int {
	if g.shrinkN == 0 {
		return 0
	}
	k := int(turn/g.shrinkN - g.baseShrinks)
	if k <= 0 {
		return 0
	}
	if k >= len(g.layers) {
		k = len(g.layers) - 1
	}
	return k
}

// Hazards returns the hazard counts that damage a move leaving a state at
// turn (nil when there are none).
func (g *Game) Hazards(turn int32) []uint8 { return g.layers[g.layer(turn)] }

// SafeRect is the royale safe rectangle in force at turn (the whole board
// when there are no hazards).
func (g *Game) SafeRect(turn int32) board.Rect { return g.rects[g.layer(turn)] }

// Dist is the Manhattan distance between cells, toroidal on wrapped boards.
func (g *Game) Dist(a, b int16) int {
	ax, ay := int(a)%g.W, int(a)/g.W
	bx, by := int(b)%g.W, int(b)/g.W
	dx, dy := ax-bx, ay-by
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if g.Wrapped {
		if w := g.W - dx; w < dx {
			dx = w
		}
		if h := g.H - dy; h < dy {
			dy = h
		}
	}
	return dx + dy
}

// Snake is one snake. The body is a ring buffer of cell indices, head at
// start, so a move writes one cell instead of shifting the body.
type Snake struct {
	body   []int16
	start  int32
	Len    int32
	Health int32
	Alive  bool
}

// Seg is body segment k (0 = head).
func (sn *Snake) Seg(k int32) int16 {
	i := sn.start + k
	if i >= int32(len(sn.body)) {
		i -= int32(len(sn.body))
	}
	return sn.body[i]
}

// Head is segment 0.
func (sn *Snake) Head() int16 { return sn.body[sn.start] }

// Tail is the last segment.
func (sn *Snake) Tail() int16 { return sn.Seg(sn.Len - 1) }

// TailVacates is R4: the tail frees next turn unless it is duplicated.
func (sn *Snake) TailVacates() bool { return sn.Len < 2 || sn.Seg(sn.Len-1) != sn.Seg(sn.Len-2) }

// State is one position. Snake 0 is always "you".
type State struct {
	G    *Game
	Turn int32
	N    int
	S    [MaxSnakes]Snake
	// occ counts body segments (heads included) of live snakes per cell.
	occ  []uint8
	food []bool
	// foods lists every cell that held food at construction; search only ever
	// removes food, so this bounds every food scan.
	foods []int16
	Hash  uint64
}

// New builds the fast state for bs. ok is false when the position does not
// fit: more than MaxSnakes snakes, an off-board segment, or an absurd body.
func New(bs *board.State, g *Game) (*State, bool) {
	if len(bs.Snakes) == 0 || len(bs.Snakes) > MaxSnakes || g.Cells <= 0 || g.Cells >= 1<<15 {
		return nil, false
	}
	st := &State{G: g, Turn: int32(bs.Turn), N: len(bs.Snakes), occ: make([]uint8, g.Cells), food: make([]bool, g.Cells)}
	backing := make([]int16, int(g.capBody)*len(bs.Snakes))
	for i := range bs.Snakes {
		src := &bs.Snakes[i]
		sn := &st.S[i]
		sn.body = backing[i*int(g.capBody) : (i+1)*int(g.capBody) : (i+1)*int(g.capBody)]
		sn.Health = int32(src.Health)
		sn.Alive = src.Alive()
		if !sn.Alive {
			// Eliminated snakes never act or block; their body may even be off the
			// board (wall death), so keep a one-cell stub.
			sn.Len = 1
			continue
		}
		if len(src.Body) == 0 || int32(len(src.Body)) >= g.capBody-2 {
			return nil, false
		}
		for k, p := range src.Body {
			if !bs.InBounds(p) {
				return nil, false
			}
			sn.body[k] = int16(bs.Idx(p))
		}
		sn.Len = int32(len(src.Body))
		for k := int32(0); k < sn.Len; k++ {
			c := sn.body[k]
			if st.occ[c] == 255 {
				return nil, false
			}
			st.occ[c]++
		}
	}
	for _, f := range bs.Food {
		if !bs.InBounds(f) {
			continue
		}
		if c := int16(bs.Idx(f)); !st.food[c] {
			st.food[c] = true
			st.foods = append(st.foods, c)
		}
	}
	st.Hash = st.ComputeHash()
	return st, true
}

// Food reports food at c.
func (st *State) Food(c int16) bool { return st.food[c] }

// FoodSites lists every cell that held food at construction; check Food(c)
// for whether it still does. The slice must not be modified.
func (st *State) FoodSites() []int16 { return st.foods }

// FoodCells calls fn for every cell that holds food now.
func (st *State) FoodCells(fn func(c int16)) {
	for _, c := range st.foods {
		if st.food[c] {
			fn(c)
		}
	}
}

// Occupied counts live body segments (heads included) at c.
func (st *State) Occupied(c int16) int { return int(st.occ[c]) }

// AliveCount counts live snakes.
func (st *State) AliveCount() int {
	n := 0
	for i := 0; i < st.N; i++ {
		if st.S[i].Alive {
			n++
		}
	}
	return n
}

// DefaultMove is what the engine plays for a snake that sends no move:
// continue from neck to head, else up.
func (st *State) DefaultMove(i int) board.Dir {
	sn := &st.S[i]
	if sn.Len >= 2 {
		h, n := sn.Head(), sn.Seg(1)
		for _, d := range board.AllDirs {
			if st.G.Nbr[n][d] == h {
				return d
			}
		}
	}
	return board.Up
}

// ToBoard converts back to a board.State (tests and diagnostics). Eliminated
// snakes are marked with a generic collision cause.
func (st *State) ToBoard(rules board.Rules) *board.State {
	g := st.G
	bs := &board.State{W: g.W, H: g.H, Turn: int(st.Turn), Rules: rules, Hazard: g.Hazards(st.Turn)}
	pt := func(c int16) board.Point { return board.Point{X: int(c) % g.W, Y: int(c) / g.W} }
	for i := 0; i < st.N; i++ {
		sn := &st.S[i]
		b := board.Snake{Health: int(sn.Health)}
		for k := int32(0); k < sn.Len; k++ {
			b.Body = append(b.Body, pt(sn.Seg(k)))
		}
		if !sn.Alive {
			b.Cause = board.ByCollision
		}
		bs.Snakes = append(bs.Snakes, b)
	}
	st.FoodCells(func(c int16) { bs.Food = append(bs.Food, pt(c)) })
	return bs
}

// ComputeHash recomputes the Zobrist-style key from scratch. Make keeps Hash
// equal to this incrementally; the tests check it.
func (st *State) ComputeHash() uint64 {
	var h uint64
	for i := 0; i < st.N; i++ {
		sn := &st.S[i]
		if !sn.Alive {
			h ^= keyDead(i)
			continue
		}
		h ^= keyHead(i, sn.Head()) ^ keyLen(i, sn.Len) ^ keyHealth(i, sn.Health)
		for k := int32(0); k < sn.Len; k++ {
			h ^= keySeg(i, sn.Seg(k))
		}
	}
	for _, c := range st.foods {
		if st.food[c] {
			h ^= keyFood(c)
		}
	}
	return h ^ keyShrink(st.G.layer(st.Turn))
}

// mix is the splitmix64 finaliser: every key is derived, no tables to allocate.
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func keySeg(i int, c int16) uint64    { return mix(uint64(i)<<56 | 1<<52 | uint64(uint16(c))) }
func keyHead(i int, c int16) uint64   { return mix(uint64(i)<<56 | 2<<52 | uint64(uint16(c))) }
func keyLen(i int, l int32) uint64    { return mix(uint64(i)<<56 | 3<<52 | uint64(uint32(l))) }
func keyHealth(i int, h int32) uint64 { return mix(uint64(i)<<56 | 4<<52 | uint64(uint32(h))) }
func keyDead(i int) uint64            { return mix(uint64(i)<<56 | 5<<52) }
func keyFood(c int16) uint64          { return mix(6<<52 | uint64(uint16(c))) }
func keyShrink(k int) uint64          { return mix(7<<52 | uint64(k)) }
