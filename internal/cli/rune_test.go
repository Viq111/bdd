package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunePutShowRoundTrip(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "put", "--workspace", dir, "role/programmer",
		"--kind", "role", "--title", "Programmer", "--body", "Implement things."}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune put) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "show", "--workspace", dir, "--json", "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune show) exit = %d, stderr = %q", code, stderr.String())
	}
	var r RuneResult
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if r.Key != "role/programmer" || r.Kind != "role" || r.Title != "Programmer" || r.Body != "Implement things." {
		t.Fatalf("r = %+v, unexpected", r)
	}
	if !r.Enabled {
		t.Fatal("r.Enabled = false, want true (default)")
	}
	if r.Revision != 1 {
		t.Fatalf("r.Revision = %d, want 1", r.Revision)
	}
}

func TestRunePutBodyFile(t *testing.T) {
	dir := initTestWorkspace(t)
	bodyPath := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyPath, []byte("file body"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "put", "--workspace", dir, "policy/conventions",
		"--kind", "policy", "--title", "Conventions", "--body-file", bodyPath}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune put --body-file) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "show", "--workspace", dir, "--json", "policy/conventions"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune show) exit = %d, stderr = %q", code, stderr.String())
	}
	var r RuneResult
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if r.Body != "file body" {
		t.Fatalf("r.Body = %q, want %q", r.Body, "file body")
	}
}

func TestRuneListAndSearch(t *testing.T) {
	dir := initTestWorkspace(t)

	put := func(key, kind, title, body string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		args := []string{"rune", "put", "--workspace", dir, key, "--kind", kind, "--title", title, "--body", body}
		if code := Run(args, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
			t.Fatalf("Run(rune put %s) exit = %d, stderr = %q", key, code, stderr.String())
		}
	}
	put("role/programmer", "role", "Programmer", "Go implementation work.")
	put("role/qa", "role", "QA", "Reviews changes.")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "list", "--workspace", dir, "--json", "--kind", "role"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune list) exit = %d, stderr = %q", code, stderr.String())
	}
	var summaries []RuneSummaryResult
	if err := json.Unmarshal(stdout.Bytes(), &summaries); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(summaries) != 2 {
		t.Fatalf("len(summaries) = %d, want 2", len(summaries))
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "search", "--workspace", dir, "--json", "Go implementation"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune search) exit = %d, stderr = %q", code, stderr.String())
	}
	var found []RuneSummaryResult
	if err := json.Unmarshal(stdout.Bytes(), &found); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(found) != 1 || found[0].Key != "role/programmer" {
		t.Fatalf("found = %+v, want exactly [role/programmer]", found)
	}
}

func TestRuneEnableDisable(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rune", "put", "--workspace", dir, "role/programmer", "--kind", "role", "--title", "P", "--body", "b"}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(rune put) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"rune", "disable", "--workspace", dir, "--json", "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune disable) exit = %d, stderr = %q", code, stderr.String())
	}
	var r RuneResult
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if r.Enabled {
		t.Fatal("r.Enabled = true after disable, want false")
	}

	// Disabled runes are excluded from list without --all.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "list", "--workspace", dir, "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune list) exit = %d, stderr = %q", code, stderr.String())
	}
	var summaries []RuneSummaryResult
	if err := json.Unmarshal(stdout.Bytes(), &summaries); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(summaries) != 0 {
		t.Fatalf("len(summaries) = %d, want 0 (disabled rune hidden by default)", len(summaries))
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "list", "--workspace", dir, "--json", "--all"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune list --all) exit = %d, stderr = %q", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &summaries); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(summaries) != 1 {
		t.Fatalf("len(summaries) = %d, want 1 with --all", len(summaries))
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "enable", "--workspace", dir, "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune enable) exit = %d, stderr = %q", code, stderr.String())
	}
}

func TestRuneRemove(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rune", "put", "--workspace", dir, "role/programmer", "--kind", "role", "--title", "P", "--body", "b"}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(rune put) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"rune", "remove", "--workspace", dir, "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune remove) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "show", "--workspace", dir, "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitNotFound {
		t.Fatalf("Run(rune show) after remove exit = %d, want %d", code, ExitNotFound)
	}
}

func TestRuneProtectedRequiresForce(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "put", "--workspace", dir, "role/programmer",
		"--kind", "role", "--title", "P", "--body", "b", "--protected"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune put --protected) exit = %d, stderr = %q", code, stderr.String())
	}

	// Update without --force must fail with ExitConflict (4).
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "put", "--workspace", dir, "role/programmer",
		"--kind", "role", "--body", "changed"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitConflict {
		t.Fatalf("Run(rune put, protected, no force) exit = %d, want %d, stderr = %q", code, ExitConflict, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty on failure", stdout.String())
	}

	// Disable without --force must also fail with ExitConflict.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "disable", "--workspace", dir, "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitConflict {
		t.Fatalf("Run(rune disable, protected, no force) exit = %d, want %d", code, ExitConflict)
	}

	// Remove without --force must also fail with ExitConflict.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "remove", "--workspace", dir, "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitConflict {
		t.Fatalf("Run(rune remove, protected, no force) exit = %d, want %d", code, ExitConflict)
	}

	// With --force, the update succeeds.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "put", "--workspace", dir, "role/programmer",
		"--kind", "role", "--body", "changed", "--force"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune put, protected, --force) exit = %d, stderr = %q", code, stderr.String())
	}
}

func TestRuneExportMarkdown(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rune", "put", "--workspace", dir, "role/programmer", "--kind", "role", "--title", "Programmer", "--body", "Body text"}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(rune put) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"rune", "export", "--workspace", dir, "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune export) exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "# Programmer") {
		t.Fatalf("stdout = %q, want markdown export with title heading", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "export", "--workspace", dir, "role/programmer", "--format", "json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune export --format json) exit = %d, stderr = %q", code, stderr.String())
	}
	var exported map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &exported); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if exported["key"] != "role/programmer" {
		t.Fatalf("exported = %+v, want key=role/programmer", exported)
	}
}

func TestRuneShowMissingIsNotFound(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "show", "--workspace", dir, "role/nope"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitNotFound {
		t.Fatalf("Run(rune show) exit = %d, want %d", code, ExitNotFound)
	}
}

func TestRunePutCreateOnlyRejectsExisting(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rune", "put", "--workspace", dir, "role/programmer", "--kind", "role", "--title", "P", "--body", "b"}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(rune put) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"rune", "put", "--workspace", dir, "role/programmer", "--kind", "role", "--body", "b2", "--create-only"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitConflict {
		t.Fatalf("Run(rune put --create-only) exit = %d, want %d", code, ExitConflict)
	}
}
