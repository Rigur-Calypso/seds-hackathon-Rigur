// Package rules is OUR clean-room one-turn resolver. It reproduces the engine
// pipeline documented in CLAUDE.md §2 (verified against BattlesnakeOfficial/rules
// v1.2.3 behaviour, no code copied):
//
//	Movement → Starvation(-1) → HazardDamage → FeedSnakes → Elimination → [Constrictor grow] → [Royale hazards]
//
// The GameOver stage is exposed separately (GameOver) because planning never
// resolves finished games.
package rules

import "github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"

// ShrinkMode says how to regenerate royale hazards on a shrink turn. R8: the
// shrink direction is not in the request and must never be predicted.
type ShrinkMode uint8

const (
	// ShrinkKeep leaves hazards unchanged (optimistic, cheap).
	ShrinkKeep ShrinkMode = iota
	// ShrinkPessimistic hazards the union of all four possible next rings.
	ShrinkPessimistic
	// ShrinkExact removes the line on Options.Side (tests / differential only).
	ShrinkExact
)

// Options tunes resolution.
type Options struct {
	Shrink ShrinkMode
	Side   int
}

// SnakeMaxHealth is the engine health cap.
const SnakeMaxHealth = 100

// GameOver mirrors the engine's first stage: a non-solo game ends when at most
// one snake is alive, a solo game when none are.
func GameOver(s *board.State, solo bool) bool {
	n := s.AliveCount()
	if solo {
		return n == 0
	}
	return n <= 1
}

// Resolve applies one simultaneous turn. moves[i] is snake i's move (ignored
// for eliminated snakes; missing entries default to Up). The input is not
// modified.
func Resolve(s *board.State, moves []board.Dir, opt Options) *board.State {
	n := s.Clone()
	n.Turn = s.Turn + 1
	moveSnakes(n, moves)
	starve(n)
	damageHazards(n)
	feed(n)
	eliminate(n)
	if n.Rules.Constrictor {
		constrict(n)
	}
	if n.Rules.Royale {
		regenerateRoyale(s, n, opt)
	}
	return n
}

// moveSnakes prepends the new head and pops the tail in place. R4 falls out of
// this: a duplicated tail leaves its twin behind.
func moveSnakes(n *board.State, moves []board.Dir) {
	for i := range n.Snakes {
		sn := &n.Snakes[i]
		if !sn.Alive() || len(sn.Body) == 0 {
			continue
		}
		d := board.Up
		if i < len(moves) {
			d = moves[i]
		}
		head := sn.Body[0].Add(d)
		if n.Rules.Wrapped {
			head.X = ((head.X % n.W) + n.W) % n.W
			head.Y = ((head.Y % n.H) + n.H) % n.H
		}
		copy(sn.Body[1:], sn.Body[:len(sn.Body)-1])
		sn.Body[0] = head
	}
}

// starve: every live snake loses 1 health before feeding (R1).
func starve(n *board.State) {
	for i := range n.Snakes {
		if n.Snakes[i].Alive() {
			n.Snakes[i].Health--
		}
	}
}

// damageHazards is R5 + R6: a hazard square that holds food deals no damage at
// all; otherwise damage is applied once per hazard entry, clamped to [0,100],
// and the snake is eliminated inline — before feeding.
func damageHazards(n *board.State) {
	if n.Hazard == nil {
		return
	}
	dmg := n.Rules.HazardDamage
	for i := range n.Snakes {
		sn := &n.Snakes[i]
		if !sn.Alive() {
			continue
		}
		h := sn.Body[0]
		cnt := n.HazardAt(h)
		if cnt == 0 || n.HasFood(h) {
			continue
		}
		for k := 0; k < cnt; k++ {
			sn.Health -= dmg
			if sn.Health < 0 {
				sn.Health = 0
			}
			if sn.Health > SnakeMaxHealth {
				sn.Health = SnakeMaxHealth
			}
			if sn.Health <= 0 {
				sn.Cause, sn.ElimTurn = board.ByHazard, n.Turn
			}
		}
	}
}

