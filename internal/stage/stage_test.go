package stage

import (
	"testing"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name, ruleset, gameMap string
		w, h                   int
		want                   Stage
		profile                string
	}{
		{"qualifying", "standard", "standard", 11, 11, Qualifying, "qualifying"},
		{"bracket", "royale", "standard", 11, 11, Bracket, "royale"},
		{"final", "royale", "standard", 19, 19, Final, "duel"},
		{"royale-as-map", "standard", "royale", 11, 11, Bracket, "royale"},
		{"final-as-map", "standard", "royale", 19, 19, Final, "duel"},
		{"constrictor", "constrictor", "standard", 11, 11, Constrictor, "constrictor"},
		{"wrapped-constrictor", "wrapped_constrictor", "standard", 11, 11, Constrictor, "constrictor"},
		{"unknown-defaults", "solo", "", 7, 7, Qualifying, "qualifying"},
	}
	for _, c := range cases {
		gs := &api.GameState{}
		gs.Game.Ruleset.Name, gs.Game.Map = c.ruleset, c.gameMap
		gs.Board.Width, gs.Board.Height = c.w, c.h
		if got := Classify(gs); got != c.want || got.Profile() != c.profile {
			t.Errorf("%s: got %v/%s want %v/%s", c.name, got, got.Profile(), c.want, c.profile)
		}
	}
}
