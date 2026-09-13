package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

// --results writes one JSON line per game of configuration A. Two arena
// binaries built before and after a code change (one that is a fix rather than
// a tunable, so it has no profile flag) can then be compared game by game on
// identical seeds.
var resultsPath = flag.String("results", "", "write per-game results of A as JSON lines to this file")

type gameLine struct {
	Seed      int64    `json:"seed"`
	Points    float64  `json:"points"`
	Won       bool     `json:"won"`
	Place     float64  `json:"place"`
	Turns     int      `json:"turns"`
	Survived  int      `json:"survived"`
	Cause     string   `json:"cause,omitempty"`
	Opponents []string `json:"opponents"`
}

func writeResultsIfRequested(rs []GameResult) {
	if *resultsPath == "" {
		return
	}
	f, err := os.Create(*resultsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "results:", err)
		os.Exit(1)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range rs {
		_ = enc.Encode(gameLine{Seed: r.Seed, Points: r.Points, Won: r.Won, Place: r.Place, Turns: r.Turns,
			Survived: r.Survived, Cause: r.Cause, Opponents: r.Opponents})
	}
}
