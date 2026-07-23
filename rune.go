package bdd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/viq111/bdd/internal/sqlite"
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

	// Prime controls whether `bdd prime` treats this rune as standing
	// instruction: RunePrimeRequired inlines the full body, RunePrimeOptional
	// (the default) includes only a key/title/kind/revision summary, and
	// RunePrimeNever omits it from prime entirely.
	Prime string

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
	Prime     string
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
	Prime    *string // one of RunePrimeRequired, RunePrimeOptional, RunePrimeNever

	Enabled   *bool
	Protected *bool
}

// Rune prime designations: see Rune.Prime.
const (
	RunePrimeRequired = "required"
	RunePrimeOptional = "optional"
	RunePrimeNever    = "never"
)

func validRunePrime(v string) bool {
	switch v {
	case RunePrimeRequired, RunePrimeOptional, RunePrimeNever:
		return true
	default:
		return false
	}
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

// requireReady returns ErrInvalidArgument if db is closed and
// ErrSchemaTooOld if its schema predates this build, per the Open/Upgrade
// contract in bdd.go.
func (db *DB) requireReady() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.closed {
		return fmt.Errorf("bdd: database is closed: %w", ErrInvalidArgument)
	}
	if db.schemaTooOld {
		return ErrSchemaTooOld
	}
	return nil
}

// runeKeySegment matches one lowercase, letter-led segment of a rune key.
var runeKeySegment = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// parseRuneKey validates key against the "<kind>/<name>" grammar and
// returns its two segments.
func parseRuneKey(key string) (kind, name string, err error) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) != 2 || !runeKeySegment.MatchString(parts[0]) || !runeKeySegment.MatchString(parts[1]) {
		return "", "", &ValidationError{Fields: []string{"key"}}
	}
	return parts[0], parts[1], nil
}

// PutRune atomically creates or updates the rune identified by in.Key.
func (db *DB) PutRune(ctx context.Context, in PutRune) (*Rune, error) {
	if err := db.requireReady(); err != nil {
		return nil, err
	}
	if db.opts.ReadOnly {
		return nil, fmt.Errorf("bdd: set rune: database is read-only: %w", ErrInvalidArgument)
	}

	kind, _, err := parseRuneKey(in.Key)
	if err != nil {
		return nil, err
	}
	if in.Kind == "" || in.Kind != kind {
		return nil, &ValidationError{Fields: []string{"kind"}}
	}
	if in.Mutation.Metadata != nil && !json.Valid([]byte(*in.Mutation.Metadata)) {
		return nil, &ValidationError{Fields: []string{"metadata"}}
	}
	if in.Mutation.Prime != nil && !validRunePrime(*in.Mutation.Prime) {
		return nil, &ValidationError{Fields: []string{"prime"}}
	}

	var result *Rune
	err = sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		existing, err := getRuneTx(ctx, tx, in.Key)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}

		now := time.Now().UTC()
		if existing == nil {
			r, err := createRuneTx(ctx, tx, in, now)
			if err != nil {
				return err
			}
			result = r
		} else {
			if in.CreateOnly {
				return ErrAlreadyExists
			}
			if in.ExpectedRevision != nil && *in.ExpectedRevision != existing.Revision {
				return fmt.Errorf("bdd: set rune: expected revision %d, found %d: %w", *in.ExpectedRevision, existing.Revision, ErrInvalidArgument)
			}
			if existing.Protected && !in.Force {
				return fmt.Errorf("bdd: set rune: %s is protected, Force required: %w", in.Key, ErrInvalidArgument)
			}
			r, err := updateRuneTx(ctx, tx, existing, in, now)
			if err != nil {
				return err
			}
			result = r
		}

		return tx.Commit()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func createRuneTx(ctx context.Context, tx *sql.Tx, in PutRune, now time.Time) (*Rune, error) {
	title := ""
	if in.Mutation.Title != nil {
		title = *in.Mutation.Title
	}
	body := ""
	if in.Mutation.Body != nil {
		body = *in.Mutation.Body
	}
	metadata := "{}"
	if in.Mutation.Metadata != nil {
		metadata = *in.Mutation.Metadata
	}
	prime := RunePrimeOptional
	if in.Mutation.Prime != nil {
		prime = *in.Mutation.Prime
	}
	enabled := true
	if in.Mutation.Enabled != nil {
		enabled = *in.Mutation.Enabled
	}
	protected := false
	if in.Mutation.Protected != nil {
		protected = *in.Mutation.Protected
	}
	if in.ExpectedRevision != nil {
		return nil, fmt.Errorf("bdd: set rune: %s does not exist, cannot check expected revision: %w", in.Key, ErrInvalidArgument)
	}

	nowStr := now.Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO runes (key, kind, title, body, metadata_json, prime, enabled, protected, created_by, updated_by, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		in.Key, in.Kind, title, body, metadata, prime, boolToInt(enabled), boolToInt(protected), in.Actor, in.Actor, nowStr, nowStr)
	if err != nil {
		return nil, err
	}

	if err := insertRuneEvent(ctx, tx, in.Key, 1, "rune.create", in.Actor, map[string]any{"kind": in.Kind}, now); err != nil {
		return nil, err
	}

	return &Rune{
		Key: in.Key, Kind: in.Kind, Title: title, Body: body, Metadata: metadata, Prime: prime,
		Enabled: enabled, Protected: protected,
		CreatedBy: in.Actor, UpdatedBy: in.Actor, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}, nil
}

