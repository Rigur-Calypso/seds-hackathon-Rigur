package api_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
)

// FuzzParse: malformed payloads never panic, and anything Parse accepts is
// normalised — sane board size, positive timeout and shrink cadence, every
// snake with a body whose head and length match. `go test` runs the seed
// corpus; `go test -fuzz FuzzParse ./internal/api` explores further.
func FuzzParse(f *testing.F) {
	paths, _ := filepath.Glob("../../testdata/payloads/*.json")
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(b)
		}
	}
	for _, s := range []string{
		"", "{}", "null", "[]", `"move"`,
		`{"board":{"width":-5,"height":3}}`,
		`{"board":{"snakes":[{"body":[]}]},"you":{"body":[]}}`,
		`{"you":{"latency":{}}}`,
		`{"game":{"timeout":-1,"ruleset":{"settings":{"royale":{"shrinkEveryNTurns":-3}}}}}`,
		`{"board":{"width":11,"height":11,"snakes":[{"id":"a","body":[{"x":99999,"y":-4},{"x":1,"y":1}]}]},"you":{"id":"a"}}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		gs, err := api.Parse(body)
		if err != nil {
			return
		}
		if gs.Board.Width <= 0 || gs.Board.Height <= 0 || gs.Board.Width > api.MaxBoardSide || gs.Board.Height > api.MaxBoardSide {
			t.Fatalf("board %dx%d accepted", gs.Board.Width, gs.Board.Height)
		}
		if gs.Game.Timeout <= 0 || gs.Game.Ruleset.Settings.Royale.ShrinkEveryNTurns <= 0 {
			t.Fatalf("timeout %d / shrink %d not normalised", gs.Game.Timeout, gs.Game.Ruleset.Settings.Royale.ShrinkEveryNTurns)
		}
		for _, sn := range gs.Board.Snakes {
			if len(sn.Body) == 0 || sn.Length != len(sn.Body) || sn.Head != sn.Body[0] {
				t.Fatalf("snake not normalised: %+v", sn)
			}
		}
	})
}
