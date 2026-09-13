// Package fixture is a tiny line-based DSL for board positions, used by golden
// tests, decision fixtures and fatal-board logging. One directive per line:
//
//	rules standard|royale|constrictor|wrapped    map royale
//	size 11 11     turn 10     timeout 500     damage 14     shrink 25
//	you 90 5,5 5,4 5,3               # health, then body head-first
//	snake bob 90 7,5 7,4 7,3         # name, health, body
//	food 3,3 9,9                     hazard 0,0 0,1
//	safe 1 1 9 9                     # royale ring: hazard outside this rect
//	expect up left                   # decision must be one of these
//	reject down                      # decision must not be this
package fixture

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
)

// Fixture is a parsed position plus assertions.
type Fixture struct {
	Name   string
	State  *api.GameState
	Expect []string
	Reject []string
}

// Parse parses DSL text.
func Parse(name, text string) (*Fixture, error) {
	gs := &api.GameState{}
	gs.Game = api.Game{ID: "fixture-" + name, Timeout: 500, Map: "standard", Ruleset: api.Ruleset{
		Name: "standard",
		Settings: api.RulesetSettings{FoodSpawnChance: 15, MinimumFood: 1, HazardDamagePerTurn: 14,
			Royale: api.RoyaleSettings{ShrinkEveryNTurns: 25}},
	}}
	gs.Board.Width, gs.Board.Height = 11, 11
	gs.Turn = 10
	f := &Fixture{Name: name, State: gs}
	var safe []int
	for ln, raw := range strings.Split(text, "\n") {
		line := raw
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fs := strings.Fields(line)
		if len(fs) == 0 {
			continue
		}
		bad := func(err error) error { return fmt.Errorf("%s:%d: %q: %v", name, ln+1, strings.TrimSpace(raw), err) }
		ints := func(args []string) ([]int, error) {
			out := make([]int, len(args))
			for i, a := range args {
				v, err := strconv.Atoi(a)
				if err != nil {
					return nil, err
				}
				out[i] = v
			}
			return out, nil
		}
		switch fs[0] {
		case "rules":
			gs.Game.Ruleset.Name = fs[1]
		case "map":
			gs.Game.Map = fs[1]
		case "size", "turn", "timeout", "damage", "shrink", "safe":
			v, err := ints(fs[1:])
			if err != nil || len(v) == 0 {
				return nil, bad(fmt.Errorf("want integers"))
			}
			switch fs[0] {
			case "size":
				if len(v) != 2 {
					return nil, bad(fmt.Errorf("want W H"))
				}
				gs.Board.Width, gs.Board.Height = v[0], v[1]
			case "turn":
				gs.Turn = v[0]
			case "timeout":
				gs.Game.Timeout = v[0]
			case "damage":
				gs.Game.Ruleset.Settings.HazardDamagePerTurn = v[0]
			case "shrink":
				gs.Game.Ruleset.Settings.Royale.ShrinkEveryNTurns = v[0]
			case "safe":
				if len(v) != 4 {
					return nil, bad(fmt.Errorf("want minX minY maxX maxY"))
				}
				safe = v
			}
		case "you", "snake":
			args := fs[1:]
			sn := api.Snake{ID: "you", Name: "you", Latency: 50}
			if fs[0] == "snake" {
				if len(args) < 1 {
					return nil, bad(fmt.Errorf("missing name"))
				}
				sn.Name = args[0]
				sn.ID = fmt.Sprintf("s%d", len(gs.Board.Snakes))
				args = args[1:]
			}
			if len(args) < 2 {
				return nil, bad(fmt.Errorf("want health and body"))
			}
			h, err := strconv.Atoi(args[0])
			if err != nil {
				return nil, bad(err)
			}
			sn.Health = h
			body, err := coords(args[1:])
			if err != nil {
				return nil, bad(err)
			}
			sn.Body = body
			sn.Head, sn.Length = body[0], len(body)
			if fs[0] == "you" {
				gs.You = sn
				gs.Board.Snakes = append([]api.Snake{sn}, gs.Board.Snakes...)
			} else {
				gs.Board.Snakes = append(gs.Board.Snakes, sn)
			}
		case "food", "hazard":
			cs, err := coords(fs[1:])
			if err != nil {
				return nil, bad(err)
			}
			if fs[0] == "food" {
				gs.Board.Food = append(gs.Board.Food, cs...)
			} else {
				gs.Board.Hazards = append(gs.Board.Hazards, cs...)
			}
		case "expect":
			f.Expect = append(f.Expect, fs[1:]...)
		case "reject":
			f.Reject = append(f.Reject, fs[1:]...)
		default:
			return nil, bad(fmt.Errorf("unknown directive"))
		}
	}
	if safe != nil {
		r := board.Rect{MinX: safe[0], MinY: safe[1], MaxX: safe[2], MaxY: safe[3]}
		for y := 0; y < gs.Board.Height; y++ {
			for x := 0; x < gs.Board.Width; x++ {
				if !r.Contains(board.Point{X: x, Y: y}) {
					gs.Board.Hazards = append(gs.Board.Hazards, api.Coord{X: x, Y: y})
				}
			}
		}
	}
	api.Normalize(gs)
	return f, nil
}

