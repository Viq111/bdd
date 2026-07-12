package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/viq111/bdd/internal/bench"
	"github.com/viq111/bdd/internal/fixture"
)

// TestRunResolvesRelativeBinaryPath is a regression test: run stages each
// command's working directory under a staged fixture workspace (WorkDir),
// so a relative -binary path must be resolved against the caller's cwd
// before exec, not against WorkDir.
func TestRunResolvesRelativeBinaryPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bdd script is a shell script")
	}

	dir := t.TempDir()
	binPath := filepath.Join(dir, "fakebdd.sh")
	script := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	fixturePath := filepath.Join(t.TempDir(), "fixture.sqlite")
	m, err := fixture.Generate(fixture.Options{Path: fixturePath, Cards: 5, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relBin, err := filepath.Rel(cwd, binPath)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"-binary", relBin,
		"-manifest", manifestPath,
		"-iterations", "1",
		"-warmup", "0",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s", code, stderr.String())
	}

	var report bench.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid report JSON: %v\n%s", err, stdout.String())
	}
	for _, res := range report.Results {
		if res.Status == bench.StatusError {
			t.Errorf("command %s errored: %s", res.Name, res.Detail)
		}
	}
}
