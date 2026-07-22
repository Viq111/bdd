package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestMemorySetWithKeyAndGet(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"memory", "set", "--workspace", dir, "--key", "testing-race", "Always run the race tests"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory set) exit = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "testing-race" {
		t.Fatalf("stdout = %q, want %q", got, "testing-race")
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"memory", "get", "--workspace", dir, "testing-race"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory get) exit = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "Always run the race tests" {
		t.Fatalf("stdout = %q, want %q", got, "Always run the race tests")
	}
}

func TestMemorySetWithoutKeyDerivesOne(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"memory", "set", "--workspace", dir, "--json", "Some memorable body"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory set) exit = %d, stderr = %q", code, stderr.String())
	}
	var m MemoryResult
	if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if !strings.HasPrefix(m.Key, "some-memorable-body-") {
		t.Fatalf("m.Key = %q, want derived slug prefix", m.Key)
	}
	if m.Revision != 1 {
		t.Fatalf("m.Revision = %d, want 1", m.Revision)
	}
}

func TestMemorySetStdin(t *testing.T) {
	dir := initTestWorkspace(t)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("from stdin"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"memory", "set", "--workspace", dir, "--key", "from-pipe", "--stdin"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory set --stdin) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"memory", "get", "--workspace", dir, "from-pipe"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory get) exit = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "from stdin" {
		t.Fatalf("stdout = %q, want %q", got, "from stdin")
	}
}

func TestMemorySetRejectsBodyAndStdinTogether(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"memory", "set", "--workspace", dir, "body", "--stdin"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(memory set) exit = %d, want %d", code, ExitUsage)
	}
}

func TestMemoryListListsEverything(t *testing.T) {
	dir := initTestWorkspace(t)

	run := func(args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		full := append([]string{"memory", "set", "--workspace", dir}, args...)
		if code := Run(full, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
			t.Fatalf("Run(memory set %v) exit = %d, stderr = %q", args, code, stderr.String())
		}
	}
	run("--key", "alpha", "about cats")
	run("--key", "beta", "about dogs")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"memory", "list", "--workspace", dir, "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory list) exit = %d, stderr = %q", code, stderr.String())
	}
	var results []MemoryResult
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2 entries", results)
	}
}

func TestMemorySearchQueryFiltersByKeyOrBody(t *testing.T) {
	dir := initTestWorkspace(t)

	run := func(args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		full := append([]string{"memory", "set", "--workspace", dir}, args...)
		if code := Run(full, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
			t.Fatalf("Run(memory set %v) exit = %d, stderr = %q", args, code, stderr.String())
		}
	}
	run("--key", "alpha", "about cats")
	run("--key", "beta", "about dogs")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"memory", "search", "--workspace", dir, "--json", "cats"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory search) exit = %d, stderr = %q", code, stderr.String())
	}
	var results []MemoryResult
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(results) != 1 || results[0].Key != "alpha" {
		t.Fatalf("results = %+v, want exactly [alpha]", results)
	}
}

func TestMemoryRemoveDeletesMemory(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"memory", "set", "--workspace", dir, "--key", "temp", "body"}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(memory set) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"memory", "remove", "--workspace", dir, "temp"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memory remove) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"memory", "get", "--workspace", dir, "temp"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitNotFound {
		t.Fatalf("Run(memory get) after remove exit = %d, want %d", code, ExitNotFound)
	}
}

func TestMemoryRemoveMissingKeyIsNotFound(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"memory", "remove", "--workspace", dir, "does-not-exist"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitNotFound {
		t.Fatalf("Run(memory remove) exit = %d, want %d", code, ExitNotFound)
	}
}

// TestMemoryCommandsRejectRemovedDBFlag covers every memory command that
// takes a leading positional key/query: the removed global --db flag must
// be identified as an unknown flag rather than swallowed as the
// key/query or misreported through a wrong-arity error.
func TestMemoryCommandsRejectRemovedDBFlag(t *testing.T) {
	dir := initTestWorkspace(t)

	cases := []struct {
		name    string
		args    []string
		wantCmd string
	}{
		{"get", []string{"memory", "get", "somekey"}, "memory get"},
		{"remove", []string{"memory", "remove", "somekey"}, "memory remove"},
		{"search", []string{"memory", "search", "query"}, "memory search"},
		{"list", []string{"memory", "list"}, "memory list"},
	}
	dbForms := []struct {
		lead []string
		want string
	}{
		{[]string{"--db", "/tmp/example.sqlite"}, "--db"},
		{[]string{"--db=/tmp/example.sqlite"}, "--db=/tmp/example.sqlite"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, form := range dbForms {
				var stdout, stderr bytes.Buffer
				args := append(append([]string{}, form.lead...), tc.args...)
				args = append(args, "--workspace", dir)
				code := Run(args, &stdout, &stderr, "dev", "unspecified")
				if code != ExitUsage {
					t.Fatalf("Run(%v) exit = %d, want %d, stderr=%q", args, code, ExitUsage, stderr.String())
				}
				want := `bdd: ` + tc.wantCmd + `: unknown flag "` + form.want + `"`
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("Run(%v) stderr = %q, want it to contain %q", args, stderr.String(), want)
				}
			}
		})
	}
}
