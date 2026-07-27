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

	"github.com/viq111/bdd/internal/schema"
	_ "modernc.org/sqlite"
)

func TestStatusUpToDateAfterInit(t *testing.T) {
	dir := t.TempDir()

	var initOut, initErr bytes.Buffer
	if code := Run([]string{"init", "--prefix", "acme", dir}, &initOut, &initErr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(init) exit = %d, stderr = %q", code, initErr.String())
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"status", "--json", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(status) exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var result StatusResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if !result.UpToDate {
		t.Fatal("result.UpToDate = false, want true")
	}
	if result.Prefix == nil || *result.Prefix != "acme" {
		t.Fatalf("result.Prefix = %v, want \"acme\"", result.Prefix)
	}
	if result.Upgraded {
		t.Fatal("result.Upgraded = true, want false (no --upgrade requested)")
	}
	wantWorkspace, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Workspace != wantWorkspace {
		t.Fatalf("result.Workspace = %q, want %q", result.Workspace, wantWorkspace)
	}
}

func TestStatusUnknownFlagVsArgument(t *testing.T) {
	dir := t.TempDir()

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
		code := Run([]string{"status", "--workspace", dir, tc.arg}, &stdout, &stderr, "dev", "unspecified")
		if code != ExitUsage {
			t.Fatalf("Run(status %s) exit = %d, want %d", tc.arg, code, ExitUsage)
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Fatalf("Run(status %s) stderr = %q, want it to contain %q", tc.arg, stderr.String(), tc.want)
		}
	}
}

func TestStatusMissingDatabaseReturnsNotFound(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"status", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitNotFound {
		t.Fatalf("Run(status) exit = %d, want %d", code, ExitNotFound)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	const want = "bdd: no .bdd/bdd.sqlite found, init database with bdd init"
	if !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), want)
	}
	if strings.Contains(stderr.String(), "walking up from") {
		t.Fatalf("stderr = %q, should not leak the raw discovery error", stderr.String())
	}
}

func TestStatusUpgradeAppliesMigrations(t *testing.T) {
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
	code := Run([]string{"status", "--json", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(status) exit = %d, stderr = %q", code, stderr.String())
	}
	var before StatusResult
	if err := json.Unmarshal(stdout.Bytes(), &before); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if before.UpToDate {
		t.Fatal("before.UpToDate = true, want false for an unversioned database")
	}
	if before.Prefix != nil {
		t.Fatalf("before.Prefix = %v, want nil (workspace table doesn't exist yet)", before.Prefix)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"status", "--json", "--workspace", dir, "--upgrade"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(status --upgrade) exit = %d, stderr = %q", code, stderr.String())
	}

	var after StatusResult
	if err := json.Unmarshal(stdout.Bytes(), &after); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !after.Upgraded {
		t.Fatal("after.Upgraded = false, want true")
	}
	if !after.UpToDate {
		t.Fatal("after.UpToDate = false, want true after upgrade")
	}
	if after.SchemaVersion != schema.CurrentVersion() {
		t.Fatalf("after.SchemaVersion = %d, want %d", after.SchemaVersion, schema.CurrentVersion())
	}
}

func TestStatusSilentEmitsOnlyDatabasePath(t *testing.T) {
	dir := t.TempDir()

	var initOut, initErr bytes.Buffer
	if code := Run([]string{"init", "--prefix", "acme", dir}, &initOut, &initErr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(init) exit = %d, stderr = %q", code, initErr.String())
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"status", "--silent", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(status) exit = %d, stderr = %q", code, stderr.String())
	}

	wantDB := filepath.Join(dir, ".bdd", "bdd.sqlite")
	got := stdout.String()
	if got != wantDB+"\n" {
		t.Fatalf("stdout = %q, want %q", got, wantDB+"\n")
	}
}

func writeHooksYAML(t *testing.T, dir, content string) {
	t.Helper()
	path := filepath.Join(dir, ".bdd", "hooks.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func statusHooks(t *testing.T, args ...string) HooksResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"status", "--json"}, args...), &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(status) exit = %d, stderr = %q", code, stderr.String())
	}
	var result StatusResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	return result.Hooks
}

func TestStatusHooksNoFile(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	h := statusHooks(t, "--workspace", dir)
	if h.Present {
		t.Fatalf("Hooks.Present = true, want false")
	}
	if h.Active {
		t.Fatalf("Hooks.Active = true, want false")
	}
}

func TestStatusHooksPresentButDisabled(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	writeHooksYAML(t, dir, `
version: 1
hooks:
  - event: status-change
    command: ["true"]
`)

	h := statusHooks(t, "--workspace", dir)
	if !h.Present {
		t.Fatalf("Hooks.Present = false, want true")
	}
	if h.Error != "" {
		t.Fatalf("Hooks.Error = %q, want empty", h.Error)
	}
	if h.Enabled {
		t.Fatalf("Hooks.Enabled = true, want false (no opt-in)")
	}
	if h.Active {
		t.Fatalf("Hooks.Active = true, want false (no opt-in)")
	}
}

func TestStatusHooksActive(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	writeHooksYAML(t, dir, `
version: 1
hooks:
  - event: status-change
    to_status: [awaiting_review]
    command: ["true"]
  - event: label-change
    added: [review-approved]
    command: ["true"]
`)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"config", "set", "hooks.enabled", "true", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(config set) exit = %d, stderr = %q", code, stderr.String())
	}

	h := statusHooks(t, "--workspace", dir)
	if !h.Present || !h.Enabled || !h.Active {
		t.Fatalf("Hooks = %+v, want present, enabled, and active", h)
	}
	if h.HookCount != 2 {
		t.Fatalf("Hooks.HookCount = %d, want 2", h.HookCount)
	}
}

func TestStatusHooksInvalidFile(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	writeHooksYAML(t, dir, `
version: 1
hooks:
  - event: bogus
    command: ["true"]
`)

	h := statusHooks(t, "--workspace", dir)
	if !h.Present {
		t.Fatalf("Hooks.Present = false, want true")
	}
	if h.Error == "" {
		t.Fatalf("Hooks.Error = empty, want a parse error")
	}
	if h.Active {
		t.Fatalf("Hooks.Active = true, want false for an invalid file")
	}
}

func TestStatusHooksNoHooksFlagForcesDisabled(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	writeHooksYAML(t, dir, `
version: 1
hooks:
  - event: status-change
    command: ["true"]
`)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"config", "set", "hooks.enabled", "true", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(config set) exit = %d, stderr = %q", code, stderr.String())
	}

	h := statusHooks(t, "--workspace", dir, "--no-hooks")
	if !h.Enabled {
		t.Fatalf("Hooks.Enabled = false, want true (config gate unaffected by --no-hooks)")
	}
	if h.Active {
		t.Fatalf("Hooks.Active = true, want false with --no-hooks")
	}
}

func TestStatusHooksEnvVarForcesDisabled(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	writeHooksYAML(t, dir, `
version: 1
hooks:
  - event: status-change
    command: ["true"]
`)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"config", "set", "hooks.enabled", "true", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(config set) exit = %d, stderr = %q", code, stderr.String())
	}

	t.Setenv("BDD_NO_HOOKS", "1")
	h := statusHooks(t, "--workspace", dir)
	if h.Active {
		t.Fatalf("Hooks.Active = true, want false with BDD_NO_HOOKS=1")
	}
}
