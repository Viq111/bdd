package bdd

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/viq111/bdd/internal/sqlite"
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
	if err := requiredCreateFields(in); err != nil {
		return nil, err
	}
	if !validateLabels(in.Labels) {
		return nil, fmt.Errorf("bdd: create card: labels must be non-empty, valid UTF-8, and at most %d bytes: %w", MaxLabelBytes, ErrInvalidArgument)
	}
	if in.Priority != nil && *in.Priority < 0 {
		return nil, fmt.Errorf("bdd: create card: priority must be >= 0: %w", ErrInvalidArgument)
	}
	if len(in.Parents) > 0 {
		// Parent/child edges (including creation-time --parent) are out of
		// scope for this card; they land with cycle detection separately.
		return nil, fmt.Errorf("bdd: create card: parent/child edges are not yet supported: %w", ErrInvalidArgument)
	}

	if err := db.ready(); err != nil {
		return nil, err
	}

	prefix, err := db.workspacePrefix(ctx)
	if err != nil {
		return nil, fmt.Errorf("bdd: create card: reading workspace prefix: %w", err)
	}

	priority := int32(2)
	if in.Priority != nil {
		priority = *in.Priority
	}
	labels := dedupe(in.Labels)

	var card *Card
	err = sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		now := time.Now().UTC()
		nowStr := formatTime(now)

		id, err := insertCardRow(ctx, tx, prefix, in, priority, nowStr)
		if err != nil {
			return err
		}

		for _, l := range labels {
			if _, err := tx.ExecContext(ctx, `INSERT INTO labels (card_id, label) VALUES (?, ?)`, id, l); err != nil {
				return err
			}
		}

		if in.Notes != nil && *in.Notes != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO notes (card_id, author, body, created_at) VALUES (?, ?, ?, ?)`, id, in.CreatedBy, *in.Notes, nowStr); err != nil {
				return err
			}
		}

		payload, _ := json.Marshal(map[string]any{"title": in.Title, "type": string(in.Type)})
		if err := writeEvent(ctx, tx, id, 1, "create", in.CreatedBy, nowStr, payload); err != nil {
			return err
		}

		got, err := loadCard(ctx, tx, id)
		if err != nil {
			return err
		}
		card = got

		return tx.Commit()
	})
	if err != nil {
		return nil, translateWriteErr(err, "create card")
	}
	return card, nil
}

// UpdateCard applies in to the card identified by id in a single
// transaction and returns the resulting card. See the UpdateCard type for
// field semantics.
//
// Status is applied as an ordinary field (validated only by the database's
// status_definitions foreign key): the legal-transition state machine and
// ErrInvalidTransition land with the dedicated lifecycle mutations
// (ClaimCard, CloseCard, ReopenCard, DeferCard, HumanCard), which are out of
// scope for this method.
func (db *DB) UpdateCard(ctx context.Context, id string, in UpdateCard) (*Card, error) {
	if err := validateUpdateCard(in); err != nil {
		return nil, err
	}

	if err := db.ready(); err != nil {
		return nil, err
	}

	addLabels := dedupe(in.AddLabels)
	removeLabels := dedupe(in.RemoveLabels)

	var card *Card
	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		now := time.Now().UTC()
		nowStr := formatTime(now)

		sets := []string{"updated_at = ?", "revision = revision + 1"}
		args := []any{nowStr}
		changed := map[string]any{}

		if in.Title != nil {
			sets = append(sets, "title = ?")
			args = append(args, *in.Title)
			changed["title"] = *in.Title
		}
		if in.Type != nil {
			sets = append(sets, "card_type = ?")
			args = append(args, string(*in.Type))
			changed["type"] = string(*in.Type)
		}
		if in.Status != nil {
			sets = append(sets, "status = ?")
			args = append(args, string(*in.Status))
			changed["status"] = string(*in.Status)
		}
		if in.Priority != nil {
			sets = append(sets, "priority = ?")
			args = append(args, *in.Priority)
			changed["priority"] = *in.Priority
		}
		if in.Description != nil {
			sets = append(sets, "description = ?")
			args = append(args, *in.Description)
			changed["description"] = *in.Description
		}
		if in.Reproduction != nil {
			sets = append(sets, "reproduction = ?")
			args = append(args, *in.Reproduction)
			changed["reproduction"] = *in.Reproduction
		}
		if in.Design != nil {
			sets = append(sets, "design = ?")
			args = append(args, *in.Design)
			changed["design"] = *in.Design
		}
		if in.Acceptance != nil {
			sets = append(sets, "acceptance = ?")
			args = append(args, *in.Acceptance)
			changed["acceptance"] = *in.Acceptance
		}
		if in.ExternalRef != nil {
			sets = append(sets, "external_ref = ?")
			args = append(args, *in.ExternalRef)
			changed["external_ref"] = *in.ExternalRef
		}
		if in.ClearWorktree {
			sets = append(sets, "worktree = ?")
			args = append(args, "")
			changed["worktree"] = ""
		} else if in.Worktree != nil {
			sets = append(sets, "worktree = ?")
			args = append(args, *in.Worktree)
			changed["worktree"] = *in.Worktree
		}

		args = append(args, id)
		query := "UPDATE cards SET " + strings.Join(sets, ", ") + " WHERE id = ?"
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("bdd: card %s: %w", id, ErrNotFound)
		}

		for _, l := range addLabels {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO labels (card_id, label) VALUES (?, ?)`, id, l); err != nil {
				return err
			}
		}
		for _, l := range removeLabels {
			if _, err := tx.ExecContext(ctx, `DELETE FROM labels WHERE card_id = ? AND label = ?`, id, l); err != nil {
				return err
			}
		}
		if len(addLabels) > 0 {
			changed["add_labels"] = addLabels
		}
		if len(removeLabels) > 0 {
			changed["remove_labels"] = removeLabels
		}

		got, err := loadCard(ctx, tx, id)
		if err != nil {
			return err
		}

		payload, _ := json.Marshal(changed)
		if err := writeEvent(ctx, tx, id, got.Revision, "update", in.Actor, nowStr, payload); err != nil {
			return err
		}

		card = got
		return tx.Commit()
	})
	if err != nil {
		return nil, translateWriteErr(err, "update card")
	}
	return card, nil
}

