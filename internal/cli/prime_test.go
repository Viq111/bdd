package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPrimeHumanOutputListsSupportedCommandsAndMemories(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "remember", "hello there", "--key", "greeting"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(remember) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(prime) exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"update <id> --claim", "ready --explain", "note <id>", "rune put", "snapshot [--output", "restore <snapshot.sqlite>", "greeting"} {
		if !strings.Contains(out, want) {
			t.Fatalf("prime output missing %q; got:\n%s", want, out)
		}
	}

	// Every command name prime advertises must also be one Run's switch
	// actually dispatches (plan section 19): "worktree" and "claim" are
	// never top-level commands, only flags, so must not appear as such.
	if strings.Contains(out, "\n  worktree ") || strings.Contains(out, "\n  claim ") {
		t.Fatalf("prime output advertises a non-existent top-level command; got:\n%s", out)
	}
}

func TestPrimeJSONIncludesMemories(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "remember", "first", "--key", "a"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(remember) exit = %d", code)
	}
	stdout.Reset()
	code = Run([]string{"--workspace", dir, "remember", "second", "--key", "b"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(remember) exit = %d", code)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime", "--json"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(prime --json) exit = %d, stderr = %q", code, stderr.String())
	}

	var result PrimeResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if result.MemoryCount != 2 {
		t.Fatalf("result.MemoryCount = %d, want 2", result.MemoryCount)
	}
	if len(result.Memories) != 2 {
		t.Fatalf("len(result.Memories) = %d, want 2", len(result.Memories))
	}
	if result.SchemaVersion == 0 {
		t.Fatal("result.SchemaVersion = 0, want > 0")
	}
}

func TestPrimeMemoryLimit(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	for _, key := range []string{"a", "b", "c"} {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"--workspace", dir, "remember", "body-" + key, "--key", key}, &stdout, &stderr, "dev")
		if code != ExitSuccess {
			t.Fatalf("Run(remember) exit = %d", code)
		}
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "prime", "--memory-limit", "1", "--json"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(prime) exit = %d, stderr = %q", code, stderr.String())
	}

	var result PrimeResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	if result.MemoryCount != 3 {
		t.Fatalf("result.MemoryCount = %d, want 3", result.MemoryCount)
	}
	if len(result.Memories) != 1 {
		t.Fatalf("len(result.Memories) = %d, want 1", len(result.Memories))
	}
	if result.MemoryLimit == nil || *result.MemoryLimit != 1 {
		t.Fatalf("result.MemoryLimit = %v, want 1", result.MemoryLimit)
	}
}

func TestPrimeNoMemories(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "remember", "hidden", "--key", "secret"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(remember) exit = %d", code)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime", "--no-memories"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(prime) exit = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "hidden") {
		t.Fatalf("stdout = %q, want no memory content with --no-memories", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime", "--no-memories", "--json"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(prime --json) exit = %d, stderr = %q", code, stderr.String())
	}
	var result PrimeResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	if result.Memories != nil {
		t.Fatalf("result.Memories = %v, want nil with --no-memories", result.Memories)
	}
}

func TestPrimeRejectsCombiningLimitAndNoMemories(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "prime", "--memory-limit", "1", "--no-memories"}, &stdout, &stderr, "dev")
	if code != ExitUsage {
		t.Fatalf("Run(prime) exit = %d, want %d", code, ExitUsage)
	}
}
