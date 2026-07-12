package bench

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {
	d := []time.Duration{
		10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond,
		40 * time.Millisecond, 50 * time.Millisecond, 60 * time.Millisecond,
		70 * time.Millisecond, 80 * time.Millisecond, 90 * time.Millisecond,
		100 * time.Millisecond,
	}
	if got := percentile(d, 50); got != 50 {
		t.Errorf("p50 = %v, want 50", got)
	}
	if got := percentile(d, 95); got != 100 {
		t.Errorf("p95 = %v, want 100", got)
	}
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("percentile of empty slice = %v, want 0", got)
	}
}

func TestMinMax(t *testing.T) {
	d := []time.Duration{30 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond}
	lo, hi := minMax(d)
	if lo != 10 || hi != 30 {
		t.Errorf("minMax = (%v, %v), want (10, 30)", lo, hi)
	}
}

// fakeBddScript builds a tiny shell script standing in for the real bdd
// binary: `ok` always succeeds, anything else exits 2 with the same
// "unknown command" text cmd/bdd emits for a subcommand that has not
// landed yet.
func fakeBddScript(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake bdd script is a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fakebdd.sh")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = ok ]; then exit 0; fi\n" +
		"echo \"bdd: unknown command \\\"$1\\\"\" >&2\n" +
		"exit 2\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunDetectsUnimplemented(t *testing.T) {
	bin := fakeBddScript(t)
	results := Run(context.Background(), []Command{
		{Name: "known", Args: []string{"ok"}},
		{Name: "missing", Args: []string{"show"}},
	}, Options{BinaryPath: bin, Iterations: 3, Warmup: 1})

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Status != StatusOK {
		t.Errorf("known: status = %v, want %v", results[0].Status, StatusOK)
	}
	if results[0].Samples != 3 {
		t.Errorf("known: samples = %d, want 3", results[0].Samples)
	}
	if results[1].Status != StatusUnimplemented {
		t.Errorf("missing: status = %v, want %v", results[1].Status, StatusUnimplemented)
	}
}

func TestRunReportsErrorWithoutAborting(t *testing.T) {
	bin := fakeBddScript(t)
	results := Run(context.Background(), []Command{
		{Name: "boom", Args: []string{"exit-with-garbage"}},
		{Name: "known", Args: []string{"ok"}},
	}, Options{BinaryPath: bin, Iterations: 2, Warmup: 0})

	if results[0].Status != StatusUnimplemented {
		// the fake script always reports "unknown command" on non-ok args,
		// so this exercises the same path; a real non-2 exit is covered by
		// looksUnimplemented's exit-code check directly.
		t.Errorf("boom: status = %v", results[0].Status)
	}
	if results[1].Status != StatusOK {
		t.Errorf("subsequent command did not run after a failure: status = %v", results[1].Status)
	}
}
