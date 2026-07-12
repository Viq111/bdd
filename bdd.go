// Package bdd is a typed Go library over a single SQLite file that tracks
// small units of work ("cards") for agent-oriented workflows, plus durable
// workspace memories and named rune records (roles, policies, prompts).
//
// The companion cmd/bdd binary is a thin, fast, non-interactive CLI over
// this library.
package bdd

import "context"

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
}

// Open resolves a bdd workspace database per opts and opens it. A normal
// open only inspects the schema version; it never runs DDL, integrity
// checks, WAL checkpoints, or config rewrites. If the on-disk schema is
// older than this build expects, Open still succeeds but every method
// returns ErrSchemaTooOld until Upgrade is called; if the on-disk schema is
// newer, Open returns ErrSchemaTooNew.
func Open(ctx context.Context, opts OpenOptions) (*DB, error) {
	return nil, errNotImplemented
}

// Close releases the underlying database connection(s). Close is safe to
// call more than once.
func (db *DB) Close() error {
	return errNotImplemented
}

// Upgrade applies every pending schema migration to bring the database to
// the schema version this build expects. Upgrade is a no-op if the schema
// is already current.
func (db *DB) Upgrade(ctx context.Context) error {
	return errNotImplemented
}
