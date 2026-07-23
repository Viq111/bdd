package bdd

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/viq111/bdd/internal/schema"
	_ "modernc.org/sqlite"
)

func TestInitCreatesWorkspaceDatabase(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	db, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer db.Close()

	dbPath := filepath.Join(dir, ".bdd", "bdd.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected database at %s: %v", dbPath, err)
	}

	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer raw.Close()

	var prefix string
	if err := raw.QueryRowContext(ctx, "SELECT prefix FROM workspace WHERE singleton = 1").Scan(&prefix); err != nil {
		t.Fatalf("reading workspace row: %v", err)
	}
	if prefix != "bdd" {
		t.Fatalf("workspace.prefix = %q, want %q", prefix, "bdd")
	}

	v, err := schema.ReadVersion(ctx, raw)
	if err != nil {
		t.Fatalf("ReadVersion() error = %v", err)
	}
	if v != schema.CurrentVersion() {
		t.Fatalf("schema version = %d, want %d", v, schema.CurrentVersion())
	}
}

func TestInitHonorsExplicitDBPath(t *testing.T) {
	workspaceDir := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "custom.sqlite")
	ctx := context.Background()

	db, err := Init(ctx, InitOptions{Workspace: workspaceDir, DBPath: dbPath, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer db.Close()

	if db.Path() != dbPath {
		t.Fatalf("db.Path() = %q, want %q", db.Path(), dbPath)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected database at %s: %v", dbPath, err)
	}

	defaultPath := filepath.Join(workspaceDir, ".bdd", "bdd.sqlite")
	if _, err := os.Stat(defaultPath); err == nil {
		t.Fatalf("expected no database at workspace-derived path %s, DBPath should have taken precedence", defaultPath)
	}
}

func TestInitFailsIfDatabaseAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	db, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("first Init() error = %v", err)
	}
	db.Close()

	_, err = Init(ctx, InitOptions{Workspace: dir, Prefix: "other"})
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second Init() error = %v, want ErrAlreadyExists", err)
	}
}

func TestInitFailsIfIncompatibleFileAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".bdd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".bdd", "bdd.sqlite"), []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Init(context.Background(), InitOptions{Workspace: dir, Prefix: "bdd"})
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("Init() error = %v, want ErrAlreadyExists", err)
	}
}

func TestInitRejectsInvalidPrefix(t *testing.T) {
	dir := t.TempDir()
	for _, prefix := range []string{"", "Bdd", "1bdd", "bdd space", "-bdd"} {
		_, err := Init(context.Background(), InitOptions{Workspace: dir, Prefix: prefix})
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("Init(prefix=%q) error = %v, want *ValidationError", prefix, err)
		}
		if prefix == "" {
			if verr.Detail != "" {
				t.Fatalf("Init(prefix=%q) Detail = %q, want empty for a genuinely missing value", prefix, verr.Detail)
			}
			continue
		}
		// A non-empty but malformed prefix was supplied, so the message must
		// not claim the field is missing.
		if verr.Detail == "" {
			t.Fatalf("Init(prefix=%q) Detail is empty, want an explanation of why the supplied prefix is invalid", prefix)
		}
		if msg := err.Error(); strings.Contains(msg, "missing required field") {
			t.Fatalf("Init(prefix=%q) error = %q, must not claim prefix is missing when a value was supplied", prefix, msg)
		}
	}
}

func TestOpenWithExplicitPath(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	init, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	init.Close()

	dbPath := filepath.Join(dir, ".bdd", "bdd.sqlite")
	db, err := Open(ctx, OpenOptions{Path: dbPath})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	if db.path != dbPath {
		t.Fatalf("db.path = %q, want %q", db.path, dbPath)
	}
}

func TestOpenDoesNotMutateJournalMode(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	init, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	init.Close()

	dbPath := filepath.Join(dir, ".bdd", "bdd.sqlite")

	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := raw.ExecContext(ctx, "PRAGMA journal_mode = DELETE"); err != nil {
		raw.Close()
		t.Fatalf("PRAGMA journal_mode = DELETE: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	db, err := Open(ctx, OpenOptions{Path: dbPath})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	verify, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open(verify) error = %v", err)
	}
	defer verify.Close()

	var mode string
	if err := verify.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "delete" {
		t.Fatalf("journal_mode = %q, want %q (normal Open must not rewrite it)", mode, "delete")
	}
}

func TestOpenWithExplicitPathMissingReturnsNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(context.Background(), OpenOptions{Path: filepath.Join(dir, "missing.sqlite")})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open() error = %v, want ErrNotFound", err)
	}
}

func TestOpenDiscoversWorkspaceWalkingUpward(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	init, err := Init(ctx, InitOptions{Workspace: root, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	init.Close()

	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	db, err := Open(ctx, OpenOptions{Workspace: nested})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	wantPath := filepath.Join(root, ".bdd", "bdd.sqlite")
	if db.path != wantPath {
		t.Fatalf("db.path = %q, want %q", db.path, wantPath)
	}
}

func TestOpenDiscoveryNotFoundReturnsErrNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(context.Background(), OpenOptions{Workspace: dir})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open() error = %v, want ErrNotFound", err)
	}
}

func TestOpenReturnsErrSchemaTooNewForNewerDatabase(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	init, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	init.Close()

	dbPath := filepath.Join(dir, ".bdd", "bdd.sqlite")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := raw.ExecContext(ctx, "PRAGMA user_version = 999999"); err != nil {
		t.Fatalf("bumping user_version: %v", err)
	}
	raw.Close()

	_, err = Open(ctx, OpenOptions{Path: dbPath})
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Open() error = %v, want ErrSchemaTooNew", err)
	}
}

func TestOpenSucceedsAndUpgradeClearsTooOldSchema(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	dbPath := filepath.Join(dir, ".bdd", "bdd.sqlite")

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if err := raw.PingContext(ctx); err != nil {
		t.Fatalf("PingContext() error = %v", err)
	}
	raw.Close()

	db, err := Open(ctx, OpenOptions{Path: dbPath})
	if err != nil {
		t.Fatalf("Open() on schema-less database error = %v, want success (ErrSchemaTooOld is deferred to method calls)", err)
	}
	if !db.schemaTooOld {
		t.Fatal("schemaTooOld = false, want true for a fresh unversioned database")
	}

	if err := db.Upgrade(ctx); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if db.schemaTooOld {
		t.Fatal("schemaTooOld = true after Upgrade(), want false")
	}

	v, err := schema.ReadVersion(ctx, db.sql)
	if err != nil {
		t.Fatalf("ReadVersion() error = %v", err)
	}
	if v != schema.CurrentVersion() {
		t.Fatalf("schema version = %d, want %d", v, schema.CurrentVersion())
	}

	db.Close()
}

func TestCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	db, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}
}
