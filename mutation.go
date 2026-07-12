package bdd

import (
	"context"
	"time"
)

// CreateCard is the input to (*DB).CreateCard.
//
// Description, Reproduction, Design, and Acceptance are pointer-typed so
// CreateCard can distinguish an omitted field (nil) from an explicitly
// empty one (non-nil, pointing at ""). The creation-time required-field
// matrix checks presence, not content:
//
//	bug              requires Reproduction, Acceptance
//	task/feature/epic requires Acceptance
//	decision         requires Description, Design
//	chore            requires nothing beyond Title
//
// A required field satisfied by a non-nil pointer to "" is valid: it
// acknowledges the field was considered and intentionally left blank. On
// failure, CreateCard returns a *ValidationError listing every omitted
// field (not just the first) before opening the database, and never
// partially writes. Custom types carry no extra required-field rules.
type CreateCard struct {
	Title string
	Type  CardType

	Priority     *int32
	Description  *string
	Reproduction *string
	Design       *string
	Acceptance   *string
	ExternalRef  *string
	Worktree     *string

	Labels  []string
	Parents []string
	Notes   *string

	CreatedBy string
}

// UpdateCard is the input to (*DB).UpdateCard. Every pointer field left nil
// is left unchanged; a non-nil pointer to "" clears the stored value.
// Changing Type does not re-run CreateCard's required-field rules.
// AddLabels/RemoveLabels are idempotent. Status, label, and edge changes
// (via AddParent/RemoveParent/AddChild/RemoveChild) are applied atomically
// alongside field changes in a single transaction; an illegal status
// transition returns ErrInvalidTransition and nothing is written.
type UpdateCard struct {
	Title *string
	Type  *CardType

	Status *Status

	Priority     *int32
	Description  *string
	Reproduction *string
	Design       *string
	Acceptance   *string
	ExternalRef  *string

	Worktree      *string
	ClearWorktree bool

	AddLabels    []string
	RemoveLabels []string

	Actor string
}

// CloseCard is the input to (*DB).CloseCard.
type CloseCard struct {
	// Reason, if non-empty, is recorded as an appended note.
	Reason string
	Actor  string
}

// AddNote is the input to (*DB).AddNote.
type AddNote struct {
	CardID string
	Body   string
	Author string
}

// DeleteCardResult reports the edges removed by a successful DeleteCard
// call.
type DeleteCardResult struct {
	RemovedParents  []string
	RemovedChildren []string
}

// CreateCard creates a new card. See the CreateCard type for the
// required-field matrix and omitted-versus-explicitly-empty semantics.
// IDs are generated as <workspace-prefix>-<random-suffix>.
func (db *DB) CreateCard(ctx context.Context, in CreateCard) (*Card, error) {
	return nil, errNotImplemented
}

// UpdateCard applies in to the card identified by id in a single
// transaction and returns the resulting card. See the UpdateCard type for
// field semantics.
func (db *DB) UpdateCard(ctx context.Context, id string, in UpdateCard) (*Card, error) {
	return nil, errNotImplemented
}

// DeleteCard hard-deletes id and every record scoped to it (labels, notes,
// and edges in both directions), leaving a tombstone audit event. force
// must be true or DeleteCard returns ErrInvalidArgument without deleting
// anything. Deleting a card never mutates its parents or children beyond
// removing the edges to id itself.
func (db *DB) DeleteCard(ctx context.Context, id string, actor string, force bool) (*DeleteCardResult, error) {
	return nil, errNotImplemented
}

// AddNote appends a note to a card and returns it.
func (db *DB) AddNote(ctx context.Context, in AddNote) (*Note, error) {
	return nil, errNotImplemented
}

// AddLabel idempotently adds label to id.
func (db *DB) AddLabel(ctx context.Context, id, label, actor string) (*Card, error) {
	return nil, errNotImplemented
}

// RemoveLabel idempotently removes label from id.
func (db *DB) RemoveLabel(ctx context.Context, id, label, actor string) (*Card, error) {
	return nil, errNotImplemented
}

// ClaimCard atomically moves an active-category card to in_progress,
// setting Assignee and StartedAt. Claiming a card already claimed by a
// different actor returns ErrClaimed; claiming again as the same actor is a
// no-op that returns the current card.
func (db *DB) ClaimCard(ctx context.Context, id, actor string) (*Card, error) {
	return nil, errNotImplemented
}

// CloseCard moves a card to a done-category status, setting ClosedAt.
// CloseCard is idempotent.
func (db *DB) CloseCard(ctx context.Context, id string, in CloseCard) (*Card, error) {
	return nil, errNotImplemented
}

// ReopenCard moves a done-category card back to StatusOpen, clearing
// ClosedAt, StartedAt, and Assignee.
func (db *DB) ReopenCard(ctx context.Context, id string, actor string) (*Card, error) {
	return nil, errNotImplemented
}

// DeferCard moves a card to StatusDeferred, optionally recording until as
// DeferUntil. Deferral is never applied automatically by time passing.
func (db *DB) DeferCard(ctx context.Context, id string, actor string, until *time.Time) (*Card, error) {
	return nil, errNotImplemented
}

// HumanCard atomically adds the "human" label and appends reason as a note
// in one transaction, flagging the card as needing human attention.
func (db *DB) HumanCard(ctx context.Context, id string, actor string, reason string) (*Card, error) {
	return nil, errNotImplemented
}

// AddParent adds a blocking edge: parentID must reach a done-category
// status before childID is ready. Idempotent; rejects self-edges
// (ErrInvalidArgument) and edges that would create a cycle (ErrCycle).
func (db *DB) AddParent(ctx context.Context, childID, parentID, actor string) error {
	return errNotImplemented
}

// RemoveParent idempotently removes the blocking edge added by AddParent.
func (db *DB) RemoveParent(ctx context.Context, childID, parentID, actor string) error {
	return errNotImplemented
}

// AddChild adds a blocking edge in the opposite direction of AddParent:
// parentID must reach a done-category status before childID is ready.
func (db *DB) AddChild(ctx context.Context, parentID, childID, actor string) error {
	return errNotImplemented
}

// RemoveChild idempotently removes the blocking edge added by AddChild.
func (db *DB) RemoveChild(ctx context.Context, parentID, childID, actor string) error {
	return errNotImplemented
}
