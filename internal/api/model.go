// Package api holds the Battlesnake wire types. Field paths follow the official
// client models exactly (R10, R11).
package api

import (
	"bytes"
	"strconv"
)

// Coord is a board coordinate. (0,0) is bottom-left and "up" is +Y.
type Coord struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Latency is R11: the engine sends it as a string ("123"). We also tolerate a
// bare number, null or garbage, because a latency field must never fail a parse.
type Latency int

// UnmarshalJSON accepts "123", 123, "", null.
func (l *Latency) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(bytes.TrimSpace(b), `"`)
	v, err := strconv.Atoi(string(b))
	if err != nil {
		v = 0
	}
	*l = Latency(v)
	return nil
}

// MarshalJSON writes the engine's string form.
func (l Latency) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(strconv.Itoa(int(l)))), nil
}

// Snake is one snake on the board.
type Snake struct {
	ID      string  `json:"id"`   // R11: game-scoped
	Name    string  `json:"name"` // R11: stable across games
	Latency Latency `json:"latency"`
	Health  int     `json:"health"`
	Body    []Coord `json:"body"`
	Head    Coord   `json:"head"`
	Length  int     `json:"length"`
	Shout   string  `json:"shout,omitempty"`
	Squad   string  `json:"squad,omitempty"`
}

// Board is the board state.
type Board struct {
	Height  int     `json:"height"`
	Width   int     `json:"width"`
	Food    []Coord `json:"food"`
	Hazards []Coord `json:"hazards"`
	Snakes  []Snake `json:"snakes"`
}

// RoyaleSettings is nested under settings.royale (R10).
type RoyaleSettings struct {
	ShrinkEveryNTurns int `json:"shrinkEveryNTurns"`
}

// RulesetSettings is game.ruleset.settings.
type RulesetSettings struct {
	FoodSpawnChance     int            `json:"foodSpawnChance"`
	MinimumFood         int            `json:"minimumFood"`
	HazardDamagePerTurn int            `json:"hazardDamagePerTurn"` // R10: root level
	Royale              RoyaleSettings `json:"royale"`              // R10: NESTED
}

// Ruleset is game.ruleset.
type Ruleset struct {
	Name     string          `json:"name"`
	Version  string          `json:"version"`
	Settings RulesetSettings `json:"settings"`
}

// Game is the game block.
type Game struct {
	ID      string  `json:"id"`
	Ruleset Ruleset `json:"ruleset"`
	Map     string  `json:"map"`
	Timeout int     `json:"timeout"` // R10: USE THIS, never hardcode 500
	Source  string  `json:"source,omitempty"`
}

// GameState is the body of /start, /move and /end.
type GameState struct {
	Game  Game  `json:"game"`
	Turn  int   `json:"turn"`
	Board Board `json:"board"`
	You   Snake `json:"you"`
}

// MoveResponse is the /move reply.
type MoveResponse struct {
	Move  string `json:"move"`
	Shout string `json:"shout,omitempty"`
}

// InfoResponse is the GET / reply.
type InfoResponse struct {
	APIVersion string `json:"apiversion"`
	Author     string `json:"author,omitempty"`
	Color      string `json:"color,omitempty"`
	Head       string `json:"head,omitempty"`
	Tail       string `json:"tail,omitempty"`
	Version    string `json:"version,omitempty"`
}