// requiredCreateFields checks the creation-time required-field matrix
// (plan section 10): presence only, not content, and only for built-in
// types. Custom types carry no extra required fields.
func requiredCreateFields(in CreateCard) error {
	var missing []string

	if in.Title == "" {
		missing = append(missing, "title")
	}
	if in.Type == "" {
		missing = append(missing, "type")
	}

	switch in.Type {
	case CardTypeBug:
		if in.Reproduction == nil {
			missing = append(missing, "reproduction")
		}
		if in.Acceptance == nil {
			missing = append(missing, "acceptance")
		}
	case CardTypeTask, CardTypeFeature, CardTypeEpic:
		if in.Acceptance == nil {
			missing = append(missing, "acceptance")
		}
	case CardTypeDecision:
		if in.Description == nil {
			missing = append(missing, "description")
		}
		if in.Design == nil {
			missing = append(missing, "design")
		}
	case CardTypeChore:
		// No fields beyond title.
	default:
		// Custom type: no extra required-field rules in v1.
	}

	if len(missing) > 0 {
		return &ValidationError{Fields: missing}
	}
	return nil
}

// validateUpdateCard checks UpdateCard invariants that hold regardless of
// database state (non-DB-dependent, so they run before opening a
// transaction): a supplied title/type may not be cleared to empty, priority
// may not go negative, Worktree and ClearWorktree may not both be set, and
// any supplied label must be valid.
func validateUpdateCard(in UpdateCard) error {
	if in.Title != nil && *in.Title == "" {
		return fmt.Errorf("bdd: update card: title cannot be cleared: %w", ErrInvalidArgument)
	}
	if in.Type != nil && *in.Type == "" {
		return fmt.Errorf("bdd: update card: type cannot be cleared: %w", ErrInvalidArgument)
	}
	if in.Priority != nil && *in.Priority < 0 {
		return fmt.Errorf("bdd: update card: priority must be >= 0: %w", ErrInvalidArgument)
	}
	if in.ClearWorktree && in.Worktree != nil {
		return fmt.Errorf("bdd: update card: cannot set Worktree and ClearWorktree together: %w", ErrInvalidArgument)
	}
	if !validateLabels(in.AddLabels) {
		return fmt.Errorf("bdd: update card: labels must be non-empty, valid UTF-8, and at most %d bytes: %w", MaxLabelBytes, ErrInvalidArgument)
	}
	return nil
}

