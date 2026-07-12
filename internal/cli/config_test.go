package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// initTestWorkspace initializes a fresh workspace under a temp dir and
// returns its path, failing the test on error.
func initTestWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"init", "--prefix", "acme", dir}, &stdout, &stderr, "dev"); code != ExitSuccess {
		t.Fatalf("Run(init) exit = %d, stderr = %q", code, stderr.String())
	}
	return dir
}

func TestConfigSetGetUnsetRoundTrip(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"config", "set", "--workspace", dir, "greeting", "hello"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(config set) exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "greeting=hello" {
		t.Fatalf("stdout = %q, want %q", got, "greeting=hello")
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"config", "get", "--workspace", dir, "--json", "greeting"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(config get) exit = %d, stderr = %q", code, stderr.String())
	}
	var got ConfigEntryResult
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if got.Key != "greeting" || got.Value != "hello" {
		t.Fatalf("got = %+v, want key=greeting value=hello", got)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"config", "unset", "--workspace", dir, "greeting"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(config unset) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"config", "get", "--workspace", dir, "greeting"}, &stdout, &stderr, "dev")
	if code != ExitNotFound {
		t.Fatalf("Run(config get) after unset exit = %d, want %d", code, ExitNotFound)
	}
}

func TestConfigListJSON(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"config", "set", "--workspace", dir, "a", "1"}, &stdout, &stderr, "dev"); code != ExitSuccess {
		t.Fatalf("Run(config set a) exit = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"config", "set", "--workspace", dir, "b", "2"}, &stdout, &stderr, "dev"); code != ExitSuccess {
		t.Fatalf("Run(config set b) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"config", "list", "--workspace", dir, "--json"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(config list) exit = %d, stderr = %q", code, stderr.String())
	}
	var entries []ConfigEntryResult
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Key != "a" || entries[1].Key != "b" {
		t.Fatalf("entries = %+v, want ordered a, b", entries)
	}
}

func TestConfigSetStatusCustomPreviewsReadinessImpact(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"config", "set", "--workspace", dir, "status.custom", "qa_testing:wip"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(config set status.custom) exit = %d, stderr = %q", code, stderr.String())
	}

	// No cards use qa_testing yet, so changing its category should report no
	// impact.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"config", "set", "--workspace", dir, "--json", "status.custom", "qa_testing:active"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(config set status.custom) exit = %d, stderr = %q", code, stderr.String())
	}
	var result ConfigSetResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(result.Impact) != 0 {
		t.Fatalf("result.Impact = %v, want empty (no cards use qa_testing)", result.Impact)
	}
}

func TestConfigSetRejectsBadStatusCategory(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"config", "set", "--workspace", dir, "status.custom", "oops:not_a_category"}, &stdout, &stderr, "dev")
	if code != ExitUsage {
		t.Fatalf("Run(config set) exit = %d, want %d, stderr = %q", code, ExitUsage, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestConfigGetMissingKeyIsNotFound(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"config", "get", "--workspace", dir, "does.not.exist"}, &stdout, &stderr, "dev")
	if code != ExitNotFound {
		t.Fatalf("Run(config get) exit = %d, want %d", code, ExitNotFound)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}
