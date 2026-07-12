// Command bddbench runs the section 7 subprocess latency benchmark: it
// executes the compiled bdd binary against a fixture workspace for each of
// version/help, show <id>, update <id> --claim, ready --limit 100, and
// search <text> --limit 50, and reports p50/p95 per command plus a
// separate cold-start sample. See docs/benchmark.md for methodology,
// reference-machine assumptions, and how this fits into CI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/viq111/bdd/internal/bench"
	"github.com/viq111/bdd/internal/fixture"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bddbench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	binary := fs.String("binary", "", "path to the compiled bdd binary (required)")
	manifestPath := fs.String("manifest", "", "path to a fixture manifest JSON file (required)")
	iterations := fs.Int("iterations", 50, "warm timed iterations per command")
	warmup := fs.Int("warmup", 5, "untimed warm-up runs per command before timing")
	outPath := fs.String("out", "", "path to write the JSON report (default: stdout)")
	workspace := fs.String("workspace", "", "directory to stage the fixture workspace in (default: a temp dir)")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *binary == "" || *manifestPath == "" {
		fmt.Fprintln(stderr, "bddbench: -binary and -manifest are required")
		fs.Usage()
		return 2
	}

	m, err := loadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintf(stderr, "bddbench: %v\n", err)
		return 1
	}

	// Resolve to an absolute path: commands run with cmd.Dir set to the
	// staged workspace, so a relative binary path would be looked up there
	// instead of relative to the caller's working directory.
	binPath, err := filepath.Abs(*binary)
	if err != nil {
		fmt.Fprintf(stderr, "bddbench: %v\n", err)
		return 1
	}

	workDir := *workspace
	if workDir == "" {
		dir, err := os.MkdirTemp("", "bddbench-workspace-")
		if err != nil {
			fmt.Fprintf(stderr, "bddbench: %v\n", err)
			return 1
		}
		defer os.RemoveAll(dir)
		workDir = dir
	}
	if err := stageWorkspace(workDir, m.Path); err != nil {
		fmt.Fprintf(stderr, "bddbench: stage workspace: %v\n", err)
		return 1
	}

	cmds := []bench.Command{
		{Name: "version", Args: []string{"version"}},
		{Name: "help", Args: []string{"help"}},
		{Name: "show", Args: []string{"show", m.ShowID}},
		{Name: "update_claim", Args: []string{"update", m.ClaimID, "--claim"}},
		{Name: "ready", Args: []string{"ready", "--limit", "100"}},
		{Name: "search", Args: []string{"search", m.SearchQuery, "--limit", "50"}},
	}

	results := bench.Run(context.Background(), cmds, bench.Options{
		BinaryPath: binPath,
		WorkDir:    workDir,
		Iterations: *iterations,
		Warmup:     *warmup,
	})

	report := bench.Report{
		GeneratedAt: time.Now().UTC(),
		Binary:      binPath,
		Fixture:     m.Path,
		Seed:        m.Seed,
		CardCount:   m.CardCount,
		Iterations:  *iterations,
		Warmup:      *warmup,
		Host:        bench.CurrentHost(),
		Results:     results,
	}

	out := stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintf(stderr, "bddbench: %v\n", err)
			return 1
		}
		defer f.Close()
		out = f
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(stderr, "bddbench: write report: %v\n", err)
		return 1
	}

	printSummary(stderr, report)
	return 0
}

func loadManifest(path string) (*fixture.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m fixture.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// stageWorkspace copies the fixture database into workDir/.bdd/bdd.sqlite so
// bdd's workspace discovery (once implemented) finds it the same way it
// would in real use.
func stageWorkspace(workDir, fixturePath string) error {
	bddDir := filepath.Join(workDir, ".bdd")
	if err := os.MkdirAll(bddDir, 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bddDir, "bdd.sqlite"), data, 0o644)
}

func printSummary(w io.Writer, r bench.Report) {
	fmt.Fprintf(w, "\nbdd subprocess latency benchmark (%s/%s, %d cards, %d iterations + %d warmup)\n",
		r.Host.OS, r.Host.Arch, r.CardCount, r.Iterations, r.Warmup)
	fmt.Fprintf(w, "%-14s %-10s %8s %8s %8s\n", "command", "status", "cold_ms", "p50_ms", "p95_ms")
	for _, res := range r.Results {
		switch res.Status {
		case bench.StatusOK:
			fmt.Fprintf(w, "%-14s %-10s %8.2f %8.2f %8.2f\n", res.Name, res.Status, res.ColdMillis, res.P50Millis, res.P95Millis)
		default:
			fmt.Fprintf(w, "%-14s %-10s %8s %8s %8s  %s\n", res.Name, res.Status, "-", "-", "-", res.Detail)
		}
	}
}
