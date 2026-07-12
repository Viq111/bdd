// Package bench measures the wall-clock latency of the compiled bdd binary
// by executing it as a real subprocess, the way an interactive user or an
// agent would invoke it. It never calls into the bdd library directly: the
// point of the harness is to capture process-start overhead, flag parsing,
// and (once implemented) workspace discovery and SQLite open, not just the
// cost of a Go function call.
package bench

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Command is one benchmarked bdd invocation.
type Command struct {
	// Name identifies the command in reports (e.g. "show", "update_claim").
	Name string
	// Args is the full argv passed to the bdd binary, excluding argv[0].
	Args []string
}

// Status reports how a Command's benchmark run went.
type Status string

const (
	StatusOK            Status = "ok"
	StatusUnimplemented Status = "unimplemented"
	StatusError         Status = "error"
)

// Result is the outcome of benchmarking a single Command.
type Result struct {
	Name    string   `json:"name"`
	Args    []string `json:"args"`
	Status  Status   `json:"status"`
	Detail  string   `json:"detail,omitempty"`
	Samples int      `json:"samples,omitempty"`

	ColdMillis float64 `json:"cold_ms,omitempty"`
	P50Millis  float64 `json:"p50_ms,omitempty"`
	P95Millis  float64 `json:"p95_ms,omitempty"`
	MinMillis  float64 `json:"min_ms,omitempty"`
	MaxMillis  float64 `json:"max_ms,omitempty"`
}

// Options configures Run.
type Options struct {
	// BinaryPath is the compiled bdd binary to exec.
	BinaryPath string
	// WorkDir is the working directory bdd is invoked from (typically a
	// directory containing or above a .bdd/bdd.sqlite fixture).
	WorkDir string
	// Iterations is the number of warm timed runs per command, after the
	// cold-start sample and Warmup discarded runs.
	Iterations int
	// Warmup is the number of untimed runs executed (and discarded) after
	// the cold-start sample and before timed iterations, so the OS file
	// cache for the fixture database is populated.
	Warmup int
}

// unimplementedMarker matches cmd/bdd's error text for a subcommand that
// has not landed yet (internal/cli/main.go: `bdd: unknown command %q`).
// bdd version/help never hit this path, so detecting it here is safe:
// any other non-zero exit is reported as StatusError instead of silently
// treated as unimplemented.
const unimplementedMarker = "unknown command"

// Run executes every Command in cmds against opts.BinaryPath and returns one
// Result each, in the same order. A Command whose first invocation looks
// like "not implemented yet" (see unimplementedMarker) is reported as
// StatusUnimplemented without further timing; a Command that errors for any
// other reason is reported as StatusError. Run never returns an error itself
// so that one misbehaving command cannot abort the rest of the benchmark.
func Run(ctx context.Context, cmds []Command, opts Options) []Result {
	results := make([]Result, 0, len(cmds))
	for _, c := range cmds {
		results = append(results, runOne(ctx, c, opts))
	}
	return results
}

func runOne(ctx context.Context, c Command, opts Options) Result {
	res := Result{Name: c.Name, Args: c.Args}

	coldDur, err, stderr := exec1(ctx, opts.BinaryPath, opts.WorkDir, c.Args)
	if err != nil {
		if looksUnimplemented(err, stderr) {
			res.Status = StatusUnimplemented
			res.Detail = strings.TrimSpace(stderr)
			return res
		}
		res.Status = StatusError
		res.Detail = fmt.Sprintf("%v: %s", err, strings.TrimSpace(stderr))
		return res
	}
	res.ColdMillis = coldDur.Seconds() * 1000

	for i := 0; i < opts.Warmup; i++ {
		if _, err, stderr := exec1(ctx, opts.BinaryPath, opts.WorkDir, c.Args); err != nil {
			res.Status = StatusError
			res.Detail = fmt.Sprintf("warmup: %v: %s", err, strings.TrimSpace(stderr))
			return res
		}
	}

	samples := make([]time.Duration, 0, opts.Iterations)
	for i := 0; i < opts.Iterations; i++ {
		d, err, stderr := exec1(ctx, opts.BinaryPath, opts.WorkDir, c.Args)
		if err != nil {
			res.Status = StatusError
			res.Detail = fmt.Sprintf("iteration %d: %v: %s", i, err, strings.TrimSpace(stderr))
			return res
		}
		samples = append(samples, d)
	}

	res.Status = StatusOK
	res.Samples = len(samples)
	res.MinMillis, res.MaxMillis = minMax(samples)
	res.P50Millis = percentile(samples, 50)
	res.P95Millis = percentile(samples, 95)
	return res
}

func exec1(ctx context.Context, binary, workDir string, args []string) (time.Duration, error, string) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = workDir
	var stderr strings.Builder
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	return elapsed, err, stderr.String()
}

func looksUnimplemented(err error, stderr string) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return exitErr.ExitCode() == 2 && strings.Contains(stderr, unimplementedMarker)
}

func minMax(d []time.Duration) (min, max float64) {
	if len(d) == 0 {
		return 0, 0
	}
	lo, hi := d[0], d[0]
	for _, v := range d[1:] {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return lo.Seconds() * 1000, hi.Seconds() * 1000
}

// percentile returns the p-th nearest-rank percentile of d, in
// milliseconds. d is not mutated.
func percentile(d []time.Duration, p int) float64 {
	if len(d) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(d))
	copy(sorted, d)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	rank := (p*len(sorted) + 99) / 100 // ceil(p/100 * n), 1-indexed
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1].Seconds() * 1000
}
