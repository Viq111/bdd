package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func captureRun(t *testing.T, args []string) (stdout, stderr string, code int) {
	t.Helper()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	code = run(args, outW, errW)

	outW.Close()
	errW.Close()

	var outBuf, errBuf bytes.Buffer
	outBuf.ReadFrom(outR)
	errBuf.ReadFrom(errR)

	return outBuf.String(), errBuf.String(), code
}

func TestVersionCommand(t *testing.T) {
	stdout, stderr, code := captureRun(t, []string{"version"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	want := "bdd version " + version + " (" + commit + ")"
	if strings.TrimSpace(stdout) != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestHelpCommand(t *testing.T) {
	for _, args := range [][]string{{"help"}, {}} {
		stdout, stderr, code := captureRun(t, args)
		if code != 0 {
			t.Fatalf("run(%v) exit code = %d, want 0", args, code)
		}
		if stderr != "" {
			t.Fatalf("run(%v) stderr = %q, want empty", args, stderr)
		}
		if !strings.Contains(stdout, "Usage:") {
			t.Fatalf("run(%v) stdout = %q, want help text", args, stdout)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	stdout, stderr, code := captureRun(t, []string{"nope"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "nope") {
		t.Fatalf("stderr = %q, want mention of unknown command", stderr)
	}
}
