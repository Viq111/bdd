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
	if code := Run([]string{"init", "--prefix", "acme", dir}, &initOut, &initErr, "dev"); code != ExitSuccess {
		t.Fatalf("Run(init) exit = %d, stderr = %q", code, initErr.String())
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"status", "--json", "--workspace", dir}, &stdout, &stderr, "dev")
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

func TestStatusMissingDatabaseReturnsNotFound(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"status", "--workspace", dir}, &stdout, &stderr, "dev")
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
	code := Run([]string{"status", "--json", "--workspace", dir}, &stdout, &stderr, "dev")
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
	code = Run([]string{"status", "--json", "--workspace", dir, "--upgrade"}, &stdout, &stderr, "dev")
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
	if code := Run([]string{"init", "--prefix", "acme", dir}, &initOut, &initErr, "dev"); code != ExitSuccess {
		t.Fatalf("Run(init) exit = %d, stderr = %q", code, initErr.String())
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"status", "--silent", "--workspace", dir}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(status) exit = %d, stderr = %q", code, stderr.String())
	}

	wantDB := filepath.Join(dir, ".bdd", "bdd.sqlite")
	got := stdout.String()
	if got != wantDB+"\n" {
		t.Fatalf("stdout = %q, want %q", got, wantDB+"\n")
	}
}
