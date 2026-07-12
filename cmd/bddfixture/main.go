// Command bddfixture generates a deterministic bdd-shaped SQLite workspace
// for benchmarking and QA verification. See internal/fixture for the schema
// and generation rules, and docs/benchmark.md for how this fits into the
// latency benchmark harness.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/viq111/bdd/internal/fixture"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("bddfixture", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "path to write the SQLite fixture (required)")
	manifestPath := fs.String("manifest", "", "path to write the JSON manifest (required)")
	cards := fs.Int("cards", 10000, "number of cards to generate")
	seed := fs.Int64("seed", 42, "PRNG seed; same seed+cards is byte-for-byte reproducible")
	prefix := fs.String("prefix", "bdd", "workspace ID prefix")
	force := fs.Bool("force", false, "remove an existing fixture at -out before generating")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *out == "" || *manifestPath == "" {
		fmt.Fprintln(stderr, "bddfixture: -out and -manifest are required")
		fs.Usage()
		return 2
	}

	if *force {
		os.Remove(*out)
		os.Remove(*out + "-wal")
		os.Remove(*out + "-shm")
	}

	m, err := fixture.Generate(fixture.Options{
		Path:     *out,
		Cards:    *cards,
		Seed:     *seed,
		IDPrefix: *prefix,
	})
	if err != nil {
		fmt.Fprintf(stderr, "bddfixture: %v\n", err)
		return 1
	}

	f, err := os.Create(*manifestPath)
	if err != nil {
		fmt.Fprintf(stderr, "bddfixture: create manifest: %v\n", err)
		return 1
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		fmt.Fprintf(stderr, "bddfixture: write manifest: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "wrote %d cards to %s (manifest: %s)\n", m.CardCount, *out, *manifestPath)
	return 0
}
