package bdd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// MaxLabelBytes is the maximum length, in UTF-8 bytes, of a single label.
// Labels require no registration: any non-empty valid UTF-8 string up to
// this length is accepted, and add/remove is idempotent (plan section 10).
const MaxLabelBytes = 128

// StatusCategory groups statuses into the four buckets the readiness
// predicate and lifecycle transitions reason about. Custom statuses
// (workspace config) must declare one of these categories.
type StatusCategory string

const (
	// StatusCategoryActive marks a card as not yet started but eligible for
	// dispatch.
	StatusCategoryActive StatusCategory = "active"
	// StatusCategoryWIP marks a card as claimed and in progress.
	StatusCategoryWIP StatusCategory = "wip"
	// StatusCategoryDone marks a card as finished, in any of its terminal
	// forms.
	StatusCategoryDone StatusCategory = "done"
	// StatusCategoryFrozen marks a card as intentionally not dispatchable
	// without being finished (deferred, blocked, etc).
	StatusCategoryFrozen StatusCategory = "frozen"
)

// Status is a card's lifecycle state. Built-in statuses are seeded into
// every workspace; a workspace may additionally define custom statuses via
// the status.custom config key, each declaring one of the StatusCategory
// values above.
type Status string

// Built-in statuses seeded into every workspace, each with a fixed
// StatusCategory (see BuiltinStatusCategories).
const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusBlocked    Status = "blocked"
	StatusDeferred   Status = "deferred"
	StatusClosed     Status = "closed"
	StatusWontFix    Status = "wontfix"
)

// BuiltinStatusCategories maps every built-in Status to its
// StatusCategory. Custom statuses are not represented here; resolve their
// category via the config/status-definitions API instead.
var BuiltinStatusCategories = map[Status]StatusCategory{
	StatusOpen:       StatusCategoryActive,
	StatusInProgress: StatusCategoryWIP,
	StatusBlocked:    StatusCategoryFrozen,
	StatusDeferred:   StatusCategoryFrozen,
	StatusClosed:     StatusCategoryDone,
	StatusWontFix:    StatusCategoryDone,
}

// CardType is the kind of work a card represents. It determines which
// fields are required at creation time (see the required-field matrix in
// CreateCard's documentation).
type CardType string

// Built-in card types seeded into every workspace. A workspace may
// additionally define custom types via the types.custom config key; custom
// types carry no extra required-field rules in v1.
const (
	CardTypeBug      CardType = "bug"
	CardTypeTask     CardType = "task"
	CardTypeFeature  CardType = "feature"
	CardTypeEpic     CardType = "epic"
	CardTypeDecision CardType = "decision"
	CardTypeChore    CardType = "chore"
)

// Card is the full-fat representation of a card, as returned by GetCard and
// by mutation methods. ListCards, SearchCards, and ReadyCards return a
// lighter projection (core fields plus labels, no note/edge expansion) but
// use the same struct.
type Card struct {
	ID           string
	Title        string
	Type         CardType
	Status       Status
	Priority     int32 // 0..2147483647, default 2, ascending = higher priority
	Description  string
	Reproduction string
	Design       string
	Acceptance   string
	ExternalRef  string
	Worktree     string
	Assignee     string
	Dispatchable bool
	Labels       []string

	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time

	StartedAt  *time.Time
	ClosedAt   *time.Time
	DeferUntil *time.Time

	Revision int64
}

// CardRef is a lightweight reference to a card, returned by Parents and
// Children.
type CardRef struct {
	ID     string
	Title  string
	Type   CardType
	Status Status
}

// Note is one append-only entry in a card's note log. Notes have no mutable
// body; corrections are made by appending a new note.
type Note struct {
	ID        int64
	CardID    string
	Body      string
	Author    string
	CreatedAt time.Time
}

// GetCard returns the full record for id, including labels. Use ListCards,
// SearchCards, or ReadyCards for bulk reads that do not need the full-fat
// projection.
func (db *DB) GetCard(ctx context.Context, id string) (*Card, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}
	return loadCard(ctx, db.sql, id)
}

// Notes returns every note attached to cardID, in chronological order.
func (db *DB) Notes(ctx context.Context, cardID string) ([]Note, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}

	var exists int
	if err := db.sql.QueryRowContext(ctx, `SELECT 1 FROM cards WHERE id = ?`, cardID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("bdd: card %s: %w", cardID, ErrNotFound)
		}
		return nil, fmt.Errorf("bdd: notes: %w", err)
	}

	rows, err := db.sql.QueryContext(ctx, `SELECT id, card_id, author, body, created_at FROM notes WHERE card_id = ? ORDER BY created_at ASC, id ASC`, cardID)
	if err != nil {
		return nil, fmt.Errorf("bdd: notes: %w", err)
	}
	defer rows.Close()

	var out []Note
	for rows.Next() {
		var n Note
		var author sql.NullString
		var createdAt string
		if err := rows.Scan(&n.ID, &n.CardID, &author, &n.Body, &createdAt); err != nil {
			return nil, fmt.Errorf("bdd: notes: %w", err)
		}
		n.Author = author.String
		t, err := parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("bdd: notes: %w", err)
		}
		n.CreatedAt = t
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bdd: notes: %w", err)
	}
	return out, nil
}

