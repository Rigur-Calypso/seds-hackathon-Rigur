// Package stage classifies a request into a tournament stage (CLAUDE.md §4).
// The stage is read off the game state, never guessed.
package stage

import (
	"strings"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/api"
)

// Stage is the tournament stage.
type Stage int

const (
	Qualifying Stage = iota
	Bracket
	Final
	Constrictor
)

func (s Stage) String() string {
	switch s {
	case Qualifying:
		return "qualifying"
	case Bracket:
		return "bracket"
	case Final:
		return "final"
	case Constrictor:
		return "constrictor"
	}
	return "unknown"
}

// Profile is the config profile name for the stage.
func (s Stage) Profile() string {
	switch s {
	case Bracket:
		return "royale"
	case Final:
		return "duel"
	case Constrictor:
		return "constrictor"
	}
	return "qualifying"
}

// IsRoyale reports royale from either the ruleset name or the map id. The live
// engine can express royale as a map ("map":"royale") on top of a ruleset, so
// checking only the ruleset name would silently misclassify the bracket.
func IsRoyale(g *api.GameState) bool {
	return strings.EqualFold(g.Game.Ruleset.Name, "royale") || strings.EqualFold(g.Game.Map, "royale")
}

// Classify maps a request to its stage.
func Classify(g *api.GameState) Stage {
	name := strings.ToLower(g.Game.Ruleset.Name)
	switch {
	case strings.Contains(name, "constrictor"):
		return Constrictor
	case IsRoyale(g) && (g.Board.Width >= 19 || g.Board.Height >= 19):
		return Final
	case IsRoyale(g):
		return Bracket
	default:
		return Qualifying
	}
}
