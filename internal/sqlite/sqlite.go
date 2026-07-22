package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	modernc "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// DriverName is the database/sql driver name registered by
// modernc.org/sqlite.
const DriverName = "sqlite"

// Pool selects the connection pooling policy for an opened database.
type Pool int

const (
	// PoolOneShot configures a single connection, appropriate for
	// short-lived CLI processes that make one or a few calls and exit.
	PoolOneShot Pool = iota
	// PoolConservative configures a small multi-connection pool,
	// appropriate for long-lived library callers (daemons, servers).
	PoolConservative
)

// Options configures Open.
type Options struct {
	Pool Pool
	// ReadOnly opens the database file in read-only mode.
	ReadOnly bool
	// SkipJournalMode leaves the database's on-disk journal mode
	// untouched. journal_mode is persisted in the database file itself,
	// so issuing "PRAGMA journal_mode = WAL" can rewrite the file even
	// when the requested mode already matches. Callers that must not
	// mutate the database on a normal open (bdd.Open) set this; callers
	// that are creating or upgrading a database set it false so the
	// required WAL mode gets established.
	SkipJournalMode bool
}

// conservativeMaxOpenConns bounds the pool for long-lived callers. SQLite
// under WAL supports one writer at a time, so a large pool buys nothing and
// only increases lock contention.
const conservativeMaxOpenConns = 4

// Open opens the SQLite database at path, applying the required PRAGMAs
// (foreign_keys, synchronous NORMAL, busy_timeout, and WAL journal mode
// unless Options.SkipJournalMode is set) on every connection. It does not
// create the file unless the driver's DSN options say so; callers that need
// create-if-missing semantics rely on SQLite's default behavior of creating
// a new file for a path that does not exist yet.
func Open(ctx context.Context, path string, opts Options) (*sql.DB, error) {
	if err := ValidatePath(path); err != nil {
		return nil, err
	}

	dsn := path
	if opts.ReadOnly {
		dsn = fmt.Sprintf("%s?mode=ro", path)
	}

	db, err := sql.Open(DriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}

	switch opts.Pool {
	case PoolConservative:
		db.SetMaxOpenConns(conservativeMaxOpenConns)
		db.SetMaxIdleConns(conservativeMaxOpenConns)
	default:
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}

	if err := applyPragmas(ctx, db, opts.SkipJournalMode); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func applyPragmas(ctx context.Context, db *sql.DB, skipJournalMode bool) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	}
	if !skipJournalMode {
		pragmas = append(pragmas, "PRAGMA journal_mode = WAL")
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("sqlite: %s: %w", p, err)
		}
	}
	return nil
}

// Retry runs fn, retrying with bounded exponential backoff and jitter while
// fn returns a SQLITE_BUSY or SQLITE_LOCKED error. It gives up and returns
// the last error after a fixed number of attempts, or immediately if ctx is
// done.
func Retry(ctx context.Context, fn func() error) error {
	const (
		maxAttempts = 5
		baseDelay   = 10 * time.Millisecond
		maxDelay    = 200 * time.Millisecond
	)

	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = fn()
		if err == nil || !IsBusy(err) {
			return err
		}
		if attempt == maxAttempts-1 {
			break
		}

		delay := baseDelay * time.Duration(1<<uint(attempt))
		if delay > maxDelay {
			delay = maxDelay
		}
		delay = delay/2 + time.Duration(rand.Int63n(int64(delay/2+1)))

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

// IsBusy reports whether err is a SQLITE_BUSY or SQLITE_LOCKED error.
func IsBusy(err error) bool {
	var sqliteErr *modernc.Error
	if errors.As(err, &sqliteErr) {
		code := sqliteErr.Code()
		return code == sqlite3.SQLITE_BUSY || code == sqlite3.SQLITE_LOCKED
	}
	return false
}

// ValidatePath rejects a database path that could be misinterpreted as a
// SQLite DSN carrying embedded connection parameters. modernc.org/sqlite
// splits a plain (non-"file:"-prefixed) DSN at its first unescaped '?' and
// parses the remainder as query parameters — including "_pragma", which
// runs an arbitrary "PRAGMA <value>" statement on open, and "vfs", which
// selects an alternate VFS. Every caller in this package builds its DSN
// directly from a caller- or flag-supplied filesystem path (--workspace
// discovery, snapshot/restore paths), so a path containing '?'
// would let whoever controls that path smuggle arbitrary PRAGMA execution
// or VFS selection into the open call. Legitimate SQLite filenames have no
// need for '?', so Open rejects it outright rather than attempting lossy
// percent-encoding.
func ValidatePath(path string) error {
	if strings.ContainsRune(path, '?') {
		return fmt.Errorf("sqlite: database path %q must not contain '?'", path)
	}
	return nil
}

// IsUniqueViolation reports whether err is a SQLITE_CONSTRAINT_UNIQUE or
// SQLITE_CONSTRAINT_PRIMARYKEY error, used to retry ID generation on
// collision.
func IsUniqueViolation(err error) bool {
	var sqliteErr *modernc.Error
	if errors.As(err, &sqliteErr) {
		code := sqliteErr.Code()
		return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}
