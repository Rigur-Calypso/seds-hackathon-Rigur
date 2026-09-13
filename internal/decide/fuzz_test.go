package decide_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	seds "github.com/Rigur-Calypso/seds-hackathon-Rigur"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/decide"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/fixture"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/search"
)

// FuzzDecide: for any payload the parser accepts, the full engine returns one
// of the four moves within its deadline and never needs its panic recovery.
func FuzzDecide(f *testing.F) {
	paths, _ := filepath.Glob("../../testdata/payloads/*.json")
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(b)
		}
	}
	if fx, err := fixture.LoadDir("../../testdata/fixtures"); err == nil {
		for _, x := range fx {
			if b, err := json.Marshal(x.State); err == nil {
				f.Add(b)
			}
		}
	}
	f.Add([]byte(`{"game":{"id":"f","ruleset":{"name":"royale"}},"board":{"width":3,"height":3,"hazards":[{"x":1,"y":1},{"x":1,"y":1}],"snakes":[{"id":"a","health":1,"body":[{"x":1,"y":1},{"x":1,"y":1},{"x":1,"y":1}]},{"id":"b","health":100,"body":[{"x":0,"y":0},{"x":0,"y":0}]}]},"you":{"id":"a"}}`))
	f.Add([]byte(`{"game":{"id":"w","ruleset":{"name":"wrapped_constrictor"}},"board":{"width":2,"height":2,"snakes":[{"id":"a","health":100,"body":[{"x":0,"y":0},{"x":1,"y":0},{"x":1,"y":1},{"x":1,"y":1}]}]},"you":{"id":"a"}}`))

	ps, err := config.Load(seds.ConfigFS)
	if err != nil {
		f.Fatal(err)
	}
	eng := decide.New(ps, search.Evaluate)
	f.Fuzz(func(t *testing.T, body []byte) {
		gs, err := api.Parse(body)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		start := time.Now()
		d := eng.Decide(ctx, gs)
		if _, ok := board.ParseDir(d.Move); !ok {
			t.Fatalf("invalid move %q", d.Move)
		}
		if d.Reason == decide.ReasonPanic {
			t.Fatalf("engine panicked: %s", d.Err)
		}
		if el := time.Since(start); el > 250*time.Millisecond {
			t.Fatalf("decision took %v", el)
		}
	})
}