// feed: every live snake whose head is on food grows (duplicate tail) and resets
// to full health (R1: a snake arriving at exactly 0 health survives). Several
// snakes can eat the same food; it is consumed.
func feed(n *board.State) {
	if len(n.Food) == 0 {
		return
	}
	kept := n.Food[:0]
	for _, f := range n.Food {
		eaten := false
		for i := range n.Snakes {
			sn := &n.Snakes[i]
			if !sn.Alive() || len(sn.Body) == 0 || sn.Body[0] != f {
				continue
			}
			sn.Body = append(sn.Body, sn.Body[len(sn.Body)-1])
			sn.Health = SnakeMaxHealth
			eaten = true
		}
		if !eaten {
			kept = append(kept, f)
		}
	}
	n.Food = kept
}

// eliminate runs the engine's elimination stage.
//
// Phase 1: out of health, then out of bounds.
// Phase 2: collisions, checked only against snakes still alive after phase 1 —
// R12 (found in engine source, not in CLAUDE.md): a snake that starved, took
// lethal hazard damage or left the board this turn does NOT block anyone.
// Collision eliminations are collected and applied together (R7), so mutual
// destruction in one tick is possible. Self-collision is checked first, then
// body collision, then head-to-head. R3: heads (index 0) are not body cells.
// R2: len(me) <= len(them) loses a head-to-head.
func eliminate(n *board.State) {
	for i := range n.Snakes {
		sn := &n.Snakes[i]
		if !sn.Alive() {
			continue
		}
		if sn.Health <= 0 {
			sn.Cause, sn.ElimTurn = board.ByOutOfHealth, n.Turn
			continue
		}
		if !n.InBounds(sn.Body[0]) {
			sn.Cause, sn.ElimTurn = board.ByOutOfBounds, n.Turn
		}
	}

	var pending [16]struct {
		i     int
		cause board.Cause
	}
	np := 0
	push := func(i int, c board.Cause) {
		if np < len(pending) {
			pending[np].i, pending[np].cause = i, c
			np++
		} else {
			n.Snakes[i].Cause, n.Snakes[i].ElimTurn = c, n.Turn
		}
	}
	for i := range n.Snakes {
		me := &n.Snakes[i]
		if !me.Alive() {
			continue
		}
		h := me.Body[0]
		if bodyHit(me, h) {
			push(i, board.BySelfCollision)
			continue
		}
		hit := false
		for j := range n.Snakes {
			if j != i && n.Snakes[j].Alive() && bodyHit(&n.Snakes[j], h) {
				push(i, board.ByCollision)
				hit = true
				break
			}
		}
		if hit {
			continue
		}
		for j := range n.Snakes {
			o := &n.Snakes[j]
			if j != i && o.Alive() && o.Body[0] == h && len(me.Body) <= len(o.Body) {
				push(i, board.ByHeadToHead)
				break
			}
		}
	}
	for k := 0; k < np; k++ {
		sn := &n.Snakes[pending[k].i]
		sn.Cause, sn.ElimTurn = pending[k].cause, n.Turn
	}
}

// bodyHit: does h land on any of other's segments except its head (R3)?
func bodyHit(other *board.Snake, h board.Point) bool {
	for k := 1; k < len(other.Body); k++ {
		if other.Body[k] == h {
			return true
		}
	}
	return false
}

// constrict is R9: food cleared, every snake (the engine does not skip
// eliminated ones) pinned to full health and force-grown unless its tail is
// already duplicated. Note hazard damage still applied earlier this turn.
func constrict(n *board.State) {
	n.Food = nil
	for i := range n.Snakes {
		sn := &n.Snakes[i]
		if len(sn.Body) < 2 {
			continue
		}
		sn.Health = SnakeMaxHealth
		if sn.Body[len(sn.Body)-1] != sn.Body[len(sn.Body)-2] {
			sn.Body = append(sn.Body, sn.Body[len(sn.Body)-1])
		}
	}
}

// regenerateRoyale is R8. The engine regenerates the ring from
// numShrinks = turn / shrinkEveryNTurns, where turn is the NEW turn number. The
// ring only changes when that count increments (turn % N == 0).
func regenerateRoyale(prev, n *board.State, opt Options) {
	N := n.Rules.ShrinkEveryN
	if N < 1 {
		return
	}
	if n.Turn < N {
		n.Hazard = nil
		return
	}
	if n.Turn%N != 0 {
		return
	}
	r := board.SafeRect(prev)
	switch opt.Shrink {
	case ShrinkKeep:
		return
	case ShrinkPessimistic:
		r = r.ShrinkAll()
	case ShrinkExact:
		r = r.Shrink(opt.Side)
	}
	n.Hazard = board.RingHazards(n.W, n.H, r)
}
