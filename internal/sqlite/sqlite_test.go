package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestOpenAppliesPragmas(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bdd.sqlite")

	db, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	checks := map[string]string{
		"PRAGMA foreign_keys": "1",
		"PRAGMA journal_mode": "wal",
		"PRAGMA synchronous":  "1", // NORMAL
		"PRAGMA busy_timeout": "5000",
	}
	for pragma, want := range checks {
		var got string
		if err := db.QueryRowContext(ctx, pragma).Scan(&got); err != nil {
			t.Fatalf("%s: %v", pragma, err)
		}
		if got != want {
			t.Fatalf("%s = %q, want %q", pragma, got, want)
		}
	}
}

func TestOpenSkipJournalModeLeavesExistingModeUntouched(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bdd.sqlite")

	setup, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatalf("Open(setup) error = %v", err)
	}
	if _, err := setup.ExecContext(ctx, "PRAGMA journal_mode = DELETE"); err != nil {
		setup.Close()
		t.Fatalf("PRAGMA journal_mode = DELETE: %v", err)
	}
	if err := setup.Close(); err != nil {
		t.Fatalf("close setup: %v", err)
	}

	db, err := Open(ctx, path, Options{SkipJournalMode: true})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "delete" {
		t.Fatalf("journal_mode = %q, want %q (SkipJournalMode must not rewrite it)", mode, "delete")
	}
}

func TestOpenOneShotPoolIsSingleConnection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bdd.sqlite")

	db, err := Open(ctx, path, Options{Pool: PoolOneShot})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}
}

func TestOpenConservativePoolAllowsMultipleConnections(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bdd.sqlite")

	db, err := Open(ctx, path, Options{Pool: PoolConservative})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	if got := db.Stats().MaxOpenConnections; got <= 1 {
		t.Fatalf("MaxOpenConnections = %d, want > 1", got)
	}
}

func TestIsBusyFalseForPlainError(t *testing.T) {
	if IsBusy(errors.New("plain")) {
		t.Fatal("IsBusy(plain error) = true, want false")
	}
}

// captureBusyError provokes a genuine SQLITE_BUSY by holding an open write
// transaction on one connection while writing from a second, independently
// opened connection with its busy_timeout disabled.
func captureBusyError(t *testing.T) error {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bdd.sqlite")

	a, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatalf("Open(a) error = %v", err)
	}
	t.Cleanup(func() { a.Close() })
	if _, err := a.ExecContext(ctx, "CREATE TABLE t (v INTEGER)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	b, err := sql.Open(DriverName, path)
	if err != nil {
		t.Fatalf("sql.Open(b) error = %v", err)
	}
	t.Cleanup(func() { b.Close() })
	b.SetMaxOpenConns(1)
	if _, err := b.ExecContext(ctx, "PRAGMA busy_timeout = 0"); err != nil {
		t.Fatalf("set busy_timeout: %v", err)
	}

	txA, err := a.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx a: %v", err)
	}
	t.Cleanup(func() { txA.Rollback() })
	if _, err := txA.ExecContext(ctx, "INSERT INTO t (v) VALUES (1)"); err != nil {
		t.Fatalf("insert in tx a: %v", err)
	}

	_, err = b.ExecContext(ctx, "INSERT INTO t (v) VALUES (2)")
	if err == nil {
		t.Skip("did not observe SQLITE_BUSY under this platform's locking behavior")
	}
	return err
}

func TestIsBusyTrueForRealBusyConflict(t *testing.T) {
	if !IsBusy(captureBusyError(t)) {
		t.Fatal("IsBusy(real busy error) = false, want true")
	}
}

func TestRetrySucceedsAfterTransientBusy(t *testing.T) {
	busyErr := captureBusyError(t)

	var attempts int32
	err := Retry(context.Background(), func() error {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return busyErr
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetryGivesUpOnNonBusyError(t *testing.T) {
	wantErr := errors.New("boom")
	var attempts int32
	err := Retry(context.Background(), func() error {
		atomic.AddInt32(&attempts, 1)
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Retry() error = %v, want %v", err, wantErr)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (should not retry non-busy errors)", attempts)
	}
}

func TestRetryStopsAfterBoundedAttempts(t *testing.T) {
	busyErr := captureBusyError(t)

	var attempts int32
	err := Retry(context.Background(), func() error {
		atomic.AddInt32(&attempts, 1)
		return busyErr
	})
	if err == nil {
		t.Fatal("Retry() error = nil, want a busy error after exhausting attempts")
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d, want at least 2", attempts)
	}
	if attempts > 10 {
		t.Fatalf("attempts = %d, retry budget should be bounded", attempts)
	}
}