// execer is satisfied by both *sql.DB and *sql.Tx, letting storage helpers
// work inside or outside a transaction.
type execer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

const cardSelectSQL = `SELECT
	id, title, worktree, description, reproduction, design, acceptance,
	status, priority, card_type, external_ref, assignee, created_by,
	dispatchable, created_at, updated_at, started_at, closed_at, defer_until, revision
FROM cards WHERE id = ?`

// loadCard reads a card row plus its labels via q, wrapping a missing row in
// ErrNotFound.
func loadCard(ctx context.Context, q execer, id string) (*Card, error) {
	c, err := scanCard(q.QueryRowContext(ctx, cardSelectSQL, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("bdd: card %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("bdd: get card: %w", err)
	}

	labels, err := loadLabels(ctx, q, id)
	if err != nil {
		return nil, fmt.Errorf("bdd: get card: %w", err)
	}
	c.Labels = labels
	return c, nil
}

func loadLabels(ctx context.Context, q execer, id string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT label FROM labels WHERE card_id = ? ORDER BY label ASC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func scanCard(row scanner) (*Card, error) {
	var c Card
	var status, cardType string
	var priority int64
	var dispatchable int64
	var createdAt, updatedAt string
	var startedAt, closedAt, deferUntil sql.NullString

	if err := row.Scan(
		&c.ID, &c.Title, &c.Worktree, &c.Description, &c.Reproduction, &c.Design, &c.Acceptance,
		&status, &priority, &cardType, &c.ExternalRef, &c.Assignee, &c.CreatedBy,
		&dispatchable, &createdAt, &updatedAt, &startedAt, &closedAt, &deferUntil, &c.Revision,
	); err != nil {
		return nil, err
	}

	c.Status = Status(status)
	c.Type = CardType(cardType)
	c.Priority = int32(priority)
	c.Dispatchable = dispatchable != 0

	ca, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	c.CreatedAt = ca

	ua, err := parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	c.UpdatedAt = ua

	if c.StartedAt, err = parseNullableTime(startedAt); err != nil {
		return nil, err
	}
	if c.ClosedAt, err = parseNullableTime(closedAt); err != nil {
		return nil, err
	}
	if c.DeferUntil, err = parseNullableTime(deferUntil); err != nil {
		return nil, err
	}

	return &c, nil
}

// formatTime renders t as a UTC RFC3339 string with fractional seconds
// (plan section 18).
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

func parseNullableTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid || ns.String == "" {
		return nil, nil
	}
	t, err := parseTime(ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// validateLabels reports whether every label is non-empty, valid UTF-8, and
// at most MaxLabelBytes long.
func validateLabels(labels []string) bool {
	for _, l := range labels {
		if l == "" || len(l) > MaxLabelBytes || !utf8.ValidString(l) {
			return false
		}
	}
	return true
}

// dedupe returns items with duplicates removed, preserving first-seen order,
// so repeated label/parent inputs behave idempotently.
func dedupe(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		if seen[it] {
			continue
		}
		seen[it] = true
		out = append(out, it)
	}
	return out
}

// Parents returns every card that blocks id, ordered by ID ascending. It
// returns an empty (not nil) slice when id has no parents, and ErrNotFound
// when id does not exist.
func (db *DB) Parents(ctx context.Context, id string) ([]CardRef, error) {
	const query = `SELECT c.id, c.title, c.card_type, c.status
FROM card_edges ce JOIN cards c ON c.id = ce.parent_id
WHERE ce.child_id = ? ORDER BY c.id ASC`
	return db.edgeRefs(ctx, id, query)
}

// Children returns every card blocked by id, ordered by ID ascending. It
// returns an empty (not nil) slice when id has no children, and ErrNotFound
// when id does not exist.
func (db *DB) Children(ctx context.Context, id string) ([]CardRef, error) {
	const query = `SELECT c.id, c.title, c.card_type, c.status
FROM card_edges ce JOIN cards c ON c.id = ce.child_id
WHERE ce.parent_id = ? ORDER BY c.id ASC`
	return db.edgeRefs(ctx, id, query)
}
