package api_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/board"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/stage"
)

const royale = `{"game":{"id":"g1","ruleset":{"name":"royale","version":"v1.2.3","settings":{"foodSpawnChance":15,"minimumFood":1,"hazardDamagePerTurn":14,"royale":{"shrinkEveryNTurns":20}}},"map":"standard","timeout":350},
"turn":5,"board":{"height":11,"width":11,"food":[{"x":1,"y":1}],"hazards":[],
"snakes":[{"id":"a","name":"A","latency":"123","health":90,"body":[{"x":5,"y":5},{"x":5,"y":4},{"x":5,"y":3}],"head":{"x":5,"y":5},"length":3}]},
"you":{"id":"a","name":"A","latency":"123","health":90,"body":[{"x":5,"y":5},{"x":5,"y":4},{"x":5,"y":3}],"head":{"x":5,"y":5},"length":3}}`

// R10: shrinkEveryNTurns is nested under settings.royale; timeout comes from the request.
func TestParseNestedSettings(t *testing.T) {
	gs, err := api.Parse([]byte(royale))
	if err != nil {
		t.Fatal(err)
	}
	set := gs.Game.Ruleset.Settings
	if set.Royale.ShrinkEveryNTurns != 20 || set.HazardDamagePerTurn != 14 || gs.Game.Timeout != 350 {
		t.Fatalf("settings %+v timeout %d", set, gs.Game.Timeout)
	}
	if gs.You.Latency != 123 || gs.Board.Snakes[0].Latency != 123 {
		t.Fatal("R11: latency string must parse")
	}
	if stage.Classify(gs) != stage.Bracket {
		t.Fatal("royale 11x11 is the bracket")
	}
}

// R10: a root-level shrinkEveryNTurns is the wrong path and must not be read.
func TestParseShrinkAtRootIsIgnored(t *testing.T) {
	body := strings.Replace(royale, `"royale":{"shrinkEveryNTurns":20}`, `"shrinkEveryNTurns":20`, 1)
	gs, err := api.Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if got := gs.Game.Ruleset.Settings.Royale.ShrinkEveryNTurns; got != api.DefaultShrinkEvery {
		t.Fatalf("root-level shrink must fall back to default, got %d", got)
	}
}

func TestParseDefaults(t *testing.T) {
	gs, err := api.Parse([]byte(`{"game":{"id":"x","ruleset":{"name":"Royale"}},"board":{"snakes":[{"id":"a","health":50,"body":[{"x":3,"y":4}]}]},"you":{"id":"a","body":[{"x":3,"y":4}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	set := gs.Game.Ruleset.Settings
	if gs.Game.Timeout != 500 || set.HazardDamagePerTurn != 14 || set.Royale.ShrinkEveryNTurns != 25 {
		t.Fatalf("defaults not applied: %+v timeout=%d", set, gs.Game.Timeout)
	}
	if gs.Game.Ruleset.Name != "royale" || gs.Board.Width != 4 || gs.Board.Height != 5 {
		t.Fatalf("normalisation: name=%q w=%d h=%d", gs.Game.Ruleset.Name, gs.Board.Width, gs.Board.Height)
	}
	gs, _ = api.Parse([]byte(`{"game":{"ruleset":{"settings":{"hazardDamagePerTurn":0}}},"board":{"width":11,"height":11}}`))
	if gs.Game.Ruleset.Settings.HazardDamagePerTurn != 0 {
		t.Fatal("explicit 0 damage must be kept")
	}
}

func TestParseLatencyForms(t *testing.T) {
	for _, l := range []string{`"42"`, `42`, `""`, `null`, `"abc"`} {
		body := `{"board":{"width":3,"height":3,"snakes":[{"id":"a","latency":` + l + `,"body":[{"x":0,"y":0}]}]},"you":{"id":"a","body":[{"x":0,"y":0}]}}`
		if _, err := api.Parse([]byte(body)); err != nil {
			t.Fatalf("latency %s: %v", l, err)
		}
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, b := range []string{"", "   ", "not json", `{"board":{"width":1000,"height":1000}}`} {
		if _, err := api.Parse([]byte(b)); err == nil {
			t.Fatalf("%q must fail", b)
		}
	}
}

// Real payloads captured from the official CLI (testdata/payloads). The file
// prefix states the expected ruleset and stage.
func TestCapturedPayloads(t *testing.T) {
	paths, _ := filepath.Glob("../../testdata/payloads/*.json")
	if len(paths) == 0 {
		t.Skip("no captured payloads")
	}
	want := map[string]stage.Stage{"standard": stage.Qualifying, "royale11": stage.Bracket, "royale19": stage.Final, "constrictor": stage.Constrictor}
	seen := map[string]bool{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		gs, err := api.Parse(b)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		prefix := strings.SplitN(filepath.Base(p), "_", 2)[0]
		exp, ok := want[prefix]
		if !ok {
			continue
		}
		seen[prefix] = true
		if got := stage.Classify(gs); got != exp {
			t.Errorf("%s: stage %v want %v", p, got, exp)
		}
		if _, ok := board.FromAPI(gs); !ok {
			t.Errorf("%s: you not found on board", p)
		}
		if prefix != "standard" && prefix != "constrictor" {
			if gs.Game.Ruleset.Settings.Royale.ShrinkEveryNTurns <= 0 || gs.Game.Ruleset.Settings.HazardDamagePerTurn <= 0 {
				t.Errorf("%s: royale settings missing %+v", p, gs.Game.Ruleset.Settings)
			}
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("missing captured payload for %s", k)
		}
	}
}
