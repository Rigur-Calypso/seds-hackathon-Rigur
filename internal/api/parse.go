package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// Defaults used only when the request omits a setting (R10).
const (
	DefaultTimeoutMs    = 500
	DefaultHazardDamage = 14
	DefaultShrinkEvery  = 25
	MaxBoardSide        = 64 // engine maximum is 25; anything larger is hostile
)

var (
	ErrEmptyBody     = errors.New("api: empty body")
	ErrBoardTooLarge = errors.New("api: board too large")
)

// presence records which optional settings the payload actually carried, so an
// explicit 0 is kept while an absent field gets the default.
type presence struct {
	Game struct {
		Ruleset struct {
			Settings struct {
				HazardDamagePerTurn *int `json:"hazardDamagePerTurn"`
			} `json:"settings"`
		} `json:"ruleset"`
	} `json:"game"`
}

// Parse decodes and normalises a request body. It never panics.
func Parse(body []byte) (*GameState, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, ErrEmptyBody
	}
	var gs GameState
	if err := json.Unmarshal(body, &gs); err != nil {
		return nil, err
	}
	var pr presence
	_ = json.Unmarshal(body, &pr)
	if pr.Game.Ruleset.Settings.HazardDamagePerTurn == nil {
		gs.Game.Ruleset.Settings.HazardDamagePerTurn = DefaultHazardDamage
	}
	Normalize(&gs)
	if gs.Board.Width > MaxBoardSide || gs.Board.Height > MaxBoardSide {
		return nil, ErrBoardTooLarge
	}
	return &gs, nil
}

// Normalize fills defaults and repairs derivable fields in place: lower-cased
// ruleset/map names, timeout, shrink interval (R10), Head/Length from Body
// (R11), zero-length snakes dropped, missing board size inferred.
func Normalize(gs *GameState) {
	if gs.Game.Timeout <= 0 {
		gs.Game.Timeout = DefaultTimeoutMs
	}
	rs := &gs.Game.Ruleset
	rs.Name = strings.ToLower(strings.TrimSpace(rs.Name))
	if rs.Name == "" {
		rs.Name = "standard"
	}
	gs.Game.Map = strings.ToLower(strings.TrimSpace(gs.Game.Map))
	if rs.Settings.Royale.ShrinkEveryNTurns <= 0 {
		// Engine rejects < 1; 0 means "absent" in practice (R10 silent-zero bug).
		rs.Settings.Royale.ShrinkEveryNTurns = DefaultShrinkEvery
	}

	b := &gs.Board
	kept := b.Snakes[:0]
	for _, sn := range b.Snakes {
		if len(sn.Body) == 0 {
			continue
		}
		sn.Head = sn.Body[0]
		sn.Length = len(sn.Body)
		kept = append(kept, sn)
	}
	b.Snakes = kept
	if len(gs.You.Body) > 0 {
		gs.You.Head = gs.You.Body[0]
		gs.You.Length = len(gs.You.Body)
	}

	if b.Width <= 0 || b.Height <= 0 {
		maxX, maxY := 0, 0
		see := func(c Coord) {
			if c.X > maxX {
				maxX = c.X
			}
			if c.Y > maxY {
				maxY = c.Y
			}
		}
		for _, sn := range b.Snakes {
			for _, c := range sn.Body {
				see(c)
			}
		}
		for _, c := range b.Food {
			see(c)
		}
		for _, c := range b.Hazards {
			see(c)
		}
		if b.Width <= 0 {
			b.Width = maxX + 1
		}
		if b.Height <= 0 {
			b.Height = maxY + 1
		}
	}
}
