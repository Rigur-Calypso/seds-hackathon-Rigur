package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	seds "github.com/Rigur-Calypso/seds-hackathon-Rigur"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
)

// An equal-length rival can take 5,6 too: "up" is a mutual head-to-head (R2).
const equalHeadToHead = "you 100 5,5 5,4 5,3\nsnake rival 100 5,7 5,8 5,9\nfood 0,10\n"

func endJSON(t *testing.T, fields map[string]any) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseLog(t *testing.T) {
	recent := []frame{{Turn: 40, Move: "left", Board: equalHeadToHead}, {Turn: 41, Move: "up", Board: equalHeadToHead}}
	log := strings.Join([]string{
		"2026-09-14T10:00:00Z " + endJSON(t, map[string]any{"msg": "end", "game": "g1", "result": "loss", "recent_boards": recent}),
		endJSON(t, map[string]any{"msg": "end", "game": "g2", "result": "win"}),
		endJSON(t, map[string]any{"msg": "move", "game": "g1", "turn": 41}),
		endJSON(t, map[string]any{"msg": "end", "game": "g3", "result": "loss", "fatal_turn": 9, "fatal_move": "down", "fatal_board": equalHeadToHead}),
		endJSON(t, map[string]any{"msg": "end", "game": "g1", "result": "loss", "recent_boards": recent}),
		"not json at all",
	}, "\n")
	losses, err := parseLog(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 2 || losses[0].Game != "g1" || len(losses[0].Frames) != 2 || losses[1].Game != "g3" || losses[1].Frames[0].Move != "down" {
		t.Fatalf("got %+v", losses)
	}
}

func TestAnalyseFlagsProvenLosingMove(t *testing.T) {
	ps, err := config.Load(seds.ConfigFS)
	if err != nil {
		t.Fatal(err)
	}
	v, err := analyse(context.Background(), ps, frame{Turn: 41, Move: "up", Board: equalHeadToHead}, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Mistake || len(v.Values) != 3 || v.Values[0].Move == "up" {
		t.Fatalf("up into an equal head-to-head must be flagged: %s", v)
	}
	ok, err := analyse(context.Background(), ps, frame{Turn: 41, Move: v.Values[0].Move, Board: equalHeadToHead}, 5000)
	if err != nil || ok.Mistake {
		t.Fatalf("the best move must not be flagged: %s (%v)", ok, err)
	}
}