const insertCardSQL = `INSERT INTO cards (
	id, title, worktree, description, reproduction, design, acceptance,
	status, priority, card_type, external_ref, assignee, created_by,
	dispatchable, created_at, updated_at, started_at, closed_at, defer_until, revision
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, 1)`

const idAlphabet = "abcdefghijklmnopqrstuvwxyz234567" // lowercase base32 (RFC 4648), shell-safe
const idSuffixLen = 6
const maxIDAttempts = 8

// randomIDSuffix returns a crypto/rand-derived, lowercase, shell-safe
// suffix for card IDs (plan section 15). idAlphabet has exactly 32 symbols,
// so masking a random byte with &31 is unbiased.
func randomIDSuffix() (string, error) {
	raw := make([]byte, idSuffixLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("bdd: generating card id: %w", err)
	}
	out := make([]byte, idSuffixLen)
	for i, b := range raw {
		out[i] = idAlphabet[b&31]
	}
	return string(out), nil
}

// insertCardRow generates a fresh <prefix>-<suffix> ID and inserts the new
// card row, retrying with a new suffix on a primary-key collision.
func insertCardRow(ctx context.Context, tx *sql.Tx, prefix string, in CreateCard, priority int32, now string) (string, error) {
	for attempt := 0; attempt < maxIDAttempts; attempt++ {
		suffix, err := randomIDSuffix()
		if err != nil {
			return "", err
		}
		id := prefix + "-" + suffix

		_, err = tx.ExecContext(ctx, insertCardSQL,
			id, in.Title, strOrEmpty(in.Worktree), strOrEmpty(in.Description),
			strOrEmpty(in.Reproduction), strOrEmpty(in.Design), strOrEmpty(in.Acceptance),
			string(StatusOpen), priority, string(in.Type), strOrEmpty(in.ExternalRef), "",
			in.CreatedBy, 1, now, now,
		)
		if err == nil {
			return id, nil
		}
		if sqlite.IsUniqueViolation(err) {
			continue
		}
		return "", err
	}
	return "", fmt.Errorf("bdd: create card: exhausted %d attempts generating a unique id", maxIDAttempts)
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func writeEvent(ctx context.Context, tx *sql.Tx, cardID string, revision int64, action, actor, now string, payload []byte) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO events (subject_kind, subject_key, revision, action, actor, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"card", cardID, revision, action, actor, string(payload), now)
	return err
}

// translateWriteErr wraps a raw storage error from a card write in
// ErrInvalidArgument when it represents a constraint violation (invalid
// status/type reference, out-of-range priority, etc.), and otherwise
// passes typed sentinel errors (ErrNotFound and friends) through unchanged.
func translateWriteErr(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidArgument) || errors.Is(err, ErrAlreadyExists) {
		return err
	}
	msg := err.Error()
	if strings.Contains(msg, "FOREIGN KEY constraint failed") || strings.Contains(msg, "CHECK constraint failed") {
		return fmt.Errorf("bdd: %s: %w: %v", op, ErrInvalidArgument, err)
	}
	return fmt.Errorf("bdd: %s: %w", op, err)
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
	if in.CardID == "" {
		return nil, fmt.Errorf("bdd: add note: card_id is required: %w", ErrInvalidArgument)
	}
	if in.Body == "" {
		return nil, fmt.Errorf("bdd: add note: body is required: %w", ErrInvalidArgument)
	}

	if err := db.ready(); err != nil {
		return nil, err
	}

	var note *Note
	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		var revision int64
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM cards WHERE id = ?`, in.CardID).Scan(&revision); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("bdd: card %s: %w", in.CardID, ErrNotFound)
			}
			return err
		}

		now := time.Now().UTC()
		nowStr := formatTime(now)

		res, err := tx.ExecContext(ctx, `INSERT INTO notes (card_id, author, body, created_at) VALUES (?, ?, ?, ?)`, in.CardID, in.Author, in.Body, nowStr)
		if err != nil {
			return err
		}
		noteID, err := res.LastInsertId()
		if err != nil {
			return err
		}

		payload, _ := json.Marshal(map[string]any{"note_id": noteID})
		if err := writeEvent(ctx, tx, in.CardID, revision, "note", in.Author, nowStr, payload); err != nil {
			return err
		}

		note = &Note{ID: noteID, CardID: in.CardID, Author: in.Author, Body: in.Body, CreatedAt: now}
		return tx.Commit()
	})
	if err != nil {
		return nil, translateWriteErr(err, "add note")
	}
	return note, nil
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