func coords(args []string) ([]api.Coord, error) {
	out := make([]api.Coord, 0, len(args))
	for _, a := range args {
		parts := strings.Split(a, ",")
		if len(parts) != 2 {
			return nil, fmt.Errorf("bad coord %q", a)
		}
		x, err1 := strconv.Atoi(parts[0])
		y, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("bad coord %q", a)
		}
		out = append(out, api.Coord{X: x, Y: y})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no coordinates")
	}
	return out, nil
}

// MustState parses DSL text into a GameState or panics (tests only).
func MustState(text string) *api.GameState {
	f, err := Parse("inline", text)
	if err != nil {
		panic(err)
	}
	return f.State
}

// LoadDir loads every *.txt fixture in dir, sorted by name.
func LoadDir(dir string) ([]*Fixture, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []*Fixture
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		f, err := Parse(strings.TrimSuffix(filepath.Base(p), ".txt"), string(b))
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// Format renders a request as DSL so a fatal board from production logs can be
// pasted straight into testdata/fixtures. Royale rings compress to "safe".
func Format(gs *api.GameState) string {
	var b strings.Builder
	rs := gs.Game.Ruleset
	fmt.Fprintf(&b, "rules %s\n", orStandard(rs.Name))
	if gs.Game.Map != "" && gs.Game.Map != "standard" {
		fmt.Fprintf(&b, "map %s\n", gs.Game.Map)
	}
	fmt.Fprintf(&b, "size %d %d\nturn %d\ntimeout %d\ndamage %d\nshrink %d\n",
		gs.Board.Width, gs.Board.Height, gs.Turn, gs.Game.Timeout, rs.Settings.HazardDamagePerTurn, rs.Settings.Royale.ShrinkEveryNTurns)
	for _, sn := range gs.Board.Snakes {
		if sn.ID == gs.You.ID {
			fmt.Fprintf(&b, "you %d %s\n", sn.Health, joinCoords(sn.Body))
		}
	}
	for _, sn := range gs.Board.Snakes {
		if sn.ID != gs.You.ID {
			fmt.Fprintf(&b, "snake %s %d %s\n", sanitize(sn.Name), sn.Health, joinCoords(sn.Body))
		}
	}
	if len(gs.Board.Food) > 0 {
		fmt.Fprintf(&b, "food %s\n", joinCoords(gs.Board.Food))
	}
	if len(gs.Board.Hazards) > 0 {
		if r, ok := ringRect(gs); ok {
			fmt.Fprintf(&b, "safe %d %d %d %d\n", r.MinX, r.MinY, r.MaxX, r.MaxY)
		} else {
			fmt.Fprintf(&b, "hazard %s\n", joinCoords(gs.Board.Hazards))
		}
	}
	return b.String()
}

func orStandard(s string) string {
	if s == "" {
		return "standard"
	}
	return s
}

func sanitize(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '#' || r == '\n' {
			return '_'
		}
		return r
	}, name)
	if name == "" {
		return "anon"
	}
	return name
}

func joinCoords(cs []api.Coord) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = fmt.Sprintf("%d,%d", c.X, c.Y)
	}
	return strings.Join(parts, " ")
}

func ringRect(gs *api.GameState) (board.Rect, bool) {
	w, h := gs.Board.Width, gs.Board.Height
	if w <= 0 || h <= 0 || w > api.MaxBoardSide || h > api.MaxBoardSide {
		return board.Rect{}, false
	}
	s := &board.State{W: w, H: h, Hazard: make([]uint8, w*h)}
	for _, c := range gs.Board.Hazards {
		p := board.Point{X: c.X, Y: c.Y}
		if !s.InBounds(p) {
			return board.Rect{}, false
		}
		s.Hazard[s.Idx(p)]++
	}
	r := board.SafeRect(s)
	ring := board.RingHazards(w, h, r)
	for i := range ring {
		if ring[i] != s.Hazard[i] {
			return board.Rect{}, false
		}
	}
	return r, true
}
