package brain

import "github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"

const (
	flagExact uint8 = iota + 1
	flagLower
	flagUpper
)

type entry struct {
	key   uint64
	value float32
	gen   uint16
	depth int8
	flag  uint8
	move  uint8
}

// table is a direct-mapped transposition table. A generation stamp invalidates
// every entry between searches without clearing memory.
type table struct {
	e    []entry
	mask uint64
	gen  uint16
}

func acquireTable(t *table, bits int) *table {
	if bits < 10 {
		bits = 10
	}
	if bits > 24 {
		bits = 24
	}
	size := 1 << bits
	if t == nil || len(t.e) != size {
		t = &table{e: make([]entry, size), mask: uint64(size - 1)}
	}
	t.gen++
	if t.gen == 0 {
		for i := range t.e {
			t.e[i] = entry{}
		}
		t.gen = 1
	}
	return t
}

func (t *table) probe(key uint64) *entry {
	e := &t.e[key&t.mask]
	if e.gen == t.gen && e.key == key && e.flag != 0 {
		return e
	}
	return nil
}

// store keeps the deeper of two different positions competing for a slot.
func (t *table) store(key uint64, v float64, depth int, flag uint8, move board.Dir) {
	e := &t.e[key&t.mask]
	if e.gen == t.gen && e.key != key && int(e.depth) > depth {
		return
	}
	*e = entry{key: key, value: float32(v), gen: t.gen, depth: int8(depth), flag: flag, move: uint8(move)}
}

// Terminal values carry the ply they happen at; the table stores them relative
// to the node so a transposition at another ply reads the right distance.
func toTT(v float64, ply int, step float64) float64 {
	switch {
	case IsLoss(v):
		return v - step*float64(ply)
	case IsWin(v):
		return v + step*float64(ply)
	}
	return v
}

func fromTT(v float64, ply int, step float64) float64 {
	switch {
	case IsLoss(v):
		return v + step*float64(ply)
	case IsWin(v):
		return v - step*float64(ply)
	}
	return v
}
