// Command lossreport turns the snake's own server logs into learning material.
//
// Paste Render log lines (the JSON "end" lines of lost games) into a file, then:
//
//	go run ./cmd/lossreport -in losses.log                     # report
//	go run ./cmd/lossreport -in losses.log -out testdata/losses  # also write fixtures
//
// For every lost game it replays the positions leading up to the loss
// ("recent_boards"; older deploys only logged "fatal_board") through the
// current engine with a deep search, scores every safe move on its own, and
// flags the turns where the move that was played is a proven loss while
// another move is not. Those are the decisions worth turning into fixtures:
// with -out, each flagged position is written with the played move and a
// commented `reject` line to review before moving it into testdata/fixtures.
//
// Local tooling only: never deployed, never imports the AGPL rules.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	seds "github.com/Rigur-Calypso/seds-hackathon-Rigur"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/brain"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/legal"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/stage"
)

// frame is one answered position as the server logs it (opponent.Frame).
type frame struct {
	Turn  int    `json:"turn"`
	Move  string `json:"move"`
	Board string `json:"board"`
}

type endLine struct {
	Msg        string  `json:"msg"`
	Game       string  `json:"game"`
	Result     string  `json:"result"`
	FatalTurn  int     `json:"fatal_turn"`
	FatalMove  string  `json:"fatal_move"`
	FatalBoard string  `json:"fatal_board"`
	Recent     []frame `json:"recent_boards"`
}

// loss is one game that was not won, with the positions logged before its end.
type loss struct {
	Game   string
	Result string
	Frames []frame
}

// parseLog extracts every non-win "end" record. Lines may carry a prefix (the
// Render dashboard adds timestamps); the JSON object is taken from the first
// '{' to the last '}'. A game logged twice is reported once.
func parseLog(r io.Reader) ([]loss, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	seen := map[string]bool{}
	var out []loss
	for sc.Scan() {
		line := sc.Text()
		i, j := strings.IndexByte(line, '{'), strings.LastIndexByte(line, '}')
		if i < 0 || j <= i {
			continue
		}
		var e endLine
		if json.Unmarshal([]byte(line[i:j+1]), &e) != nil || e.Msg != "end" || e.Result == "" || e.Result == "win" {
			continue
		}
		frames := e.Recent
		if len(frames) == 0 && e.FatalBoard != "" {
			frames = []frame{{Turn: e.FatalTurn, Move: e.FatalMove, Board: e.FatalBoard}}
		}
		if len(frames) == 0 || seen[e.Game] {
			continue
		}
		seen[e.Game] = true
		out = append(out, loss{Game: e.Game, Result: e.Result, Frames: frames})
	}
	return out, sc.Err()
}

type moveValue struct {
	Move  string
	Value float64
}

// verdict is the deep-search judgement of one logged position.
type verdict struct {
	Turn    int
	Played  string
	Values  []moveValue // best first
	Mistake bool        // the played move is a proven loss and another move is not
}

// analyse scores every safe move of one position with its own deep search.
func analyse(ctx context.Context, ps *config.Profiles, f frame, nodes int) (verdict, error) {
	v := verdict{Turn: f.Turn, Played: f.Move}
	fx, err := fixture.Parse(fmt.Sprintf("turn-%d", f.Turn), f.Board)
	if err != nil {
		return v, err
	}
	s, ok := board.FromAPI(fx.State)
	if !ok {
		return v, errors.New("board has no snake 'you'")
	}
	p := *ps.Get(stage.Classify(fx.State).Profile())
	p.Engine, p.SearchNodes = "v2", nodes
	for _, m := range legal.Safe(s, 0) {
		res, err := brain.Search(ctx, s, &p, []board.Dir{m})
		if err != nil || len(res.Scores) == 0 {
			continue
		}
		v.Values = append(v.Values, moveValue{m.String(), res.Scores[0].Value})
	}
	sort.SliceStable(v.Values, func(a, b int) bool { return v.Values[a].Value > v.Values[b].Value })
	if len(v.Values) == 0 || brain.IsLoss(v.Values[0].Value) {
		return v, nil // nothing better existed
	}
	v.Mistake = true // the played move was unsafe, unless it scores as well as the rest
	for _, mv := range v.Values {
		if mv.Move == f.Move {
			v.Mistake = brain.IsLoss(mv.Value)
		}
	}
	return v, nil
}

func (v verdict) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "t=%-4d played=%-5s", v.Turn, v.Played)
	for _, mv := range v.Values {
		fmt.Fprintf(&b, " %s=%.3f", mv.Move, mv.Value)
	}
	if v.Mistake {
		b.WriteString("  <- MISTAKE: played move is a proven loss, another is not")
	}
	return b.String()
}

func writeFixture(dir, game string, f frame, v verdict) (string, error) {
	short := game
	if len(short) > 8 {
		short = short[:8]
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-t%d.txt", short, f.Turn))
	var b strings.Builder
	fmt.Fprintf(&b, "# lossreport: game %s, turn %d, played %s\n# deep-search values:", game, f.Turn, f.Move)
	for _, mv := range v.Values {
		fmt.Fprintf(&b, " %s=%.3f", mv.Move, mv.Value)
	}
	fmt.Fprintf(&b, "\n# Check the board, then uncomment to keep it as a regression test in testdata/fixtures:\n# reject %s\n%s", f.Move, f.Board)
	return path, os.WriteFile(path, []byte(b.String()), 0o644)
}

func main() {
	in := flag.String("in", "", "file of pasted log lines (default stdin)")
	out := flag.String("out", "", "write every flagged position as a fixture file into this directory")
	nodes := flag.Int("nodes", 50000, "search nodes per move (deeper than live play on purpose)")
	flag.Parse()

	var r io.Reader = os.Stdin
	if *in != "" {
		f, err := os.Open(*in)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		r = f
	}
	losses, err := parseLog(r)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ps, err := config.Load(seds.ConfigFS)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *out != "" {
		if err := os.MkdirAll(*out, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	ctx := context.Background()
	withMistake := 0
	for _, l := range losses {
		fmt.Printf("game %s (%s), %d positions\n", l.Game, l.Result, len(l.Frames))
		found := false
		for _, f := range l.Frames {
			v, err := analyse(ctx, ps, f, *nodes)
			if err != nil {
				fmt.Printf("  t=%d: %v\n", f.Turn, err)
				continue
			}
			fmt.Println("  " + v.String())
			if v.Mistake {
				found = true
				if *out != "" {
					if path, err := writeFixture(*out, l.Game, f, v); err != nil {
						fmt.Fprintln(os.Stderr, err)
					} else {
						fmt.Println("    wrote " + path)
					}
				}
			}
		}
		if found {
			withMistake++
		}
	}
	fmt.Printf("\n%d lost games, %d with a flagged mistake in the logged positions\n", len(losses), withMistake)
}
