// Package config holds every tunable. Zero magic numbers live anywhere else in
// eval or search (CLAUDE.md §9). Profiles are loaded from config/*.json over
// Defaults(), so a profile only lists what it changes.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
)

// Params is one profile.
type Params struct {
	Name string `json:"name"`

	// Time budget (ms): deadline = min(timeout - margin, CPUCapMs), where margin
	// is max(NetworkMarginMs, measured overhead + OverheadPadMs); scaled down
	// under concurrency; never below MinBudgetMs.
	NetworkMarginMs int `json:"networkMarginMs"`
	OverheadPadMs   int `json:"overheadPadMs"`
	CPUCapMs        int `json:"cpuCapMs"`
	MinBudgetMs     int `json:"minBudgetMs"`

	// TVAE envelope.
	LocalityRadius int     `json:"localityRadius"` // opponents farther than this (Manhattan) get one policy move
	MaxJoint       int     `json:"maxJoint"`       // cap on joint opponent actions per candidate
	RiskMean       float64 `json:"riskMean"`       // aggregate = RiskMean*E + RiskCVaR*CVaR + RiskMin*min
	RiskCVaR       float64 `json:"riskCVaR"`
	RiskMin        float64 `json:"riskMin"`
	CVaRAlpha      float64 `json:"cvarAlpha"`
	// EnvelopeShrinkPessimistic: on a royale shrink turn, outcomes carry the union
	// of all four possible next rings instead of the current ring (R8).
	EnvelopeShrinkPessimistic bool `json:"envelopeShrinkPessimistic"`

	// Threat Graph (P1): on TVAE outcomes where a squeeze is plausible, check
	// whether one joint reply of the nearest opponents refutes every next move.
	ThreatGraph        bool    `json:"threatGraph"`
	ThreatRadius       int     `json:"threatRadius"`       // opponents within this distance take part
	ThreatMaxOpponents int     `json:"threatMaxOpponents"` // nearest opponents enumerated
	ThreatMaxEvals     int     `json:"threatMaxEvals"`     // resolutions per decision
	ThreatForcedScore  float64 `json:"threatForcedScore"`  // value of a forced squeeze: below every normal position, above certain death
	ThreatTrapRefutes  bool    `json:"threatTrapRefutes"`  // a reply that leaves us alive but without room also refutes (off: only death refutes)
	ThreatLongerOnly   bool    `json:"threatLongerOnly"`   // only equal-or-longer opponents choose replies; shorter ones keep their default move (their bodies still block)

	// Opponent ensemble mixture (advisory weights only; all legal actions stay).
	EnsSpace   float64 `json:"ensSpace"`
	EnsFood    float64 `json:"ensFood"`
	EnsAggro   float64 `json:"ensAggro"`
	EnsUniform float64 `json:"ensUniform"`

	// Evaluation. The heuristic sum is squashed with tanh into (-1, 1).
	ContestedWeight   float64 `json:"contestedWeight"`
	AttackCellWeight  float64 `json:"attackCellWeight"` // contested cells where we are the strictly-longest arriver (R2: we win the collision)
	WArea             float64 `json:"wArea"`
	WTrapped          float64 `json:"wTrapped"`
	WRobust           float64 `json:"wRobust"`
	WExits            float64 `json:"wExits"`
	WNoSafeExit       float64 `json:"wNoSafeExit"` // every next-turn exit is reachable by an equal-or-longer head
	WHunger           float64 `json:"wHunger"`
	HungerMargin      int     `json:"hungerMargin"`
	WFood             float64 `json:"wFood"`
	FoodDecayTurns    int     `json:"foodDecayTurns"`
	FoodFloor         float64 `json:"foodFloor"`
	LengthLead        int     `json:"lengthLead"`
	SatiatedFoodScale float64 `json:"satiatedFoodScale"`
	WLength           float64 `json:"wLength"`
	LengthScale       float64 `json:"lengthScale"`
	WHealth           float64 `json:"wHealth"`
	WKill             float64 `json:"wKill"`
	WOppTrapped       float64 `json:"wOppTrapped"`
	WAttack           float64 `json:"wAttack"`

	// Royale.
	WCentre         float64 `json:"wCentre"`
	CentreRampTurns int     `json:"centreRampTurns"`
	WInHazard       float64 `json:"wInHazard"`
	WShrinkRobust   float64 `json:"wShrinkRobust"`
	ShrinkLookahead int     `json:"shrinkLookahead"`
	WHazardWeapon   float64 `json:"wHazardWeapon"`

	// Duel search (exactly two snakes alive).
	DuelEnabled  bool `json:"duelEnabled"`
	DuelMaxDepth int  `json:"duelMaxDepth"`
	// DuelMinDepth: the duel result replaces the TVAE move only if search
	// completed at least this depth (or proved a win). Measured on 19×19:
	// depth-3 paranoid search loses to TVAE head-to-head (41.5%), depth 4 beats it (55%).
	DuelMinDepth int     `json:"duelMinDepth"`
	PlyStep      float64 `json:"plyStep"` // terminal ordering: win sooner / lose later

	// Engine selects the decision engine: "v1" (TVAE envelope + duel search) or
	// "v2" (internal/brain: deep turn-based search on the fast sim state).
	Engine string `json:"engine"`
	// v2 search. Depth counts whole simultaneous turns.
	SearchMaxDepth int `json:"searchMaxDepth"`
	// SearchNodes caps resolved joint actions per decision (0 = deadline only).
	// A cap makes arena runs reproducible without a wall-clock budget.
	SearchNodes int `json:"searchNodes"`
	// SearchNodesNoDeadline caps a search whose context has no deadline and
	// SearchNodes is 0, so no call is ever unbounded.
	SearchNodesNoDeadline int `json:"searchNodesNoDeadline"`
	// Opponents whose heads are within 2·depth+SearchAdvSlack of ours choose
	// adversarial replies (at most SearchMaxAdv, nearest first); the others play
	// one predicted move.
	SearchMaxAdv   int `json:"searchMaxAdv"`
	SearchAdvSlack int `json:"searchAdvSlack"`
	// SearchExtensions: leaves with an equal-or-longer head within two cells are
	// extended by a turn, at most this many times per path.
	SearchExtensions int `json:"searchExtensions"`
	// SearchShrinkPessimistic: future royale shrinks hazard the union of the four
	// possible rings (R8) instead of keeping the supplied ring.
	SearchShrinkPessimistic bool `json:"searchShrinkPessimistic"`
	SearchTTBits            int  `json:"searchTTBits"` // transposition table size, log2 entries
	// PredictHungry: a predicted opponent at or below this health heads for food.
	PredictHungry int `json:"predictHungry"`
	// SearchRationalOpp: adversaries that are not longer than us never step onto
	// a cell next to our head (they would lose or trade the head-to-head, R2).
	// Off = fully paranoid replies.
	SearchRationalOpp bool `json:"searchRationalOpp"`
	// SearchPVS: principal-variation search — later moves and replies are first
	// tested with a null window and re-searched only if they might improve.
	SearchPVS bool `json:"searchPVS"`
	// HazardHunger: on hazard boards, hunger urgency uses the health left on
	// arriving at the cheapest reachable food (storm damage charged) instead of
	// health minus distance.
	HazardHunger bool `json:"hazardHunger"`
	// FoodDeficitScale multiplies the v2 food drive while we are not strictly
	// the longest snake (1 = unchanged, identical to v1's term).
	FoodDeficitScale float64 `json:"foodDeficitScale"`
	// SearchEndgame (P4): when no opponent can ever reach our region, keep only
	// the root moves a survival search proves last longest, up to EndgameHorizon
	// turns, spending at most EndgameNodes resolutions.
	SearchEndgame  bool `json:"searchEndgame"`
	EndgameHorizon int  `json:"endgameHorizon"`
	EndgameNodes   int  `json:"endgameNodes"`
	// EndgameAlways runs the survival filter even when regions still touch, with
	// opponents on their predicted moves: a move that cannot outlive the others
	// even then is a self-trap.
	EndgameAlways bool `json:"endgameAlways"`
}

