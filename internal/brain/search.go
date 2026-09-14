// Package brain is the v2 decision engine: iterative-deepening alpha-beta over
// whole simultaneous turns on the fast sim state.
//
// A turn is our move followed by a joint reply of the opponents near us that
// minimises our value (paranoid: they may see our move). Opponents too far away
// to interact within the remaining horizon play one predicted move instead
// (locality masking, after m-schier and bookworm), which keeps branching near
// 3×3 instead of 3×27. Every joint action is resolved with the exact one-turn
// rules (sim.Make), so head-to-heads, tail release and hazards are never
// approximated. Leaves are scored by the temporal Voronoi evaluator; deaths
// and wins are ordered terminal bands.
//
// A transposition table, history and killer ordering, and danger extensions (an
// equal-or-longer head within two cells extends a leaf by one turn) turn node
// count into depth. The search is cooperative: it polls the context and a node
// budget, never outlives the call, and returns the deepest completed
// iteration.
package brain

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/sim"
)

var (
	// ErrUnsupported means the position does not fit the fast state (too many
	// snakes, malformed bodies); the caller should use the v1 engine.
	ErrUnsupported = errors.New("brain: position not supported")
	// ErrIncomplete means not even depth 1 finished before the deadline.
	ErrIncomplete = errors.New("brain: no completed depth")
)

// maxPly bounds the recursion (turns from the root, extensions included).
const maxPly = 48

// RootScore is a root move and its value at the deepest completed depth.
type RootScore struct {
	Dir   board.Dir
	Value float64
}

// Result is the outcome of one search.
type Result struct {
	Move   board.Dir
	Depth  int
	Scores []RootScore // best first; values of non-best moves may be upper bounds
	Nodes  int64
}

// Searcher holds everything one search mutates. Pooled; never shared.
type Searcher struct {
	p        *config.Params
	g        *sim.Game
	st       *sim.State
	ctx      context.Context
	fill     sim.Fill
	tt       *table
	nodes    int64
	maxNodes int64
	stop     bool
	solo     bool

	rootAlive [sim.MaxSnakes]bool
	rootOpp   int
	rootLen   int32

	undo     [maxPly]sim.Undo
	killer   [maxPly]sim.Moves
	killerOK [maxPly]bool
	hist     [][4]int32

	endUndo         [maxSurvival]sim.Undo
	endgameSurvival int // turns the endgame filter proved, 0 if it did not run
}

var pool = sync.Pool{New: func() any { return new(Searcher) }}

// Search picks snake 0's move among root (its safe moves, preferred first).
func Search(ctx context.Context, bs *board.State, p *config.Params, root []board.Dir) (Result, error) {
	if len(root) == 0 || len(bs.Snakes) == 0 || !bs.Snakes[0].Alive() {
		return Result{}, ErrUnsupported
	}
	mode := sim.ShrinkKeep
	if p.SearchShrinkPessimistic {
		mode = sim.ShrinkPessimistic
	}
	g := sim.NewGame(bs, mode)
	st, ok := sim.New(bs, g)
	if !ok {
		return Result{}, ErrUnsupported
	}
	se := pool.Get().(*Searcher)
	defer pool.Put(se)
	se.reset(ctx, p, g, st)
	return se.run(root)
}

func (se *Searcher) reset(ctx context.Context, p *config.Params, g *sim.Game, st *sim.State) {
	se.p, se.g, se.st, se.ctx = p, g, st, ctx
	se.nodes, se.stop = 0, false
	se.maxNodes = int64(p.SearchNodes)
	se.tt = acquireTable(se.tt, p.SearchTTBits)
	se.rootOpp = 0
	for i := range se.rootAlive {
		se.rootAlive[i] = i < st.N && st.S[i].Alive
		if i > 0 && se.rootAlive[i] {
			se.rootOpp++
		}
	}
	se.solo = se.rootOpp == 0
	se.rootLen = st.S[0].Len
	se.killerOK = [maxPly]bool{}
	if cap(se.hist) < g.Cells {
		se.hist = make([][4]int32, g.Cells)
	} else {
		se.hist = se.hist[:g.Cells]
		for i := range se.hist {
			se.hist[i] = [4]int32{}
		}
	}
}

