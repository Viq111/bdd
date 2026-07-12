package bdd

import (
	"context"
	"time"
)

// Rune holds a durable, human-keyed piece of standing instruction: a role,
// policy, prompt, or convention. A rune is not a card: it has no claim,
// priority, scheduling, blocking, readiness, worktree, or close. Card
// read/query methods (GetCard, ListCards, SearchCards, ReadyCards) never
// return runes; runes live in a separate namespace.
type Rune struct {
	Key      string // "<kind>/<name>", lowercase; first segment equals Kind
	Kind     string
	Title    string
	Body     string
	Metadata string

	Enabled   bool // defaults true on create; preserved on update unless supplied
	Protected bool // protected runes require Force to update, enable/disable, or remove

	CreatedBy string
	UpdatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  int64
}

// RuneSummary is the lightweight projection returned by ListRunes and
// SearchRunes.
type RuneSummary struct {
	Key       string
	Kind      string
	Title     string
	Enabled   bool
	Protected bool
	Revision  int64
}

// RuneMutation carries the fields PutRune creates or updates. Fields left
// nil on an update leave the stored value unchanged; on create, nil Enabled
// defaults to true and nil Protected defaults to false.
type RuneMutation struct {
	Title    *string
	Body     *string
	Metadata *string

	Enabled   *bool
	Protected *bool
}

// PutRune is the input to (*DB).PutRune. Key must follow the
// "<kind>/<name>" grammar, lowercase, with the first segment equal to
// Mutation's resolved Kind. CreateOnly rejects an existing key
// (ErrAlreadyExists). ExpectedRevision, when non-nil, gives optimistic
// concurrency: a stale revision fails without writing (ErrInvalidArgument).
// Force is required to update a Protected rune.
type PutRune struct {
	Key      string
	Kind     string
	Mutation RuneMutation

	CreateOnly       bool
	ExpectedRevision *int64

	Actor string
	Force bool
}

// RuneQuery configures ListRunes and SearchRunes. Text is only consulted by
// SearchRunes, matching against key, kind, title, body, and metadata. By
// default only enabled runes are returned; set All to include disabled
// ones.
type RuneQuery struct {
	Kind  string
	Text  string
	All   bool
	Limit int // 0 means unlimited
}

// PutRune atomically creates or updates the rune identified by in.Key.
func (db *DB) PutRune(ctx context.Context, in PutRune) (*Rune, error) {
	return nil, errNotImplemented
}

// GetRune returns the complete rune for key, or ErrNotFound.
func (db *DB) GetRune(ctx context.Context, key string) (*Rune, error) {
	return nil, errNotImplemented
}

// ListRunes returns rune summaries matching q.
func (db *DB) ListRunes(ctx context.Context, q RuneQuery) ([]RuneSummary, error) {
	return nil, errNotImplemented
}

// SearchRunes returns rune summaries matching q.Text (and q.Kind, if set).
func (db *DB) SearchRunes(ctx context.Context, q RuneQuery) ([]RuneSummary, error) {
	return nil, errNotImplemented
}

// SetRuneEnabled reversibly enables or disables the rune identified by key.
// Disabled runes remain directly readable via GetRune but appear in
// ListRunes/SearchRunes only when RuneQuery.All is set. Force is required
// when the rune is Protected.
func (db *DB) SetRuneEnabled(ctx context.Context, key string, enabled bool, actor string, force bool) (*Rune, error) {
	return nil, errNotImplemented
}

// RemoveRune permanently deletes the rune identified by key, leaving a
// tombstone audit event. Force is required when the rune is Protected.
func (db *DB) RemoveRune(ctx context.Context, key string, actor string, force bool) error {
	return errNotImplemented
}

// ExportRune renders a single rune as stable Markdown ("markdown") or
// ("json") output.
func (db *DB) ExportRune(ctx context.Context, key string, format string) ([]byte, error) {
	return nil, errNotImplemented
}

// ExportRunes renders one or more runes as stable Markdown ("markdown") or
// ("json") output.
func (db *DB) ExportRunes(ctx context.Context, keys []string, format string) ([]byte, error) {
	return nil, errNotImplemented
}
