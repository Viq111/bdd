package bdd

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/viq111/bdd/internal/schema"
	"github.com/viq111/bdd/internal/sqlite"
	_ "modernc.org/sqlite"
)

func TestSnapshotDefaultsToBackupSqliteBesideTheDatabase(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	db, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer db.Close()

	result, err := db.Snapshot(ctx, SnapshotOptions{})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	wantPath := filepath.Join(dir, ".bdd", DefaultSnapshotName)
	if result.Path != wantPath {
		t.Fatalf("Snapshot().Path = %q, want %q", result.Path, wantPath)
	}
	if result.SchemaVersion != schema.CurrentVersion() {
		t.Fatalf("Snapshot().SchemaVersion = %d, want %d", result.SchemaVersion, schema.CurrentVersion())
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected snapshot at %s: %v", wantPath, err)
	}
}

func TestSnapshotProducesIntegrityCheckedCopyWithData(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	db, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer db.Close()

	if _, err := db.Remember(ctx, Remember{Key: "hello", Body: "world", Actor: "tester"}); err != nil {
		t.Fatalf("Remember() error = %v", err)
	}

	out := filepath.Join(dir, "snap.sqlite")
	if _, err := db.Snapshot(ctx, SnapshotOptions{Output: out}); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	raw, err := sql.Open(sqlite.DriverName, out)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer raw.Close()

	var result string
	if err := raw.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if result != "ok" {
		t.Fatalf("integrity_check = %q, want ok", result)
	}

	var body string
	if err := raw.QueryRowContext(ctx, "SELECT body FROM memories WHERE key = 'hello'").Scan(&body); err != nil {
		t.Fatalf("reading memory from snapshot: %v", err)
	}
	if body != "world" {
		t.Fatalf("memory body = %q, want %q", body, "world")
	}
}

func TestSnapshotFailsOnClosedDatabase(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	db, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	db.Close()

	if _, err := db.Snapshot(ctx, SnapshotOptions{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Snapshot() on closed db error = %v, want ErrInvalidArgument", err)
	}
}

func TestSnapshotSucceedsWhileWritesAreOngoing(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	db, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer db.Close()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := db.Remember(ctx, Remember{Key: "counter", Body: strconv.Itoa(i), Actor: "writer"}); err != nil {
				t.Errorf("Remember() error = %v", err)
				return
			}
			i++
		}
	}()

	for i := 0; i < 5; i++ {
		out := filepath.Join(dir, "snap-"+strconv.Itoa(i)+".sqlite")
		result, err := db.Snapshot(ctx, SnapshotOptions{Output: out})
		if err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("Snapshot() while writes ongoing error = %v", err)
		}
		if _, err := os.Stat(result.Path); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("expected snapshot at %s: %v", result.Path, err)
		}
	}

	close(stop)
	wg.Wait()
}

func TestRestoreRoundTripsData(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	db, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if _, err := db.Remember(ctx, Remember{Key: "hello", Body: "world", Actor: "tester"}); err != nil {
		t.Fatalf("Remember() error = %v", err)
	}

	snapPath := filepath.Join(dir, "snap.sqlite")
	if _, err := db.Snapshot(ctx, SnapshotOptions{Output: snapPath}); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if _, err := db.Remember(ctx, Remember{Key: "hello", Body: "changed", Actor: "tester"}); err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	db.Close()

	dbPath := filepath.Join(dir, ".bdd", "bdd.sqlite")
	result, err := Restore(ctx, RestoreOptions{Path: dbPath, Source: snapPath})
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if result.Path != dbPath {
		t.Fatalf("Restore().Path = %q, want %q", result.Path, dbPath)
	}
	wantBackup := filepath.Join(dir, ".bdd", DefaultSnapshotName)
	if result.BackupPath != wantBackup {
		t.Fatalf("Restore().BackupPath = %q, want %q", result.BackupPath, wantBackup)
	}
	if _, err := os.Stat(wantBackup); err != nil {
		t.Fatalf("expected pre-restore backup at %s: %v", wantBackup, err)
	}

	reopened, err := Open(ctx, OpenOptions{Path: dbPath})
	if err != nil {
		t.Fatalf("Open() after restore error = %v", err)
	}
	defer reopened.Close()

	m, err := reopened.Recall(ctx, "hello")
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if m.Body != "world" {
		t.Fatalf("restored memory body = %q, want %q (pre-change value)", m.Body, "world")
	}
}

