package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitDefaultPrefixAndHumanOutput(t *testing.T) {
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "My Workspace")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"init", wsDir}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(init) exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	dbPath := filepath.Join(wsDir, ".bdd", "bdd.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected database at %s: %v", dbPath, err)
	}
	if !strings.Contains(stdout.String(), "prefix: my-workspace") {
		t.Fatalf("stdout = %q, want derived prefix \"my-workspace\"", stdout.String())
	}
}

func TestInitExplicitPrefixJSON(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"init", "--prefix", "acme", "--json", dir}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(init) exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var result InitResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if result.Prefix != "acme" {
		t.Fatalf("result.Prefix = %q, want %q", result.Prefix, "acme")
	}
	if result.SchemaVersion == 0 {
		t.Fatal("result.SchemaVersion = 0, want > 0")
	}
	wantDB := filepath.Join(dir, ".bdd", "bdd.sqlite")
	if result.Database != wantDB {
		t.Fatalf("result.Database = %q, want %q", result.Database, wantDB)
	}
}

func TestInitSilentEmitsOnlyDatabasePath(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"init", "--prefix", "acme", "--silent", dir}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(init) exit = %d, stderr = %q", code, stderr.String())
	}

	wantDB := filepath.Join(dir, ".bdd", "bdd.sqlite")
	if got := strings.TrimSpace(stdout.String()); got != wantDB {
		t.Fatalf("stdout = %q, want %q", got, wantDB)
	}
}

func TestInitHonorsExplicitDBFlag(t *testing.T) {
	workspaceDir := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "custom.sqlite")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"init", "--db", dbPath, "--prefix", "qadb", "--json", workspaceDir}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(init --db) exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected database at %s: %v", dbPath, err)
	}
	defaultDBPath := filepath.Join(workspaceDir, ".bdd", "bdd.sqlite")
	if _, err := os.Stat(defaultDBPath); err == nil {
		t.Fatalf("expected no database at workspace-derived path %s, --db should have taken precedence", defaultDBPath)
	}

	var result InitResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if result.Database != dbPath {
		t.Fatalf("result.Database = %q, want %q", result.Database, dbPath)
	}

	// bdd status against the same --db path must resolve the same database.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"status", "--db", dbPath, "--json"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(status --db) exit = %d, stderr = %q", code, stderr.String())
	}
	var status StatusResult
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if status.Database != dbPath {
		t.Fatalf("status.Database = %q, want %q", status.Database, dbPath)
	}
	if status.Prefix == nil || *status.Prefix != "qadb" {
		t.Fatalf("status.Prefix = %v, want \"qadb\"", status.Prefix)
	}
}

func TestInitFailsIfAlreadyExists(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"init", "--prefix", "acme", dir}, &stdout, &stderr, "dev"); code != ExitSuccess {
		t.Fatalf("first Run(init) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"init", "--prefix", "acme", dir}, &stdout, &stderr, "dev")
	if code != ExitConflict {
		t.Fatalf("second Run(init) exit = %d, want %d", code, ExitConflict)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty on failure", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr = empty, want a diagnostic")
	}
}

func TestInitRejectsUnknownFlag(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"init", "--bogus", dir}, &stdout, &stderr, "dev")
	if code != ExitUsage {
		t.Fatalf("Run(init) exit = %d, want %d", code, ExitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestDerivePrefix(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"/tmp/My Workspace", "my-workspace"},
		{"/tmp/123-abc", "abc"},
		{"/tmp/---", "bdd"},
		{"/tmp/Bdd_Project", "bdd-project"},
	}
	for _, tt := range tests {
		if got := derivePrefix(tt.dir); got != tt.want {
			t.Errorf("derivePrefix(%q) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}