// Defaults are the qualifying-style baseline every profile starts from.
func Defaults() Params {
	return Params{
		Name:            "default",
		NetworkMarginMs: 180,
		OverheadPadMs:   40,
		CPUCapMs:        220,
		MinBudgetMs:     25,

		LocalityRadius: 6,
		MaxJoint:       64,
		RiskMean:       0.25,
		RiskCVaR:       0.75,
		RiskMin:        0,
		CVaRAlpha:      0.25,

		ThreatGraph:        false,
		ThreatRadius:       4,
		ThreatMaxOpponents: 2,
		ThreatMaxEvals:     1500,
		ThreatForcedScore:  -1.2,
		ThreatTrapRefutes:  true,

		EnsSpace:   1,
		EnsFood:    1,
		EnsAggro:   0.5,
		EnsUniform: 0.5,

		ContestedWeight:   0.5,
		AttackCellWeight:  0.85,
		WArea:             3,
		WTrapped:          3,
		WRobust:           1,
		WExits:            0.1,
		WNoSafeExit:       0.6,
		WHunger:           2,
		HungerMargin:      12,
		WFood:             0.35,
		FoodDecayTurns:    250,
		FoodFloor:         0.3,
		LengthLead:        2,
		SatiatedFoodScale: 0.3,
		WLength:           0.6,
		LengthScale:       4,
		WHealth:           0.15,
		WKill:             0.8,
		WOppTrapped:       0.5,
		WAttack:           0.5,

		WCentre:         0.4,
		CentreRampTurns: 50,
		WInHazard:       0.4,
		WShrinkRobust:   0.3,
		ShrinkLookahead: 2,
		WHazardWeapon:   0.3,

		DuelEnabled:  false,
		DuelMaxDepth: 4,
		DuelMinDepth: 4,
		PlyStep:      0.01,

		Engine:                  "v1",
		SearchMaxDepth:          20,
		SearchNodes:             0,
		SearchNodesNoDeadline:   30000,
		SearchMaxAdv:            3,
		SearchAdvSlack:          1,
		SearchExtensions:        2,
		SearchShrinkPessimistic: true,
		SearchTTBits:            16,
		PredictHungry:           35,
		FoodDeficitScale:        1,
		SearchEndgame:           false,
		EndgameHorizon:          48,
		EndgameNodes:            30000,
	}
}

