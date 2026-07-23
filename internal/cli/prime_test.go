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

func TestPrimeCompactHumanOutputListsRulesWorkflowAndContext(t *testing.T) {
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
	for _, want := range []string{
		"bdd prime contract v2",
		"workspace:",
		"schema:    current",
		"Rules:",
		"Workflow:",
		"discover:",
		"bdd ready",
		"claim:",
		"bdd update <id> --claim",
		"Context:",
		"greeting",
		"Omitted:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("prime output missing %q; got:\n%s", want, out)
		}
	}
}

func TestPrimeCompactJSONShape(t *testing.T) {
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
	if result.ContractVersion != primeContractVersion {
		t.Fatalf("result.ContractVersion = %d, want %d", result.ContractVersion, primeContractVersion)
	}
	if result.SchemaState != "current" {
		t.Fatalf("result.SchemaState = %q, want %q", result.SchemaState, "current")
	}
	if len(result.Rules) == 0 || len(result.Rules) > 8 {
		t.Fatalf("len(result.Rules) = %d, want (0, 8]", len(result.Rules))
	}
	if len(result.Workflow) == 0 {
		t.Fatalf("result.Workflow is empty")
	}
	for _, step := range result.Workflow {
		if len(step.Argv) == 0 || step.Argv[0] != "bdd" {
			t.Fatalf("workflow step %q argv = %v, want argv[0] = \"bdd\"", step.Action, step.Argv)
		}
	}
	if result.Omitted.Memories.Total != 2 {
		t.Fatalf("result.Omitted.Memories.Total = %d, want 2", result.Omitted.Memories.Total)
	}
	if result.Omitted.Memories.Returned != 2 {
		t.Fatalf("result.Omitted.Memories.Returned = %d, want 2", result.Omitted.Memories.Returned)
	}
	memCount := 0
	for _, e := range result.OptionalContext {
		if e.Type == "memory" {
			memCount++
		}
	}
	if memCount != 2 {
		t.Fatalf("optional_context memory entries = %d, want 2", memCount)
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
	if result.Omitted.Memories.Total != 3 {
		t.Fatalf("result.Omitted.Memories.Total = %d, want 3", result.Omitted.Memories.Total)
	}
	if result.Omitted.Memories.Optional != 3 {
		t.Fatalf("result.Omitted.Memories.Optional = %d, want 3", result.Omitted.Memories.Optional)
	}
	if result.Omitted.Memories.Returned != 1 {
		t.Fatalf("result.Omitted.Memories.Returned = %d, want 1", result.Omitted.Memories.Returned)
	}
	if len(result.Omitted.Memories.NextCommand) == 0 {
		t.Fatalf("result.Omitted.Memories.NextCommand is empty, want a next-page command")
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
	if strings.Contains(stdout.String(), "hidden") || strings.Contains(stdout.String(), "secret") {
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
	if result.Omitted.Memories.Total != 0 || result.Omitted.Memories.Returned != 0 {
		t.Fatalf("result.Omitted.Memories = %+v, want zero with --no-memories", result.Omitted.Memories)
	}
}

func TestPrimeRequiredRuneInlined(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "rune", "set", "role/programmer", "--kind", "role", "--title", "Programmer", "--body", "Write the code.", "--prime", "required"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune set) exit = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	code = Run([]string{"--workspace", dir, "rune", "set", "doc/style", "--kind", "doc", "--title", "Style guide", "--body", "Use tabs.", "--prime", "optional"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune set) exit = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	code = Run([]string{"--workspace", dir, "rune", "set", "doc/scratch", "--kind", "doc", "--title", "Scratch", "--body", "Not needed.", "--prime", "never"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune set) exit = %d, stderr = %q", code, stderr.String())
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
	if len(result.RequiredContext) != 1 || result.RequiredContext[0].Key != "role/programmer" {
		t.Fatalf("result.RequiredContext = %+v, want exactly role/programmer", result.RequiredContext)
	}
	if result.RequiredContext[0].Type != "rune" {
		t.Fatalf("result.RequiredContext[0].Type = %q, want rune", result.RequiredContext[0].Type)
	}
	if result.RequiredContext[0].Body != "Write the code." {
		t.Fatalf("result.RequiredContext[0].Body = %q, want full body", result.RequiredContext[0].Body)
	}

	foundOptional := false
	for _, e := range result.OptionalContext {
		if e.Type == "rune" {
			if e.Key == "doc/scratch" {
				t.Fatalf("prime:never rune doc/scratch leaked into optional_context: %+v", e)
			}
			if e.Key == "doc/style" {
				foundOptional = true
			}
		}
	}
	if !foundOptional {
		t.Fatalf("expected doc/style in optional_context, got %+v", result.OptionalContext)
	}
	if result.Omitted.Runes.Required != 1 || result.Omitted.Runes.Optional != 1 || result.Omitted.Runes.Never != 1 {
		t.Fatalf("result.Omitted.Runes = %+v, want required=1 optional=1 never=1", result.Omitted.Runes)
	}

	// Human output must inline the required rune's body and must never
	// print the never-prime rune's body or key.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(prime) exit = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Write the code.") {
		t.Fatalf("prime output missing required rune body; got:\n%s", out)
	}
	if strings.Contains(out, "doc/scratch") || strings.Contains(out, "Not needed.") {
		t.Fatalf("prime output leaked a prime:never rune; got:\n%s", out)
	}
}

func TestPrimeRequiredMemoryInlined(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "memory", "set", "always run the race tests", "--key", "testing-race", "--prime", "required"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory set) exit = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	code = Run([]string{"--workspace", dir, "memory", "set", "prefer small PRs", "--key", "pr-size"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory set) exit = %d, stderr = %q", code, stderr.String())
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

	var required *PrimeRequiredEntry
	for i, e := range result.RequiredContext {
		if e.Type == "memory" {
			required = &result.RequiredContext[i]
		}
	}
	if required == nil {
		t.Fatalf("no required memory in result.RequiredContext = %+v", result.RequiredContext)
	}
	if required.Key != "testing-race" || required.Body != "always run the race tests" {
		t.Fatalf("required memory = %+v, want key=testing-race with full body", required)
	}

	foundOptional := false
	for _, e := range result.OptionalContext {
		if e.Type == "memory" {
			if e.Key == "testing-race" {
				t.Fatalf("prime:required memory testing-race leaked into optional_context: %+v", e)
			}
			if e.Key == "pr-size" {
				foundOptional = true
			}
		}
	}
	if !foundOptional {
		t.Fatalf("expected pr-size in optional_context, got %+v", result.OptionalContext)
	}
	if result.Omitted.Memories.Total != 2 || result.Omitted.Memories.Required != 1 || result.Omitted.Memories.Optional != 1 {
		t.Fatalf("result.Omitted.Memories = %+v, want total=2 required=1 optional=1", result.Omitted.Memories)
	}

	// Human output must inline the required memory's body.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(prime) exit = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "always run the race tests") {
		t.Fatalf("prime output missing required memory body; got:\n%s", out)
	}
	if !strings.Contains(out, "[required] memory testing-race") {
		t.Fatalf("prime output missing required memory marker; got:\n%s", out)
	}
}

func TestPrimeRequiredContextOverBudgetNamesBothRunesAndMemories(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	huge := strings.Repeat("x", primeRequiredBudgetBytes)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "rune", "set", "role/huge", "--kind", "role", "--title", "Huge", "--body", huge, "--prime", "required"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune set) exit = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	code = Run([]string{"--workspace", dir, "memory", "set", "also huge", "--key", "huge-memory", "--prime", "required"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory set) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime"}, &stdout, &stderr, "dev", "unspecified")
	if code == ExitSuccess {
		t.Fatalf("Run(prime) exit = %d, want non-success when combined required context exceeds the budget", code)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want no truncated body on budget failure", stdout.String())
	}
	if !strings.Contains(stderr.String(), "bdd rune get role/huge") {
		t.Fatalf("stderr = %q, want it to name the rune retrieval command", stderr.String())
	}
	if !strings.Contains(stderr.String(), "bdd memory get huge-memory") {
		t.Fatalf("stderr = %q, want it to name the memory retrieval command", stderr.String())
	}
}

func TestPrimeRequiredRuneOverBudgetFails(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	huge := strings.Repeat("x", primeRequiredBudgetBytes+1)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "rune", "set", "role/huge", "--kind", "role", "--title", "Huge", "--body", huge, "--prime", "required"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune set) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime"}, &stdout, &stderr, "dev", "unspecified")
	if code == ExitSuccess {
		t.Fatalf("Run(prime) exit = %d, want non-success when required runes exceed the budget", code)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want no truncated body on budget failure", stdout.String())
	}
	if !strings.Contains(stderr.String(), "bdd rune get role/huge") {
		t.Fatalf("stderr = %q, want it to name the retrieval command", stderr.String())
	}
}

func TestPrimeFullReproducesProseContract(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "memory", "set", "hello there", "--key", "greeting"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory set) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime", "--full"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(prime --full) exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"update <id> --claim", "ready --explain", "note <id>", "rune set", "snapshot [--output", "restore <snapshot.sqlite>", "greeting"} {
		if !strings.Contains(out, want) {
			t.Fatalf("prime --full output missing %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\n  worktree ") || strings.Contains(out, "\n  claim ") {
		t.Fatalf("prime --full output advertises a non-existent top-level command; got:\n%s", out)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "prime", "--full", "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(prime --full --json) exit = %d, stderr = %q", code, stderr.String())
	}
	var result PrimeFullResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if result.MemoryCount != 1 || len(result.Memories) != 1 {
		t.Fatalf("result = %+v, want one memory", result)
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
	if !strings.Contains(out, "schema:    upgrade_pending") {
		t.Fatalf("prime output missing upgrade_pending schema state; got:\n%s", out)
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

func TestPrimeUnknownFlagVsArgument(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	cases := []struct {
		arg  string
		want string
	}{
		{"--db", `unknown flag "--db"`},
		{"--db=/tmp/example.sqlite", `unknown flag "--db=/tmp/example.sqlite"`},
		{"bogus", `unknown argument "bogus"`},
	}
	for _, tc := range cases {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"--workspace", dir, "prime", tc.arg}, &stdout, &stderr, "dev", "unspecified")
		if code != ExitUsage {
			t.Fatalf("Run(prime %s) exit = %d, want %d", tc.arg, code, ExitUsage)
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Fatalf("Run(prime %s) stderr = %q, want it to contain %q", tc.arg, stderr.String(), tc.want)
		}
	}
}
