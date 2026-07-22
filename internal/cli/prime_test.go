package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestPrimeHumanOutputListsSupportedCommandsAndMemories(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "memory", "set", "hello there", "--key", "greeting"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory set) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(prime) exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"update <id> --claim", "ready --explain", "note <id>", "rune set", "snapshot [--output", "restore <snapshot.sqlite>", "greeting"} {
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
	code := Run([]string{"--workspace", dir, "memory", "set", "first", "--key", "a"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory set) exit = %d", code)
	}
	stdout.Reset()
	code = Run([]string{"--workspace", dir, "memory", "set", "second", "--key", "b"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory set) exit = %d", code)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime", "--json"}, &stdout, &stderr, "dev", "unspecified")
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
}

func TestPrimeMemoryLimit(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	for _, key := range []string{"a", "b", "c"} {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"--workspace", dir, "memory", "set", "body-" + key, "--key", key}, &stdout, &stderr, "dev", "unspecified")
		if code != ExitSuccess {
			t.Fatalf("Run(memory set) exit = %d", code)
		}
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "prime", "--memory-limit", "1", "--json"}, &stdout, &stderr, "dev", "unspecified")
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
	code := Run([]string{"--workspace", dir, "memory", "set", "hidden", "--key", "secret"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory set) exit = %d", code)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime", "--no-memories"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(prime) exit = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "hidden") {
		t.Fatalf("stdout = %q, want no memory content with --no-memories", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime", "--no-memories", "--json"}, &stdout, &stderr, "dev", "unspecified")
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

func TestPrimeHeaderOmitsDatabaseAndSchemaOnCurrentSchema(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "prime"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(prime) exit = %d, stderr = %q", code, stderr.String())
	}

	out := stdout.String()
	if strings.Contains(out, "database:") {
		t.Fatalf("prime output contains a database: line; got:\n%s", out)
	}
	if strings.Contains(out, "schema:") {
		t.Fatalf("prime output contains a schema: line; got:\n%s", out)
	}
	if strings.Contains(out, "schema upgrade pending") {
		t.Fatalf("prime output contains the upgrade nudge on a current schema; got:\n%s", out)
	}
	if !strings.Contains(out, "workspace:") {
		t.Fatalf("prime output missing workspace: line; got:\n%s", out)
	}

	var result PrimeResult
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime", "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(prime --json) exit = %d, stderr = %q", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if strings.Contains(stdout.String(), `"database"`) || strings.Contains(stdout.String(), `"schema_version"`) {
		t.Fatalf("prime --json output contains database/schema_version keys; got:\n%s", stdout.String())
	}
}

func TestPrimeNudgesOnPendingUpgrade(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".bdd", "bdd.sqlite")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if err := raw.PingContext(context.Background()); err != nil {
		t.Fatalf("PingContext() error = %v", err)
	}
	raw.Close()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "prime"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(prime) exit = %d, stderr = %q", code, stderr.String())
	}

	out := stdout.String()
	const want = "⚠ schema upgrade pending — run `bdd status --upgrade`"
	if !strings.Contains(out, want) {
		t.Fatalf("prime output missing upgrade nudge; got:\n%s", out)
	}
	if strings.Contains(out, "database:") || strings.Contains(out, "schema:") {
		t.Fatalf("prime output still contains database/schema lines; got:\n%s", out)
	}
	if idx := strings.Index(out, want); idx == -1 || idx > strings.Index(out, "Commands:") {
		t.Fatalf("upgrade nudge must appear ahead of the commands contract; got:\n%s", out)
	}
}

func TestPrimeRejectsCombiningLimitAndNoMemories(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "prime", "--memory-limit", "1", "--no-memories"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(prime) exit = %d, want %d", code, ExitUsage)
	}
}
