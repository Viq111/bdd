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

	Labels []string
	// Parents attaches blocking parent edges at creation time. Every parent
	// must already exist; if any does not, CreateCard writes nothing and
	// returns ErrNotFound. A cycle is impossible here since the new card's
	// ID cannot yet appear as anyone's parent.
	Parents []string
	Notes   *string

	CreatedBy string
	// Owner is immutable source metadata, distinct from the assignee who is
	// currently working the card. It is primarily used by imports.
	Owner string
}

// UpdateCard is the input to (*DB).UpdateCard. Every pointer field left nil
// is left unchanged; a non-nil pointer to "" clears the stored value.
// Changing Type does not re-run CreateCard's required-field rules.
// AddLabels/RemoveLabels are idempotent. Status, label, and edge changes
// (via AddParents/RemoveParents/AddChildren/RemoveChildren) are applied
// atomically alongside field changes in a single transaction; an illegal
// status transition returns ErrInvalidTransition and nothing is written.
//
// Status is validated against categoryTransitionAllowed: every transition
// is legal except leaving a done-category status directly, which requires
// ReopenCard instead (the only mutation that clears ClosedAt/StartedAt/
// Assignee alongside the status change).
//
// AddParents/RemoveParents/AddChildren/RemoveChildren carry the same
// self-edge, cycle, and existence semantics as AddParent/RemoveParent/
// AddChild/RemoveChild.
//
// Claim folds ClaimCard's transition (active-category -> in_progress,
// setting Assignee and StartedAt) into the same transaction as the rest of
// the field changes, so a call combining --claim with a field change either
// applies both or neither. Claim carries the same idempotency as ClaimCard:
// claiming again as the current Assignee is a no-op. Claim and Status may
// not both be set (Claim already drives the status transition); combining
// them returns ErrInvalidArgument.
type UpdateCard struct {
	Title *string
	Type  *CardType

	Status *Status
	Claim  bool

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

	AddParents     []string
	RemoveParents  []string
	AddChildren    []string
	RemoveChildren []string

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
	if !validateUTF8(in.Title, strOrEmpty(in.Description), strOrEmpty(in.Reproduction),
		strOrEmpty(in.Design), strOrEmpty(in.Acceptance), strOrEmpty(in.ExternalRef),
		strOrEmpty(in.Worktree), strOrEmpty(in.Notes), in.Owner) {
		return nil, fmt.Errorf("bdd: create card: title/description/reproduction/design/acceptance/external_ref/worktree/notes/owner must be valid UTF-8: %w", ErrInvalidArgument)
	}
	if in.Priority != nil && *in.Priority < 0 {
		return nil, fmt.Errorf("bdd: create card: priority must be >= 0: %w", ErrInvalidArgument)
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
	parents := dedupe(in.Parents)

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

		// A freshly generated ID cannot already appear as anyone's parent, so
		// no cycle is possible here; only existence needs checking.
		for _, p := range parents {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM cards WHERE id = ?`, p).Scan(&exists); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("bdd: create card: parent %s: %w", p, ErrNotFound)
				}
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO card_edges (parent_id, child_id, created_at, created_by) VALUES (?, ?, ?, ?)`, p, id, nowStr, in.CreatedBy); err != nil {
				return err
			}
			childPayload, _ := json.Marshal(map[string]any{"parent_id": p})
			if err := writeEvent(ctx, tx, id, 1, "add_parent", in.CreatedBy, nowStr, childPayload); err != nil {
				return err
			}
			parentRev, err := cardRevision(ctx, tx, p)
			if err != nil {
				return err
			}
			parentPayload, _ := json.Marshal(map[string]any{"child_id": id})
			if err := writeEvent(ctx, tx, p, parentRev, "add_child", in.CreatedBy, nowStr, parentPayload); err != nil {
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
// field semantics, including the Status transition rules.
func (db *DB) UpdateCard(ctx context.Context, id string, in UpdateCard) (*Card, error) {
	if err := validateUpdateCard(in); err != nil {
		return nil, err
	}

	if err := db.ready(); err != nil {
		return nil, err
	}

	addLabels := dedupe(in.AddLabels)
	removeLabels := dedupe(in.RemoveLabels)
	addParents := dedupe(in.AddParents)
	removeParents := dedupe(in.RemoveParents)
	addChildren := dedupe(in.AddChildren)
	removeChildren := dedupe(in.RemoveChildren)

	var card *Card
	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		now := time.Now().UTC()
		nowStr := formatTime(now)

		if in.Status != nil {
			var curStatus string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM cards WHERE id = ?`, id).Scan(&curStatus); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("bdd: card %s: %w", id, ErrNotFound)
				}
				return err
			}
			fromCategory, err := statusCategory(ctx, tx, Status(curStatus))
			if err != nil {
				return err
			}
			toCategory, err := statusCategory(ctx, tx, *in.Status)
			if err != nil {
				return err
			}
			if !categoryTransitionAllowed(fromCategory, toCategory) {
				return fmt.Errorf("bdd: update card %s: cannot transition from %s (%s) to %s (%s): %w",
					id, curStatus, fromCategory, *in.Status, toCategory, ErrInvalidTransition)
			}
		}

		// claiming reports whether this call performs an actual
		// active-category -> in_progress transition. When in.Claim is set
		// but the card is already claimed by in.Actor, claiming stays false
		// (ClaimCard's no-op semantics), while any other field/label/edge
		// changes in in still apply.
		var claiming bool
		if in.Claim {
			var curStatus, curAssignee string
			if err := tx.QueryRowContext(ctx, `SELECT status, assignee FROM cards WHERE id = ?`, id).Scan(&curStatus, &curAssignee); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("bdd: card %s: %w", id, ErrNotFound)
				}
				return err
			}
			category, err := statusCategory(ctx, tx, Status(curStatus))
			if err != nil {
				return err
			}
			switch {
			case category == StatusCategoryWIP:
				if curAssignee != in.Actor {
					if curAssignee != "" {
						return fmt.Errorf("bdd: claim card %s: already claimed by %s: %w", id, curAssignee, ErrClaimed)
					}
					return fmt.Errorf("bdd: claim card %s: cannot claim a %s-category card: %w", id, category, ErrInvalidTransition)
				}
			case category != StatusCategoryActive:
				return fmt.Errorf("bdd: claim card %s: cannot claim a %s-category card: %w", id, category, ErrInvalidTransition)
			default:
				claiming = true
			}
		}

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

		if claiming {
			sets = append(sets, "status = ?", "assignee = ?", "started_at = ?")
			args = append(args, string(StatusInProgress), in.Actor, nowStr)
			changed["status"] = string(StatusInProgress)
			changed["assignee"] = in.Actor
			changed["claim"] = true
		}

		hasLabelOrEdgeChanges := len(addLabels) > 0 || len(removeLabels) > 0 ||
			len(addParents) > 0 || len(removeParents) > 0 || len(addChildren) > 0 || len(removeChildren) > 0
		if in.Claim && !claiming && len(sets) == 2 && !hasLabelOrEdgeChanges {
			// Already claimed by in.Actor and nothing else to change: mirror
			// ClaimCard's idempotent no-op rather than bumping revision for
			// a write that changes nothing observable.
			got, err := loadCard(ctx, tx, id)
			if err != nil {
				return err
			}
			card = got
			return tx.Commit()
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

		for _, p := range addParents {
			if err := addEdgeTx(ctx, tx, p, id, in.Actor, nowStr); err != nil {
				return err
			}
		}
		for _, p := range removeParents {
			if err := removeEdgeTx(ctx, tx, p, id, in.Actor, nowStr); err != nil {
				return err
			}
		}
		for _, c := range addChildren {
			if err := addEdgeTx(ctx, tx, id, c, in.Actor, nowStr); err != nil {
				return err
			}
		}
		for _, c := range removeChildren {
			if err := removeEdgeTx(ctx, tx, id, c, in.Actor, nowStr); err != nil {
				return err
			}
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
	if in.Claim && in.Status != nil {
		return fmt.Errorf("bdd: update card: cannot combine claim with an explicit status change: %w", ErrInvalidArgument)
	}
	if in.Claim && in.Actor == "" {
		return fmt.Errorf("bdd: update card: actor is required to claim: %w", ErrInvalidArgument)
	}
	if !validateLabels(in.AddLabels) || !validateLabels(in.RemoveLabels) {
		return fmt.Errorf("bdd: update card: labels must be non-empty, valid UTF-8, and at most %d bytes: %w", MaxLabelBytes, ErrInvalidArgument)
	}
	if !validateUTF8(strOrEmpty(in.Title), strOrEmpty(in.Description), strOrEmpty(in.Reproduction),
		strOrEmpty(in.Design), strOrEmpty(in.Acceptance), strOrEmpty(in.ExternalRef), strOrEmpty(in.Worktree)) {
		return fmt.Errorf("bdd: update card: title/description/reproduction/design/acceptance/external_ref/worktree must be valid UTF-8: %w", ErrInvalidArgument)
	}
	for _, ids := range [][]string{in.AddParents, in.RemoveParents, in.AddChildren, in.RemoveChildren} {
		for _, id := range ids {
			if id == "" {
				return fmt.Errorf("bdd: update card: edge card ids must be non-empty: %w", ErrInvalidArgument)
			}
		}
	}
	return nil
}

const insertCardSQL = `INSERT INTO cards (
	id, title, worktree, description, reproduction, design, acceptance,
	status, priority, card_type, external_ref, assignee, created_by,
	owner, dispatchable, created_at, updated_at, started_at, closed_at, defer_until, revision
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, 1)`

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
			in.CreatedBy, in.Owner, 1, now, now,
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
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidArgument) || errors.Is(err, ErrAlreadyExists) || errors.Is(err, ErrCycle) {
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
// removing the edges to id itself; it never re-evaluates or writes to
// former parents or children.
func (db *DB) DeleteCard(ctx context.Context, id string, actor string, force bool) (*DeleteCardResult, error) {
	if !force {
		return nil, fmt.Errorf("bdd: delete card: force must be true: %w", ErrInvalidArgument)
	}

	if err := db.ready(); err != nil {
		return nil, err
	}

	var result *DeleteCardResult
	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		var revision int64
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM cards WHERE id = ?`, id).Scan(&revision); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("bdd: card %s: %w", id, ErrNotFound)
			}
			return err
		}

		parents, err := queryEdgeIDs(ctx, tx, `SELECT parent_id FROM card_edges WHERE child_id = ? ORDER BY parent_id ASC`, id)
		if err != nil {
			return err
		}
		children, err := queryEdgeIDs(ctx, tx, `SELECT child_id FROM card_edges WHERE parent_id = ? ORDER BY child_id ASC`, id)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM cards WHERE id = ?`, id); err != nil {
			return err
		}

		now := formatTime(time.Now())
		payload, _ := json.Marshal(map[string]any{"removed_parents": parents, "removed_children": children})
		if err := writeEvent(ctx, tx, id, revision, "delete", actor, now, payload); err != nil {
			return err
		}

		result = &DeleteCardResult{RemovedParents: parents, RemovedChildren: children}
		return tx.Commit()
	})
	if err != nil {
		return nil, translateWriteErr(err, "delete card")
	}
	return result, nil
}

// AddNote appends a note to a card and returns it.
func (db *DB) AddNote(ctx context.Context, in AddNote) (*Note, error) {
	if in.CardID == "" {
		return nil, fmt.Errorf("bdd: add note: card_id is required: %w", ErrInvalidArgument)
	}
	if in.Body == "" {
		return nil, fmt.Errorf("bdd: add note: body is required: %w", ErrInvalidArgument)
	}
	if !validateUTF8(in.Body) {
		return nil, fmt.Errorf("bdd: add note: body must be valid UTF-8: %w", ErrInvalidArgument)
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

		now := time.Now().UTC()
		nowStr := formatTime(now)

		var revision int64
		if err := tx.QueryRowContext(ctx, `UPDATE cards SET revision = revision + 1, updated_at = ? WHERE id = ? RETURNING revision`, nowStr, in.CardID).Scan(&revision); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("bdd: card %s: %w", in.CardID, ErrNotFound)
			}
			return err
		}

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
	return db.UpdateCard(ctx, id, UpdateCard{AddLabels: []string{label}, Actor: actor})
}

// RemoveLabel idempotently removes label from id.
func (db *DB) RemoveLabel(ctx context.Context, id, label, actor string) (*Card, error) {
	return db.UpdateCard(ctx, id, UpdateCard{RemoveLabels: []string{label}, Actor: actor})
}

// ClaimCard, CloseCard, ReopenCard, DeferCard, and HumanCard (the dedicated
// lifecycle mutations) live in lifecycle.go alongside the status-category
// transition rules they and UpdateCard share.

// AddParent adds a blocking edge: parentID must reach a done-category
// status before childID is ready. Idempotent; rejects self-edges
// (ErrInvalidArgument), edges that would create a cycle (ErrCycle), and
// edges referencing a card that does not exist (ErrNotFound).
func (db *DB) AddParent(ctx context.Context, childID, parentID, actor string) error {
	return db.addEdge(ctx, parentID, childID, actor)
}

// RemoveParent idempotently removes the blocking edge added by AddParent.
func (db *DB) RemoveParent(ctx context.Context, childID, parentID, actor string) error {
	return db.removeEdge(ctx, parentID, childID, actor)
}

// AddChild adds a blocking edge in the opposite direction of AddParent:
// parentID must reach a done-category status before childID is ready. It is
// the same underlying edge as AddParent(childID, parentID, actor) and
// carries the same idempotency, self-edge, cycle, and existence semantics.
func (db *DB) AddChild(ctx context.Context, parentID, childID, actor string) error {
	return db.addEdge(ctx, parentID, childID, actor)
}

// RemoveChild idempotently removes the blocking edge added by AddChild.
func (db *DB) RemoveChild(ctx context.Context, parentID, childID, actor string) error {
	return db.removeEdge(ctx, parentID, childID, actor)
}
