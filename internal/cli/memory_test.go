package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRememberWithKeyAndRecall(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"remember", "--workspace", dir, "--key", "testing-race", "Always run the race tests"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(remember) exit = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "testing-race" {
		t.Fatalf("stdout = %q, want %q", got, "testing-race")
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"recall", "--workspace", dir, "testing-race"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(recall) exit = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "Always run the race tests" {
		t.Fatalf("stdout = %q, want %q", got, "Always run the race tests")
	}
}

func TestRememberWithoutKeyDerivesOne(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"remember", "--workspace", dir, "--json", "Some memorable body"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(remember) exit = %d, stderr = %q", code, stderr.String())
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

func TestRememberStdin(t *testing.T) {
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
	code := Run([]string{"remember", "--workspace", dir, "--key", "from-pipe", "--stdin"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(remember --stdin) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"recall", "--workspace", dir, "from-pipe"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(recall) exit = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "from stdin" {
		t.Fatalf("stdout = %q, want %q", got, "from stdin")
	}
}

func TestRememberRejectsBodyAndStdinTogether(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"remember", "--workspace", dir, "body", "--stdin"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(remember) exit = %d, want %d", code, ExitUsage)
	}
}

func TestMemoriesQueryFiltersByKeyOrBody(t *testing.T) {
	dir := initTestWorkspace(t)

	run := func(args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		full := append([]string{"remember", "--workspace", dir}, args...)
		if code := Run(full, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
			t.Fatalf("Run(remember %v) exit = %d, stderr = %q", args, code, stderr.String())
		}
	}
	run("--key", "alpha", "about cats")
	run("--key", "beta", "about dogs")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"memories", "--workspace", dir, "--json", "cats"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(memories) exit = %d, stderr = %q", code, stderr.String())
	}
	var results []MemoryResult
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(results) != 1 || results[0].Key != "alpha" {
		t.Fatalf("results = %+v, want exactly [alpha]", results)
	}
}

func TestForgetDeletesMemory(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"remember", "--workspace", dir, "--key", "temp", "body"}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(remember) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"forget", "--workspace", dir, "temp"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(forget) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"recall", "--workspace", dir, "temp"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitNotFound {
		t.Fatalf("Run(recall) after forget exit = %d, want %d", code, ExitNotFound)
	}
}

func TestForgetMissingKeyIsNotFound(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"forget", "--workspace", dir, "does-not-exist"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitNotFound {
		t.Fatalf("Run(forget) exit = %d, want %d", code, ExitNotFound)
	}
}
