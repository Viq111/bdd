package bdd

import (
	"context"
	"time"
)

// Memory is a durable, workspace-scoped, named piece of knowledge that
// survives sessions and agent rotation. Unlike a card Note, a Memory is
// keyed and updatable rather than append-only and task-scoped.
type Memory struct {
	Key       string
	Body      string
	CreatedBy string
	UpdatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  int64
}

// Remember is the input to (*DB).Remember. If Key is empty, Remember
// derives a readable slug plus a short content hash and reports the
// generated key on the returned Memory.
type Remember struct {
	Key   string
	Body  string
	Actor string
}

// MemoryQuery is the input to (*DB).Memories. An empty Query lists every
// memory.
type MemoryQuery struct {
	Query string
}

// Remember atomically creates or updates a memory by key.
func (db *DB) Remember(ctx context.Context, in Remember) (*Memory, error) {
	return nil, errNotImplemented
}

// Memories returns memories matching q, searching case-insensitively across
// key and body.
func (db *DB) Memories(ctx context.Context, q MemoryQuery) ([]Memory, error) {
	return nil, errNotImplemented
}

// Recall returns the full memory record for key, or ErrNotFound.
func (db *DB) Recall(ctx context.Context, key string) (*Memory, error) {
	return nil, errNotImplemented
}

// Forget deletes the memory identified by key and records an audit event.
func (db *DB) Forget(ctx context.Context, key string, actor string) error {
	return errNotImplemented
}