func (se *Searcher) run(root []board.Dir) (Result, error) {
	p := se.p
	se.endgameSurvival = 0
	if p.SearchEndgame {
		root = se.endgameFilter(root)
	}
	order := append([]board.Dir(nil), root...)
	var res Result
	maxDepth := p.SearchMaxDepth
	if maxDepth < 1 {
		maxDepth = 1
	}
	if maxDepth > maxPly/2 {
		maxDepth = maxPly / 2 // room for danger extensions
	}
	for depth := 1; depth <= maxDepth; depth++ {
		scores := make([]RootScore, 0, len(order))
		alpha := math.Inf(-1)
		for _, m := range order {
			v := se.minNode(m, depth, 0, p.SearchExtensions, alpha, math.Inf(1))
			if se.stop {
				break
			}
			scores = append(scores, RootScore{m, v})
			if v > alpha {
				alpha = v
			}
		}
		if se.stop {
			// The principal move was searched first; any completed alternative
			// that beats its deeper value is exact (beta is +inf at the root).
			if res.Depth > 0 && len(scores) > 1 {
				best := 0
				for i := range scores {
					if scores[i].Value > scores[best].Value {
						best = i
					}
				}
				res.Move = scores[best].Dir
			}
			break
		}
		sort.SliceStable(scores, func(a, b int) bool { return scores[a].Value > scores[b].Value })
		res.Move, res.Depth, res.Scores = scores[0].Dir, depth, scores
		for i := range scores {
			order[i] = scores[i].Dir
		}
		if depth >= 2 && (IsWin(scores[0].Value) || IsLoss(scores[0].Value)) {
			break // proven within the model; deeper search cannot change the sign
		}
	}
	res.Nodes = se.nodes
	if res.Depth == 0 {
		return res, ErrIncomplete
	}
	return res, nil
}

// tick counts a node and reports whether the search must unwind.
func (se *Searcher) tick() bool {
	se.nodes++
	if se.maxNodes > 0 && se.nodes > se.maxNodes {
		se.stop = true
	}
	if se.nodes&127 == 0 && se.ctx.Err() != nil {
		se.stop = true
	}
	return se.stop
}

// maxNode: snake 0 to choose with depth whole turns left.
func (se *Searcher) maxNode(depth, ply, ext int, alpha, beta float64) float64 {
	st := se.st
	if depth <= 0 {
		if ext <= 0 || ply >= maxPly-1 || !se.danger() {
			return se.leaf()
		}
		depth, ext = 1, ext-1
	}
	if ply >= maxPly-1 {
		return se.leaf()
	}
	alpha0 := alpha
	ttMove := board.Dir(255)
	if e := se.tt.probe(st.Hash); e != nil {
		ttMove = board.Dir(e.move)
		if int(e.depth) >= depth {
			v := fromTT(float64(e.value), ply, se.p.PlyStep)
			switch e.flag {
			case flagExact:
				return v
			case flagLower:
				if v >= beta {
					return v
				}
			case flagUpper:
				if v <= alpha {
					return v
				}
			}
		}
	}

	var moves [4]board.Dir
	n := st.SafeMoves(0, &moves)
	if n == 0 {
		moves[0], n = se.doomed(0), 1
	}
	hist := &se.hist[st.S[0].Head()]
	for a := 0; a < n; a++ {
		best := a
		for b := a + 1; b < n; b++ {
			x, y := moves[b], moves[best]
			if x == ttMove || (y != ttMove && hist[x] > hist[y]) {
				best = b
			}
		}
		moves[a], moves[best] = moves[best], moves[a]
	}

	best, bestMove := math.Inf(-1), moves[0]
	for k := 0; k < n; k++ {
		v := se.minNode(moves[k], depth, ply, ext, alpha, beta)
		if se.stop {
			return 0
		}
		if v > best {
			best, bestMove = v, moves[k]
		}
		if best > alpha {
			alpha = best
		}
		if alpha >= beta {
			hist[moves[k]] += int32(depth * depth)
			break
		}
	}
	flag := flagExact
	switch {
	case best <= alpha0:
		flag = flagUpper
	case best >= beta:
		flag = flagLower
	}
	se.tt.store(st.Hash, toTT(best, ply, se.p.PlyStep), depth, flag, bestMove)
	return best
}

// minNode: our move m is fixed; the adversarial opponents choose the joint
// reply that minimises our value, the rest play their predicted move.
func (se *Searcher) minNode(m board.Dir, depth, ply, ext int, alpha, beta float64) float64 {
	st, g := se.st, se.g
	var mv sim.Moves
	mv[0] = m
	var adv [sim.MaxSnakes]int
	na := se.adversaries(depth, &adv)
	var isAdv [sim.MaxSnakes]bool
	for k := 0; k < na; k++ {
		isAdv[adv[k]] = true
	}
	for j := 1; j < st.N; j++ {
		if st.S[j].Alive && !isAdv[j] {
			mv[j] = se.predict(j)
		}
	}

	// Our next cell: opponent moves toward it are tried first (likeliest refutations).
	target := g.Nbr[st.S[0].Head()][m&3]
	if target < 0 {
		target = st.S[0].Head()
	}
	var lists [sim.MaxSnakes][4]board.Dir
	var cnt [sim.MaxSnakes]int
	for k := 0; k < na; k++ {
		j := adv[k]
		c := st.SafeMoves(j, &lists[k])
		head := st.S[j].Head()
		if c == 0 {
			lists[k][0], c = se.doomed(j), 1
		} else if se.p.SearchRationalOpp && c > 1 && st.S[j].Len <= st.S[0].Len {
			// R2: an opponent that is not longer loses or trades any head-to-head
			// with us, so a rational one does not step where we might step. If
			// that leaves it nothing, it keeps every move.
			ours := st.S[0].Head()
			kept := 0
			for a := 0; a < c; a++ {
				if g.Dist(g.Nbr[head][lists[k][a]], ours) != 1 {
					lists[k][kept] = lists[k][a]
					kept++
				}
			}
			if kept > 0 {
				c = kept
			}
		}
		cnt[k] = c
		key := func(d board.Dir) int {
			if se.killerOK[ply] && se.killer[ply][j] == d {
				return -1
			}
			return g.Dist(g.Nbr[head][d], target)
		}
		for a := 0; a < c; a++ {
			best := a
			for b := a + 1; b < c; b++ {
				if key(lists[k][b]) < key(lists[k][best]) {
					best = b
				}
			}
			lists[k][a], lists[k][best] = lists[k][best], lists[k][a]
		}
	}

	var idx [sim.MaxSnakes]int
	worst := math.Inf(1)
	for {
		for k := 0; k < na; k++ {
			mv[adv[k]] = lists[k][idx[k]]
		}
		v := se.child(&mv, depth, ply, ext, alpha, math.Min(beta, worst))
		if se.stop {
			return 0
		}
		if v < worst {
			worst = v
		}
		if worst <= alpha {
			se.killer[ply], se.killerOK[ply] = mv, true
			break
		}
		k := 0
		for ; k < na; k++ {
			idx[k]++
			if idx[k] < cnt[k] {
				break
			}
			idx[k] = 0
		}
		if k == na {
			break
		}
	}
	return worst
}