func TestRestoreSkipBackupOmitsBackupFile(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	db, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	snapPath := filepath.Join(dir, "snap.sqlite")
	if _, err := db.Snapshot(ctx, SnapshotOptions{Output: snapPath}); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	db.Close()

	dbPath := filepath.Join(dir, ".bdd", "bdd.sqlite")
	result, err := Restore(ctx, RestoreOptions{Path: dbPath, Source: snapPath, SkipBackup: true})
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if result.BackupPath != "" {
		t.Fatalf("Restore().BackupPath = %q, want empty", result.BackupPath)
	}
	backupCandidate := filepath.Join(dir, ".bdd", DefaultSnapshotName)
	if _, err := os.Stat(backupCandidate); !os.IsNotExist(err) {
		t.Fatalf("expected no backup file at %s, stat err = %v", backupCandidate, err)
	}
}

func TestRestoreCreatesDatabaseWhenNoneExists(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	src, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	snapPath := filepath.Join(dir, "snap.sqlite")
	if _, err := src.Snapshot(ctx, SnapshotOptions{Output: snapPath}); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	src.Close()

	freshDir := t.TempDir()
	result, err := Restore(ctx, RestoreOptions{Workspace: freshDir, Source: snapPath})
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	wantPath := filepath.Join(freshDir, ".bdd", "bdd.sqlite")
	if result.Path != wantPath {
		t.Fatalf("Restore().Path = %q, want %q", result.Path, wantPath)
	}
	if result.BackupPath != "" {
		t.Fatalf("Restore().BackupPath = %q, want empty (nothing to back up)", result.BackupPath)
	}

	db, err := Open(ctx, OpenOptions{Path: wantPath})
	if err != nil {
		t.Fatalf("Open() after restore error = %v", err)
	}
	db.Close()
}

func TestRestoreRequiresSource(t *testing.T) {
	_, err := Restore(context.Background(), RestoreOptions{Path: filepath.Join(t.TempDir(), "bdd.sqlite")})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Restore() error = %v, want *ValidationError", err)
	}
}

func TestRestoreFailsForMissingSource(t *testing.T) {
	_, err := Restore(context.Background(), RestoreOptions{
		Path:   filepath.Join(t.TempDir(), "bdd.sqlite"),
		Source: filepath.Join(t.TempDir(), "does-not-exist.sqlite"),
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Restore() error = %v, want ErrNotFound", err)
	}
}

func TestRestoreFailsForCorruptSource(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad.sqlite")
	if err := os.WriteFile(badPath, []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Restore(context.Background(), RestoreOptions{
		Path:   filepath.Join(dir, "bdd.sqlite"),
		Source: badPath,
	})
	if err == nil {
		t.Fatal("Restore() error = nil, want an error for a corrupt/non-database source")
	}
}

func TestRestoreFailsForNewerSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	futurePath := filepath.Join(dir, "future.sqlite")
	raw, err := sql.Open(sqlite.DriverName, futurePath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if err := schema.Upgrade(ctx, raw); err != nil {
		raw.Close()
		t.Fatalf("schema.Upgrade() error = %v", err)
	}
	future := schema.CurrentVersion() + 1
	if _, err := raw.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(future)); err != nil {
		raw.Close()
		t.Fatalf("bumping user_version: %v", err)
	}
	raw.Close()

	_, err = Restore(ctx, RestoreOptions{
		Path:   filepath.Join(dir, "bdd.sqlite"),
		Source: futurePath,
	})
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Restore() error = %v, want ErrSchemaTooNew", err)
	}
}

func TestRestoreFailsWhenTargetIsOpenElsewhere(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	db, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	snapPath := filepath.Join(dir, "snap.sqlite")
	if _, err := db.Snapshot(ctx, SnapshotOptions{Output: snapPath}); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	db.Close()

	holder, err := sql.Open(sqlite.DriverName, filepath.Join(dir, ".bdd", "bdd.sqlite"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer holder.Close()
	if _, err := holder.ExecContext(ctx, "PRAGMA locking_mode = EXCLUSIVE"); err != nil {
		t.Fatalf("PRAGMA locking_mode: %v", err)
	}
	if _, err := holder.ExecContext(ctx, "SELECT 1 FROM sqlite_master LIMIT 1"); err != nil {
		t.Fatalf("acquiring lock: %v", err)
	}

	_, err = Restore(ctx, RestoreOptions{Path: filepath.Join(dir, ".bdd", "bdd.sqlite"), Source: snapPath})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("Restore() error = %v, want ErrBusy", err)
	}
}