func updateRuneTx(ctx context.Context, tx *sql.Tx, existing *Rune, in PutRune, now time.Time) (*Rune, error) {
	title := existing.Title
	if in.Mutation.Title != nil {
		title = *in.Mutation.Title
	}
	body := existing.Body
	if in.Mutation.Body != nil {
		body = *in.Mutation.Body
	}
	metadata := existing.Metadata
	if in.Mutation.Metadata != nil {
		metadata = *in.Mutation.Metadata
	}
	prime := existing.Prime
	if in.Mutation.Prime != nil {
		prime = *in.Mutation.Prime
	}
	enabled := existing.Enabled
	if in.Mutation.Enabled != nil {
		enabled = *in.Mutation.Enabled
	}
	protected := existing.Protected
	if in.Mutation.Protected != nil {
		protected = *in.Mutation.Protected
	}

	revision := existing.Revision + 1
	nowStr := now.Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `
		UPDATE runes SET title = ?, body = ?, metadata_json = ?, prime = ?, enabled = ?, protected = ?, updated_by = ?, updated_at = ?, revision = ?
		WHERE key = ?`,
		title, body, metadata, prime, boolToInt(enabled), boolToInt(protected), in.Actor, nowStr, revision, in.Key)
	if err != nil {
		return nil, err
	}

	if err := insertRuneEvent(ctx, tx, in.Key, revision, "rune.update", in.Actor, map[string]any{"kind": existing.Kind}, now); err != nil {
		return nil, err
	}

	return &Rune{
		Key: in.Key, Kind: existing.Kind, Title: title, Body: body, Metadata: metadata, Prime: prime,
		Enabled: enabled, Protected: protected,
		CreatedBy: existing.CreatedBy, UpdatedBy: in.Actor,
		CreatedAt: existing.CreatedAt, UpdatedAt: now, Revision: revision,
	}, nil
}

