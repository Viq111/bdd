package bdd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/viq111/bdd/internal/schema"
	"github.com/viq111/bdd/internal/sqlite"
)

// DefaultSnapshotName is the filename bdd uses for the default, git-tracked
// snapshot convention: <workspace>/.bdd/backup.sqlite (bdd plan section
// 21). Both Snapshot and Restore fall back to a file with this name,
// alongside the live database, when the caller does not supply one.
const DefaultSnapshotName = "backup.sqlite"

// SnapshotOptions configures Snapshot.
type SnapshotOptions struct {
	// Output is the destination path for the snapshot file. Empty defaults
	// to DefaultSnapshotName alongside the live database.
	Output string
}

// SnapshotResult reports the outcome of a successful Snapshot.
type SnapshotResult struct {
	Path          string
	SchemaVersion int
	CreatedAt     time.Time
}

// Snapshot produces one integrity-checked, standalone copy of db's current
// data using SQLite's VACUUM INTO, which is safe to call while other
// readers and writers hold the database open: VACUUM INTO reads a
// consistent, as-of-call snapshot without blocking them. The copy is
// written to a temporary file beside the destination, fsynced, and
// integrity-checked before it is atomically renamed into place, so a crash
// or failure at any point before the rename leaves the destination
// untouched.
func (db *DB) Snapshot(ctx context.Context, opts SnapshotOptions) (*SnapshotResult, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}

	dest := opts.Output
	if dest == "" {
		dest = filepath.Join(filepath.Dir(db.path), DefaultSnapshotName)
	}
	dest, err := filepath.Abs(dest)
	if err != nil {
		return nil, fmt.Errorf("bdd: snapshot: %w", err)
	}

	version, err := snapshotInto(ctx, db.sql, dest)
	if err != nil {
		return nil, fmt.Errorf("bdd: snapshot: %w", err)
	}

	return &SnapshotResult{Path: dest, SchemaVersion: version, CreatedAt: time.Now().UTC()}, nil
}

// RestoreOptions configures Restore.
type RestoreOptions struct {
	// Path is an explicit path to the target bdd database file, taking
	// precedence over Workspace discovery. If nothing exists at Path yet,
	// Restore creates it.
	Path string
	// Workspace is the directory Restore starts workspace discovery from
	// when Path is empty; an empty Workspace means the current working
	// directory. If discovery finds no existing database, Restore falls
	// back to creating <workspace>/.bdd/bdd.sqlite.
	Workspace string

	// Source is the snapshot file to install. Required.
	Source string

	// SkipBackup disables saving the current database before installing
	// Source. By default Restore backs up an existing target first.
	SkipBackup bool
	// BackupPath overrides where the pre-restore backup is written; empty
	// defaults to DefaultSnapshotName alongside the target database.
	BackupPath string
}

// RestoreResult reports the outcome of a successful Restore.
type RestoreResult struct {
	Path string
	// BackupPath is empty when SkipBackup was set or there was no existing
	// database to back up.
	BackupPath    string
	SchemaVersion int
}

// Restore validates Source's schema compatibility and integrity, then
// atomically installs it as the target workspace database. Restore
// requires exclusive access to an existing target: it fails with ErrBusy
// if another process holds the database open, rather than restoring out
// from under active readers or writers. Unless SkipBackup is set, Restore
// saves the current target to BackupPath before installing Source. Restore
// does not touch Source or the target until every validation has passed.
func Restore(ctx context.Context, opts RestoreOptions) (*RestoreResult, error) {
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		return nil, &ValidationError{Fields: []string{"source"}}
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("bdd: restore: %w", err)
	}
	if info, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("bdd: restore: no snapshot at %s: %w", source, ErrNotFound)
		}
		return nil, fmt.Errorf("bdd: restore: %w", err)
	} else if info.IsDir() {
		return nil, fmt.Errorf("bdd: restore: %s is a directory, not a database file: %w", source, ErrInvalidArgument)
	}

	// Validate schema compatibility and integrity before touching anything.
	version, err := verifySnapshotFile(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("bdd: restore: validating %s: %w", source, err)
	}
	if current := schema.CurrentVersion(); version > current {
		return nil, fmt.Errorf("bdd: restore: snapshot schema version %d is newer than this build supports (%d): %w", version, current, ErrSchemaTooNew)
	}

	target, err := resolveRestoreTarget(opts)
	if err != nil {
		return nil, fmt.Errorf("bdd: restore: %w", err)
	}
	targetDir := filepath.Dir(target)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("bdd: restore: %w", err)
	}

	targetExists := true
	if _, err := os.Stat(target); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("bdd: restore: %w", err)
		}
		targetExists = false
	}

	var backupPath string
	if targetExists {
		locked, err := acquireExclusive(ctx, target)
		if err != nil {
			return nil, fmt.Errorf("bdd: restore: acquiring exclusive access to %s: %w", target, err)
		}

		if !opts.SkipBackup {
			backupPath = opts.BackupPath
			if backupPath == "" {
				backupPath = filepath.Join(targetDir, DefaultSnapshotName)
			}
			backupPath, err = filepath.Abs(backupPath)
			if err != nil {
				locked.Close()
				return nil, fmt.Errorf("bdd: restore: %w", err)
			}
			if _, err := snapshotInto(ctx, locked, backupPath); err != nil {
				locked.Close()
				return nil, fmt.Errorf("bdd: restore: backing up %s: %w", target, err)
			}
		}

		locked.Close()
		// The old database's WAL/SHM sidecars belong to the file being
		// replaced; leaving them behind risks confusing the next opener.
		os.Remove(target + "-wal")
		os.Remove(target + "-shm")
	}

	tmpPath, err := reserveTempPath(targetDir, "bdd-restore-*.sqlite.tmp")
	if err != nil {
		return nil, fmt.Errorf("bdd: restore: %w", err)
	}
	defer os.Remove(tmpPath)

	if err := copyFile(source, tmpPath); err != nil {
		return nil, fmt.Errorf("bdd: restore: copying %s: %w", source, err)
	}
	if err := fsyncPath(tmpPath); err != nil {
		return nil, fmt.Errorf("bdd: restore: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return nil, fmt.Errorf("bdd: restore: installing %s: %w", target, err)
	}
	if err := fsyncPath(targetDir); err != nil {
		return nil, fmt.Errorf("bdd: restore: %w", err)
	}

	return &RestoreResult{Path: target, BackupPath: backupPath, SchemaVersion: version}, nil
}

