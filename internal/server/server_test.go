package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	seds "github.com/Rigur-Calypso/seds-hackathon-Rigur"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/decide"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/search"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	ps, err := config.Load(seds.ConfigFS)
	if err != nil {
		t.Fatal(err)
	}
	s := New(decide.New(ps, search.Evaluate), slog.New(slog.NewTextHandler(io.Discard, nil)), "test", nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func post(t *testing.T, url, body string) (int, []byte) {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func assertMove(t *testing.T, name string, code int, b []byte) {
	t.Helper()
	var mr struct{ Move string }
	if code != 200 || json.Unmarshal(b, &mr) != nil {
		t.Fatalf("%s: code %d body %q", name, code, b)
	}
	switch mr.Move {
	case "up", "down", "left", "right":
	default:
		t.Fatalf("%s: invalid move %q", name, mr.Move)
	}
}

func TestInfo(t *testing.T) {
	ts := testServer(t)
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var info map[string]string
	if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&info) != nil || info["apiversion"] != "1" {
		t.Fatalf("info: %d %v", res.StatusCode, info)
	}
}

const good = `{"game":{"id":"%s","ruleset":{"name":"standard","settings":{"hazardDamagePerTurn":14,"royale":{"shrinkEveryNTurns":25}}},"map":"standard","timeout":500},"turn":%d,
"board":{"width":11,"height":11,"food":[{"x":2,"y":2}],"hazards":[],"snakes":[
{"id":"me","name":"me","health":90,"latency":"40","body":[{"x":5,"y":5},{"x":5,"y":4},{"x":5,"y":3}]},
{"id":"o1","name":"o1","health":90,"latency":"300","body":[{"x":8,"y":8},{"x":8,"y":7},{"x":8,"y":6}]},
{"id":"o2","name":"o2","health":90,"latency":"0","body":[{"x":1,"y":9},{"x":1,"y":8},{"x":1,"y":7}]}]},
"you":{"id":"me","name":"me","health":90,"latency":"40","body":[{"x":5,"y":5},{"x":5,"y":4},{"x":5,"y":3}]}}`

// Step 8 gate: zero 5xx and a valid move for every adversarial payload.
func TestMoveAdversarial(t *testing.T) {
	ts := testServer(t)
	cases := map[string]string{
		"empty":        "",
		"garbage":      "not json",
		"null":         "null",
		"emptyobj":     "{}",
		"array":        "[1,2,3]",
		"no-board":     `{"game":{"id":"x"},"you":{"id":"a"}}`,
		"zero-length":  `{"game":{"id":"x"},"board":{"width":11,"height":11,"snakes":[{"id":"a","body":[]}]},"you":{"id":"a","body":[]}}`,
		"1x1":          `{"game":{"id":"x"},"board":{"width":1,"height":1,"snakes":[{"id":"a","health":100,"body":[{"x":0,"y":0}]}]},"you":{"id":"a","health":100,"body":[{"x":0,"y":0}]}}`,
		"oob-body":     `{"game":{"id":"x"},"board":{"width":5,"height":5,"snakes":[{"id":"a","health":100,"body":[{"x":9,"y":9},{"x":-3,"y":2}]}]},"you":{"id":"a","health":100,"body":[{"x":9,"y":9}]}}`,
		"huge-board":   `{"game":{"id":"x"},"board":{"width":100000,"height":100000},"you":{"id":"a"}}`,
		"you-missing":  `{"game":{"id":"x"},"board":{"width":11,"height":11,"snakes":[{"id":"b","health":100,"body":[{"x":1,"y":1}]}]},"you":{"id":"a","body":[{"x":5,"y":5}]}}`,
		"empty-food":   fmt.Sprintf(strings.Replace(good, `"food":[{"x":2,"y":2}]`, `"food":[]`, 1), "ef", 3),
		"null-lists":   fmt.Sprintf(strings.Replace(good, `"hazards":[]`, `"hazards":null`, 1), "nl", 3),
		"tiny-timeout": fmt.Sprintf(strings.Replace(good, `"timeout":500`, `"timeout":1`, 1), "tt", 3),
		"good":         fmt.Sprintf(good, "g", 3),
	}
	for name, body := range cases {
		code, b := post(t, ts.URL+"/move", body)
		assertMove(t, name, code, b)
		for _, ep := range []string{"/start", "/end"} {
			if code, _ := post(t, ts.URL+ep, body); code != 200 {
				t.Fatalf("%s %s: code %d", name, ep, code)
			}
		}
	}
}

// Step 8 gate: concurrent games do not interfere and stay within budget.
func TestConcurrentGames(t *testing.T) {
	ts := testServer(t)
	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for turn := 1; turn <= 25; turn++ {
				start := time.Now()
				res, err := http.Post(ts.URL+"/move", "application/json", strings.NewReader(fmt.Sprintf(good, fmt.Sprintf("game-%d", g), turn)))
				if err != nil {
					errs <- err.Error()
					return
				}
				b, _ := io.ReadAll(res.Body)
				res.Body.Close()
				var mr struct{ Move string }
				if res.StatusCode != 200 || json.Unmarshal(b, &mr) != nil || mr.Move == "" {
					errs <- fmt.Sprintf("game %d turn %d: %d %s", g, turn, res.StatusCode, b)
					return
				}
				if el := time.Since(start); el > 400*time.Millisecond {
					errs <- fmt.Sprintf("game %d turn %d: %v", g, turn, el)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
}

func TestDecisionHeader(t *testing.T) {
	ts := testServer(t)
	res, err := http.Post(ts.URL+"/move", "application/json", strings.NewReader(fmt.Sprintf(good, "hdr", 3)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	h := res.Header.Get("X-Snake-Decision")
	if !strings.Contains(h, "reason=") || !strings.Contains(h, "depth=") || !strings.Contains(h, "compute_ms=") {
		t.Fatalf("missing decision header: %q", h)
	}
}

// Shipped profiles cap compute at the value measured safe on Render's free tier.
func TestShippedCPUCap(t *testing.T) {
	ps, err := config.Load(seds.ConfigFS)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range config.Names {
		p := ps.Get(n)
		if got := p.CPUCapMs; got > 60 {
			t.Fatalf("profile %s cpuCapMs=%d; live Render measurements show stalls above ~60 ms", n, got)
		}
		// v2 searches until the deadline; on a throttled CPU the burst itself causes
		// stalls, so shipped profiles also cap nodes (DEVLOG §11.10).
		if p.Engine == "v2" && (p.SearchNodes <= 0 || p.SearchNodes > 5000) {
			t.Fatalf("profile %s searchNodes=%d; want a cap in (0, 5000]", n, p.SearchNodes)
		}
	}
}

func TestBudget(t *testing.T) {
	p := config.Defaults()
	if b := Budget(500, &p, 1, 0); b != 220*time.Millisecond {
		t.Fatalf("base: %v", b)
	}
	if b := Budget(500, &p, 1, 300); b != 160*time.Millisecond {
		t.Fatalf("measured overhead must widen the margin: %v", b)
	}
	if b := Budget(500, &p, 4, 0); b != 55*time.Millisecond {
		t.Fatalf("contention must split the budget evenly (220/4): %v", b)
	}
	if b := Budget(100, &p, 1, 0); b != time.Duration(p.MinBudgetMs)*time.Millisecond {
		t.Fatalf("floor: %v", b)
	}
	// Review finding: the floor must never push the deadline past a tiny timeout.
	if b := Budget(1, &p, 1, 0); b > time.Millisecond {
		t.Fatalf("timeout=1 must not get a %v budget", b)
	}
	if b := Budget(30, &p, 1, 0); b > 15*time.Millisecond {
		t.Fatalf("timeout=30 budget %v exceeds half the timeout", b)
	}
}