// child resolves one joint action and scores the result.
func (se *Searcher) child(mv *sim.Moves, depth, ply, ext int, alpha, beta float64) float64 {
	if se.tick() {
		return 0
	}
	st := se.st
	u := &se.undo[ply]
	st.Make(mv, u)
	var v float64
	switch {
	case !st.S[0].Alive:
		v = se.lossValue(ply + 1)
	case !se.solo && st.AliveCount() == 1:
		v = se.winValue(ply + 1)
	default:
		v = se.maxNode(depth-1, ply+1, ext, alpha, beta)
	}
	st.Unmake(u)
	return v
}

// adversaries lists the opponents that may interact with us within depth
// turns (heads close enough to meet), nearest first, at most SearchMaxAdv.
func (se *Searcher) adversaries(depth int, out *[sim.MaxSnakes]int) int {
	st, g := se.st, se.g
	head := st.S[0].Head()
	limit := 2*depth + se.p.SearchAdvSlack
	var dist [sim.MaxSnakes]int
	n := 0
	for j := 1; j < st.N; j++ {
		if !st.S[j].Alive {
			continue
		}
		d := g.Dist(head, st.S[j].Head())
		if d > limit {
			continue
		}
		k := n
		for k > 0 && dist[k-1] > d {
			out[k], dist[k] = out[k-1], dist[k-1]
			k--
		}
		out[k], dist[k] = j, d
		n++
	}
	if n > se.p.SearchMaxAdv {
		n = se.p.SearchMaxAdv
	}
	return n
}

// predict is the cheap move model for opponents outside the envelope: most
// open next cell, food first when hungry, straight on ties.
func (se *Searcher) predict(j int) board.Dir {
	st, g := se.st, se.g
	var mv [4]board.Dir
	n := st.SafeMoves(j, &mv)
	switch n {
	case 0:
		return se.doomed(j)
	case 1:
		return mv[0]
	}
	sn := &st.S[j]
	head := sn.Head()
	straight := st.DefaultMove(j)
	hungry := int(sn.Health) <= se.p.PredictHungry
	best, bestScore := mv[0], math.MinInt32
	for k := 0; k < n; k++ {
		c := g.Nbr[head][mv[k]]
		score := 0
		for d := 0; d < 4; d++ {
			if q := g.Nbr[c][d]; q >= 0 && q != head && !st.BlockedNext(q) {
				score += 8
			}
		}
		if mv[k] == straight {
			score++
		}
		if hungry {
			if st.Food(c) {
				score += 64
			} else {
				score -= 2 * se.nearestFood(c)
			}
		}
		if score > bestScore {
			best, bestScore = mv[k], score
		}
	}
	return best
}

func (se *Searcher) nearestFood(c int16) int {
	best := se.g.W + se.g.H
	for _, f := range se.st.FoodSites() {
		if se.st.Food(f) {
			if d := se.g.Dist(c, f); d < best {
				best = d
			}
		}
	}
	return best
}

// doomed is the move for a snake with no safe move: stay on the board if possible.
func (se *Searcher) doomed(j int) board.Dir {
	st, g := se.st, se.g
	d := st.DefaultMove(j)
	head := st.S[j].Head()
	if g.Nbr[head][d] >= 0 {
		return d
	}
	for _, dd := range board.AllDirs {
		if g.Nbr[head][dd] >= 0 {
			return dd
		}
	}
	return d
}

// danger: an equal-or-longer live head within two cells of ours, where one
// more turn can decide a head-to-head (R2).
func (se *Searcher) danger() bool {
	st, g := se.st, se.g
	me := &st.S[0]
	for j := 1; j < st.N; j++ {
		o := &st.S[j]
		if o.Alive && o.Len >= me.Len && g.Dist(me.Head(), o.Head()) <= 2 {
			return true
		}
	}
	return false
}