func insertRuneEvent(ctx context.Context, tx *sql.Tx, key string, revision int64, action, actor string, payload map[string]any, now time.Time) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (subject_kind, subject_key, revision, action, actor, payload_json, created_at)
		VALUES ('rune', ?, ?, ?, ?, ?, ?)`,
		key, revision, action, actor, string(b), now.Format(time.RFC3339Nano))
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GetRune returns the complete rune for key, or ErrNotFound.
func (db *DB) GetRune(ctx context.Context, key string) (*Rune, error) {
	if err := db.requireReady(); err != nil {
		return nil, err
	}
	return getRuneTx(ctx, db.sql, key)
}

// runeQuerier is satisfied by both *sql.DB and *sql.Tx.
type runeQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getRuneTx(ctx context.Context, q runeQuerier, key string) (*Rune, error) {
	row := q.QueryRowContext(ctx, `
		SELECT key, kind, title, body, metadata_json, prime, enabled, protected, created_by, updated_by, created_at, updated_at, revision
		FROM runes WHERE key = ?`, key)
	return scanRune(row)
}

func scanRune(row *sql.Row) (*Rune, error) {
	var (
		r                    Rune
		enabled, protected   int
		createdBy, updatedBy sql.NullString
		createdAt, updatedAt string
	)
	err := row.Scan(&r.Key, &r.Kind, &r.Title, &r.Body, &r.Metadata, &r.Prime, &enabled, &protected, &createdBy, &updatedBy, &createdAt, &updatedAt, &r.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	r.Enabled = enabled != 0
	r.Protected = protected != 0
	r.CreatedBy = createdBy.String
	r.UpdatedBy = updatedBy.String

	r.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("bdd: parsing rune created_at: %w", err)
	}
	r.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("bdd: parsing rune updated_at: %w", err)
	}
	return &r, nil
}

// ListRunes returns rune summaries matching q.
func (db *DB) ListRunes(ctx context.Context, q RuneQuery) ([]RuneSummary, error) {
	if err := db.requireReady(); err != nil {
		return nil, err
	}
	return listOrSearchRunes(ctx, db.sql, q, false)
}

// SearchRunes returns rune summaries matching q.Text (and q.Kind, if set).
func (db *DB) SearchRunes(ctx context.Context, q RuneQuery) ([]RuneSummary, error) {
	if err := db.requireReady(); err != nil {
		return nil, err
	}
	return listOrSearchRunes(ctx, db.sql, q, true)
}

func listOrSearchRunes(ctx context.Context, sqlDB *sql.DB, q RuneQuery, search bool) ([]RuneSummary, error) {
	var (
		conds []string
		args  []any
	)

	if !q.All {
		conds = append(conds, "enabled = 1")
	}
	if q.Kind != "" {
		conds = append(conds, "kind = ?")
		args = append(args, q.Kind)
	}
	if search && q.Text != "" {
		like := "%" + strings.ToLower(q.Text) + "%"
		conds = append(conds, "(lower(key) LIKE ? OR lower(kind) LIKE ? OR lower(title) LIKE ? OR lower(body) LIKE ? OR lower(metadata_json) LIKE ?)")
		args = append(args, like, like, like, like, like)
	}

	query := "SELECT key, kind, title, prime, enabled, protected, revision FROM runes"
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY key ASC"
	if q.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", q.Limit)
	}

	rows, err := sqlDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RuneSummary
	for rows.Next() {
		var (
			s                  RuneSummary
			enabled, protected int
		)
		if err := rows.Scan(&s.Key, &s.Kind, &s.Title, &s.Prime, &enabled, &protected, &s.Revision); err != nil {
			return nil, err
		}
		s.Enabled = enabled != 0
		s.Protected = protected != 0
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SetRuneEnabled reversibly enables or disables the rune identified by key.
// Disabled runes remain directly readable via GetRune but appear in
// ListRunes/SearchRunes only when RuneQuery.All is set. Force is required
// when the rune is Protected.
func (db *DB) SetRuneEnabled(ctx context.Context, key string, enabled bool, actor string, force bool) (*Rune, error) {
	if err := db.requireReady(); err != nil {
		return nil, err
	}
	if db.opts.ReadOnly {
		return nil, fmt.Errorf("bdd: set rune enabled: database is read-only: %w", ErrInvalidArgument)
	}

	var result *Rune
	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		existing, err := getRuneTx(ctx, tx, key)
		if err != nil {
			return err
		}
		if existing.Protected && !force {
			return fmt.Errorf("bdd: set rune enabled: %s is protected, Force required: %w", key, ErrInvalidArgument)
		}

		now := time.Now().UTC()
		revision := existing.Revision + 1
		_, err = tx.ExecContext(ctx, `
			UPDATE runes SET enabled = ?, updated_by = ?, updated_at = ?, revision = ?
			WHERE key = ?`,
			boolToInt(enabled), actor, now.Format(time.RFC3339Nano), revision, key)
		if err != nil {
			return err
		}

		action := "rune.disable"
		if enabled {
			action = "rune.enable"
		}
		if err := insertRuneEvent(ctx, tx, key, revision, action, actor, map[string]any{"kind": existing.Kind}, now); err != nil {
			return err
		}

		existing.Enabled = enabled
		existing.UpdatedBy = actor
		existing.UpdatedAt = now
		existing.Revision = revision
		result = existing

		return tx.Commit()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// RemoveRune permanently deletes the rune identified by key, leaving a
// tombstone audit event. Force is required when the rune is Protected.
func (db *DB) RemoveRune(ctx context.Context, key string, actor string, force bool) error {
	if err := db.requireReady(); err != nil {
		return err
	}
	if db.opts.ReadOnly {
		return fmt.Errorf("bdd: remove rune: database is read-only: %w", ErrInvalidArgument)
	}

	return sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		existing, err := getRuneTx(ctx, tx, key)
		if err != nil {
			return err
		}
		if existing.Protected && !force {
			return fmt.Errorf("bdd: remove rune: %s is protected, Force required: %w", key, ErrInvalidArgument)
		}

		if _, err := tx.ExecContext(ctx, "DELETE FROM runes WHERE key = ?", key); err != nil {
			return err
		}

		now := time.Now().UTC()
		if err := insertRuneEvent(ctx, tx, key, existing.Revision+1, "rune.remove", actor, map[string]any{"kind": existing.Kind}, now); err != nil {
			return err
		}

		return tx.Commit()
	})
}
