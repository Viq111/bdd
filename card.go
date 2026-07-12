package bdd

import (
	"context"
	"time"
)

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
	return nil, errNotImplemented
}

// Notes returns every note attached to cardID, in chronological order.
func (db *DB) Notes(ctx context.Context, cardID string) ([]Note, error) {
	return nil, errNotImplemented
}

// Parents returns every card that blocks id, in deterministic order.
func (db *DB) Parents(ctx context.Context, id string) ([]CardRef, error) {
	return nil, errNotImplemented
}

// Children returns every card blocked by id, in deterministic order.
func (db *DB) Children(ctx context.Context, id string) ([]CardRef, error) {
	return nil, errNotImplemented
}
