// Package bdd is a typed Go library over a single SQLite file that tracks
// small units of work ("cards") for agent-oriented workflows, plus durable
// workspace memories and named rune records (roles, policies, prompts).
//
// The companion cmd/bdd binary is a thin, fast, non-interactive CLI over
// this library.
package bdd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/viq111/bdd/internal/schema"
	"github.com/viq111/bdd/internal/sqlite"
)

// bddDirName and bddFileName are the fixed workspace-relative location of a
// bdd database: <workspace>/.bdd/bdd.sqlite. The fixed name means Open never
// has to parse a bootstrap config file to find the database.
const (
	bddDirName  = ".bdd"
	bddFileName = "bdd.sqlite"
)

// OpenOptions configures Open.
type OpenOptions struct {
	// Path is an explicit path to a bdd SQLite database file. When set, it
	// takes precedence over workspace discovery.
	Path string

	// Workspace is the directory Open starts workspace discovery from when
	// Path is empty. Discovery walks upward looking for a .bdd/bdd.sqlite
	// file, stopping at the filesystem root. An empty Workspace means the
	// current working directory.
	Workspace string

	// ReadOnly opens the database without acquiring write locks. Mutation
	// methods on a read-only DB return ErrInvalidArgument.
	ReadOnly bool
}

// DB is a handle to an open bdd workspace database. A DB is safe for
// concurrent use by multiple goroutines.
type DB struct {
	opts OpenOptions
	path string

	sql *sql.DB

	mu           sync.Mutex
	closed       bool
	schemaTooOld bool
}

// Open resolves a bdd workspace database per opts and opens it. A normal
// open only inspects the schema version; it never runs DDL, integrity
// checks, WAL checkpoints, or config rewrites. If the on-disk schema is
// older than this build expects, Open still succeeds but every method
// returns ErrSchemaTooOld until Upgrade is called; if the on-disk schema is
// newer, Open returns ErrSchemaTooNew.
func Open(ctx context.Context, opts OpenOptions) (*DB, error) {
	path := opts.Path
	if path != "" {
		if info, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("bdd: no database at %s: %w", path, ErrNotFound)
			}
			return nil, fmt.Errorf("bdd: %w", err)
		} else if info.IsDir() {
			return nil, fmt.Errorf("bdd: %s is a directory, not a database file: %w", path, ErrInvalidArgument)
		}
	} else {
		discovered, err := discoverDatabase(opts.Workspace)
		if err != nil {
			return nil, err
		}
		path = discovered
	}

	sqlDB, err := sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot, ReadOnly: opts.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("bdd: open %s: %w", path, err)
	}

	version, err := schema.ReadVersion(ctx, sqlDB)
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("bdd: %w", err)
	}

	current := schema.CurrentVersion()
	if version > current {
		sqlDB.Close()
		return nil, fmt.Errorf("bdd: database schema version %d is newer than this build supports (%d): %w", version, current, ErrSchemaTooNew)
	}

	return &DB{
		opts:         opts,
		path:         path,
		sql:          sqlDB,
		schemaTooOld: version < current,
	}, nil
}

// Close releases the underlying database connection(s). Close is safe to
// call more than once.
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.closed {
		return nil
	}
	db.closed = true
	return db.sql.Close()
}

// Upgrade applies every pending schema migration to bring the database to
// the schema version this build expects. Upgrade is a no-op if the schema
// is already current.
func (db *DB) Upgrade(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.closed {
		return fmt.Errorf("bdd: upgrade: database is closed: %w", ErrInvalidArgument)
	}
	if err := schema.Upgrade(ctx, db.sql); err != nil {
		return fmt.Errorf("bdd: %w", err)
	}
	db.schemaTooOld = false
	return nil
}

// InitOptions configures Init.
type InitOptions struct {
	// Workspace is the directory Init creates .bdd/bdd.sqlite under. An
	// empty Workspace means the current working directory.
	Workspace string

	// Prefix is the workspace's card ID prefix (<prefix>-<random-suffix>),
	// stored in the workspace table. Required: lowercase, starting with a
	// letter, and containing only letters, digits, and hyphens.
	Prefix string
}

var prefixPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Init creates a new bdd workspace database at <workspace>/.bdd/bdd.sqlite,
// applying every schema migration and seeding the built-in statuses and
// types, then records Prefix in the workspace table. Init fails rather than
// replacing anything already at that path, compatible or not: if a file
// already exists at <workspace>/.bdd/bdd.sqlite, Init returns
// ErrAlreadyExists without modifying it.
func Init(ctx context.Context, opts InitOptions) (*DB, error) {
	if !prefixPattern.MatchString(opts.Prefix) {
		return nil, &ValidationError{Fields: []string{"prefix"}}
	}

	workspaceDir := opts.Workspace
	if workspaceDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("bdd: init: %w", err)
		}
		workspaceDir = wd
	}

	dbDir := filepath.Join(workspaceDir, bddDirName)
	dbPath := filepath.Join(dbDir, bddFileName)

	if _, err := os.Stat(dbPath); err == nil {
		return nil, fmt.Errorf("bdd: init: %s already exists: %w", dbPath, ErrAlreadyExists)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("bdd: init: %w", err)
	}

	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return nil, fmt.Errorf("bdd: init: %w", err)
	}

	sqlDB, err := sqlite.Open(ctx, dbPath, sqlite.Options{Pool: sqlite.PoolOneShot})
	if err != nil {
		return nil, fmt.Errorf("bdd: init: %w", err)
	}

	if err := schema.Upgrade(ctx, sqlDB); err != nil {
		sqlDB.Close()
		os.Remove(dbPath)
		return nil, fmt.Errorf("bdd: init: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	err = sqlite.Retry(ctx, func() error {
		_, err := sqlDB.ExecContext(ctx, "INSERT INTO workspace (singleton, prefix, created_at) VALUES (1, ?, ?)", opts.Prefix, now)
		return err
	})
	if err != nil {
		sqlDB.Close()
		os.Remove(dbPath)
		return nil, fmt.Errorf("bdd: init: writing workspace row: %w", err)
	}

	return &DB{
		opts: OpenOptions{Path: dbPath},
		path: dbPath,
		sql:  sqlDB,
	}, nil
}

// discoverDatabase walks upward from start (or the current working
// directory when start is empty) looking for a <dir>/.bdd/bdd.sqlite file,
// stopping at the filesystem root.
func discoverDatabase(start string) (string, error) {
	dir := start
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("bdd: %w", err)
		}
		dir = wd
	}

	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("bdd: %w", err)
	}

	for {
		candidate := filepath.Join(dir, bddDirName, bddFileName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("bdd: no %s/%s found walking up from %s: %w", bddDirName, bddFileName, start, ErrNotFound)
}
