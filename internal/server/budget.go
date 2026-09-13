package server

import (
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
)

// Budget derives the cooperative search deadline from the request (R10):
//
//	margin = max(NetworkMarginMs, measuredOverhead + OverheadPadMs)
//	budget = min(timeout - margin, CPUCapMs), scaled by 2/(inflight+1) under
//	contention, floored at MinBudgetMs.
//
// Under contention we shrink compute, never the fallback path.
func Budget(timeoutMs int, p *config.Params, inflight, overheadMs int) time.Duration {
	if timeoutMs <= 0 {
		timeoutMs = 500
	}
	margin := p.NetworkMarginMs
	if o := overheadMs + p.OverheadPadMs; overheadMs > 0 && o > margin {
		margin = o
	}
	b := timeoutMs - margin
	if b > p.CPUCapMs {
		b = p.CPUCapMs
	}
	if inflight > 1 {
		b = b * 2 / (inflight + 1)
	}
	if b < p.MinBudgetMs {
		b = p.MinBudgetMs
	}
	return time.Duration(b) * time.Millisecond
}

// TuneRuntime caps GOMAXPROCS to the container CPU quota. Render's free tier is
// a fraction of one CPU; letting Go schedule on every host core burns the CFS
// quota in parallel and stalls the process for the rest of the 100 ms period,
// which lands directly on move latency. Newer Go does this itself only when
// go.mod declares >= 1.25, and we pin 1.21 for host compatibility.
func TuneRuntime(log *slog.Logger) {
	if os.Getenv("GOMAXPROCS") != "" {
		return
	}
	b, err := os.ReadFile("/sys/fs/cgroup/cpu.max") // cgroup v2: "quota period" or "max period"
	if err != nil {
		return
	}
	f := strings.Fields(string(b))
	if len(f) != 2 || f[0] == "max" {
		return
	}
	quota, err1 := strconv.ParseFloat(f[0], 64)
	period, err2 := strconv.ParseFloat(f[1], 64)
	if err1 != nil || err2 != nil || period <= 0 {
		return
	}
	procs := int(quota / period)
	if procs < 1 {
		procs = 1
	}
	if procs < runtime.GOMAXPROCS(0) {
		runtime.GOMAXPROCS(procs)
		log.Info("gomaxprocs_capped", "procs", procs, "quota", quota, "period", period)
	}
}