// resolveRestoreTarget determines the database path Restore installs
// Source at: opts.Path if set, otherwise the result of workspace
// discovery, falling back to <workspace>/.bdd/bdd.sqlite when discovery
// finds nothing to restore over.
func resolveRestoreTarget(opts RestoreOptions) (string, error) {
	if opts.Path != "" {
		return filepath.Abs(opts.Path)
	}

	if found, err := discoverDatabase(opts.Workspace); err == nil {
		return found, nil
	} else if !errors.Is(err, ErrNotFound) {
		return "", err
	}

	workspaceDir := opts.Workspace
	if workspaceDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		workspaceDir = wd
	}
	workspaceDir, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(workspaceDir, bddDirName, bddFileName), nil
}

// acquireExclusive opens the SQLite database at path and takes SQLite's
// EXCLUSIVE locking mode, which upgrades to (and holds) an exclusive lock
// on the very first read. Any other connection to the same database,
// reading or writing, fails with SQLITE_BUSY once busy_timeout elapses,
// which this maps to ErrBusy. The caller must Close the returned handle to
// release the lock.
func acquireExclusive(ctx context.Context, path string) (*sql.DB, error) {
	var conn *sql.DB
	err := sqlite.Retry(ctx, func() error {
		c, err := sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot, SkipJournalMode: true})
		if err != nil {
			return err
		}
		if _, err := c.ExecContext(ctx, "PRAGMA locking_mode = EXCLUSIVE"); err != nil {
			c.Close()
			return err
		}
		if _, err := c.ExecContext(ctx, "SELECT 1 FROM sqlite_master LIMIT 1"); err != nil {
			c.Close()
			return err
		}
		conn = c
		return nil
	})
	if err != nil {
		if sqlite.IsBusy(err) {
			return nil, fmt.Errorf("database is in use: %w", ErrBusy)
		}
		return nil, err
	}
	return conn, nil
}

// snapshotInto runs VACUUM INTO over exec to produce a fresh copy of its
// database at dest, validates the copy's integrity, and atomically
// installs it: written to a temp file beside dest, fsynced, then renamed
// into place. exec must not be inside an explicit transaction; VACUUM does
// not run inside one. It returns the copy's schema version.
func snapshotInto(ctx context.Context, exec execer, dest string) (int, error) {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}

	tmpPath, err := reserveTempPath(dir, "bdd-snapshot-*.sqlite.tmp")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmpPath)

	if err := sqlite.Retry(ctx, func() error {
		_, err := exec.ExecContext(ctx, "VACUUM INTO ?", tmpPath)
		return err
	}); err != nil {
		return 0, fmt.Errorf("vacuum into %s: %w", tmpPath, err)
	}

	version, err := verifySnapshotFile(ctx, tmpPath)
	if err != nil {
		return 0, err
	}

	if err := fsyncPath(tmpPath); err != nil {
		return 0, err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return 0, fmt.Errorf("installing %s: %w", dest, err)
	}
	if err := fsyncPath(dir); err != nil {
		return 0, err
	}

	return version, nil
}

// verifySnapshotFile opens path read-only, runs PRAGMA integrity_check,
// and returns its schema version. It leaves path untouched.
func verifySnapshotFile(ctx context.Context, path string) (int, error) {
	conn, err := sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot, ReadOnly: true, SkipJournalMode: true})
	if err != nil {
		return 0, fmt.Errorf("opening %s: %w", path, err)
	}
	defer conn.Close()

	var result string
	if err := conn.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return 0, fmt.Errorf("integrity check %s: %w", path, err)
	}
	if result != "ok" {
		return 0, fmt.Errorf("integrity check %s failed: %s", path, result)
	}

	version, err := schema.ReadVersion(ctx, conn)
	if err != nil {
		return 0, fmt.Errorf("reading schema version of %s: %w", path, err)
	}
	return version, nil
}

// reserveTempPath returns a unique path matching pattern (per
// os.CreateTemp) inside dir, without leaving anything on disk at that
// path. VACUUM INTO and similar create-only writers require their
// destination not to exist yet.
func reserveTempPath(dir, pattern string) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

// fsyncPath opens path, which may be a regular file or a directory, and
// syncs it, forcing its current contents (or, for a directory, its current
// entries) to stable storage.
func fsyncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// copyFile copies src to dst, which must not already exist.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