// Names of the profiles we ship (CLAUDE.md §4).
var Names = []string{"qualifying", "royale", "duel", "constrictor"}

// Profiles is a set of named Params.
type Profiles struct {
	byName map[string]*Params
}

// DefaultProfiles returns every profile set to Defaults() (last-resort only).
func DefaultProfiles() *Profiles {
	ps := &Profiles{byName: map[string]*Params{}}
	for _, n := range Names {
		p := Defaults()
		p.Name = n
		ps.byName[n] = &p
	}
	return ps
}

// Load reads config/<name>.json for every profile, strictly (unknown keys are
// an error, so a typo cannot silently fall back to a default).
func Load(fsys fs.FS) (*Profiles, error) {
	ps := &Profiles{byName: map[string]*Params{}}
	for _, n := range Names {
		b, err := fs.ReadFile(fsys, "config/"+n+".json")
		if err != nil {
			return nil, err
		}
		p, err := Decode(b)
		if err != nil {
			return nil, fmt.Errorf("config %s: %w", n, err)
		}
		if p.Name == "" || p.Name == "default" {
			p.Name = n
		}
		ps.byName[n] = p
	}
	return ps, nil
}

// Decode parses one profile over Defaults().
func Decode(b []byte) (*Params, error) {
	p := Defaults()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Get returns a profile by name, falling back to qualifying, then Defaults.
func (ps *Profiles) Get(name string) *Params {
	if p, ok := ps.byName[name]; ok {
		return p
	}
	if p, ok := ps.byName["qualifying"]; ok {
		return p
	}
	d := Defaults()
	return &d
}

// Set replaces a profile (arena overrides).
func (ps *Profiles) Set(name string, p *Params) { ps.byName[name] = p }

// Clone deep-copies the set.
func (ps *Profiles) Clone() *Profiles {
	out := &Profiles{byName: map[string]*Params{}}
	for k, v := range ps.byName {
		c := *v
		out.byName[k] = &c
	}
	return out
}
