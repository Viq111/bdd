package bdd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/viq111/bdd/internal/sqlite"
)

// HumanLabel is the reserved label ReadyCards and the readiness predicate
// treat as "needs human attention": a card carrying it is never ready,
// regardless of status (plan section 16).
const HumanLabel = "human"

// statusCategory resolves status to its StatusCategory, checking built-in
// statuses in memory before falling back to a status_definitions lookup for
// custom ones. It returns ErrInvalidArgument if status is not a status the
// workspace accepts.
func statusCategory(ctx context.Context, q execer, status Status) (StatusCategory, error) {
	if cat, ok := BuiltinStatusCategories[status]; ok {
		return cat, nil
	}
	var cat string
	err := q.QueryRowContext(ctx, `SELECT category FROM status_definitions WHERE name = ?`, string(status)).Scan(&cat)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("bdd: unknown status %q: %w", status, ErrInvalidArgument)
		}
		return "", err
	}
	return StatusCategory(cat), nil
}

// categoryTransitionAllowed reports whether a direct status write (via
// UpdateCard's Status field) may move a card from category from to
// category to. Every transition is allowed except leaving done directly:
// a done-category card can only move to a different done-category status
// (closed <-> wontfix, or a custom done status) through UpdateCard.
// Un-closing a card requires ReopenCard instead, since only it clears the
// ClosedAt/StartedAt/Assignee fields a done-category card accumulated
// (plan section 16).
func categoryTransitionAllowed(from, to StatusCategory) bool {
	if from == to {
		return true
	}
	return from != StatusCategoryDone
}

// ClaimCard atomically moves an active-category card to in_progress,
// setting Assignee and StartedAt. Claiming a card already claimed by a
// different actor returns ErrClaimed; claiming again as the same actor is a
// no-op that returns the current card. Claiming anything outside the
// active category (including a card already claimed by a different mutation
// path, left in wip with no assignee) returns ErrInvalidTransition.
func (db *DB) ClaimCard(ctx context.Context, id, actor string) (*Card, error) {
	if actor == "" {
		return nil, fmt.Errorf("bdd: claim card: actor is required: %w", ErrInvalidArgument)
	}

	if err := db.ready(); err != nil {
		return nil, err
	}

	var card *Card
	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		cur, err := loadCard(ctx, tx, id)
		if err != nil {
			return err
		}

		category, err := statusCategory(ctx, tx, cur.Status)
		if err != nil {
			return err
		}

		if category == StatusCategoryWIP {
			if cur.Assignee == actor {
				card = cur
				return tx.Commit()
			}
			if cur.Assignee != "" {
				return fmt.Errorf("bdd: claim card %s: already claimed by %s: %w", id, cur.Assignee, ErrClaimed)
			}
			return fmt.Errorf("bdd: claim card %s: cannot claim a %s-category card: %w", id, category, ErrInvalidTransition)
		}
		if category != StatusCategoryActive {
			return fmt.Errorf("bdd: claim card %s: cannot claim a %s-category card: %w", id, category, ErrInvalidTransition)
		}

		now := formatTime(time.Now())
		if _, err := tx.ExecContext(ctx, `UPDATE cards SET status = ?, assignee = ?, started_at = ?, updated_at = ?, revision = revision + 1 WHERE id = ?`,
			string(StatusInProgress), actor, now, now, id); err != nil {
			return err
		}

		got, err := loadCard(ctx, tx, id)
		if err != nil {
			return err
		}

		payload, _ := json.Marshal(map[string]any{"from_status": string(cur.Status), "to_status": string(StatusInProgress)})
		if err := writeEvent(ctx, tx, id, got.Revision, "claim", actor, now, payload); err != nil {
			return err
		}

		card = got
		return tx.Commit()
	})
	if err != nil {
		return nil, translateWriteErr(err, "claim card")
	}
	return card, nil
}

// CloseCard moves a card to StatusClosed, setting ClosedAt. CloseCard is
// idempotent: closing a card already in a done-category status (closed,
// wontfix, or a custom done status) leaves its status and ClosedAt
// untouched. A non-empty Reason is appended as a note on every call,
// closed or not.
func (db *DB) CloseCard(ctx context.Context, id string, in CloseCard) (*Card, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}

	var card *Card
	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		cur, err := loadCard(ctx, tx, id)
		if err != nil {
			return err
		}

		category, err := statusCategory(ctx, tx, cur.Status)
		if err != nil {
			return err
		}
		statusChanging := category != StatusCategoryDone

		if !statusChanging && in.Reason == "" {
			card = cur
			return tx.Commit()
		}

		now := formatTime(time.Now())

		if statusChanging {
			if _, err := tx.ExecContext(ctx, `UPDATE cards SET status = ?, closed_at = ?, updated_at = ?, revision = revision + 1 WHERE id = ?`,
				string(StatusClosed), now, now, id); err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE cards SET updated_at = ?, revision = revision + 1 WHERE id = ?`, now, id); err != nil {
				return err
			}
		}

		if in.Reason != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO notes (card_id, author, body, created_at) VALUES (?, ?, ?, ?)`, id, in.Actor, in.Reason, now); err != nil {
				return err
			}
		}

		got, err := loadCard(ctx, tx, id)
		if err != nil {
			return err
		}

		payload := map[string]any{"reason": in.Reason}
		if statusChanging {
			payload["from_status"] = string(cur.Status)
			payload["to_status"] = string(StatusClosed)
		}
		payloadJSON, _ := json.Marshal(payload)
		if err := writeEvent(ctx, tx, id, got.Revision, "close", in.Actor, now, payloadJSON); err != nil {
			return err
		}

		card = got
		return tx.Commit()
	})
	if err != nil {
		return nil, translateWriteErr(err, "close card")
	}
	return card, nil
}

// ReopenCard moves a done-category card back to StatusOpen, clearing
// ClosedAt, StartedAt, and Assignee. Reopening a card that is not currently
// in a done-category status returns ErrInvalidTransition.
func (db *DB) ReopenCard(ctx context.Context, id string, actor string) (*Card, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}

	var card *Card
	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		cur, err := loadCard(ctx, tx, id)
		if err != nil {
			return err
		}

		category, err := statusCategory(ctx, tx, cur.Status)
		if err != nil {
			return err
		}
		if category != StatusCategoryDone {
			return fmt.Errorf("bdd: reopen card %s: cannot reopen a %s-category card: %w", id, category, ErrInvalidTransition)
		}

		now := formatTime(time.Now())
		if _, err := tx.ExecContext(ctx, `UPDATE cards SET status = ?, assignee = '', started_at = NULL, closed_at = NULL, updated_at = ?, revision = revision + 1 WHERE id = ?`,
			string(StatusOpen), now, id); err != nil {
			return err
		}

		got, err := loadCard(ctx, tx, id)
		if err != nil {
			return err
		}

		payload, _ := json.Marshal(map[string]any{"from_status": string(cur.Status), "to_status": string(StatusOpen)})
		if err := writeEvent(ctx, tx, id, got.Revision, "reopen", actor, now, payload); err != nil {
			return err
		}

		card = got
		return tx.Commit()
	})
	if err != nil {
		return nil, translateWriteErr(err, "reopen card")
	}
	return card, nil
}

// DeferCard moves a card to StatusDeferred, optionally recording until as
// DeferUntil (a nil until leaves DeferUntil unset). Deferral is never
// applied automatically by time passing; DeferUntil is purely informational
// until something re-evaluates the card. Deferring a done-category card
// returns ErrInvalidTransition.
func (db *DB) DeferCard(ctx context.Context, id string, actor string, until *time.Time) (*Card, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}

	var card *Card
	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		cur, err := loadCard(ctx, tx, id)
		if err != nil {
			return err
		}

		category, err := statusCategory(ctx, tx, cur.Status)
		if err != nil {
			return err
		}
		if category == StatusCategoryDone {
			return fmt.Errorf("bdd: defer card %s: cannot defer a done-category card: %w", id, ErrInvalidTransition)
		}

		now := formatTime(time.Now())
		var deferUntil any
		deferUntilPayload := ""
		if until != nil {
			deferUntilPayload = formatTime(*until)
			deferUntil = deferUntilPayload
		}
		if _, err := tx.ExecContext(ctx, `UPDATE cards SET status = ?, defer_until = ?, updated_at = ?, revision = revision + 1 WHERE id = ?`,
			string(StatusDeferred), deferUntil, now, id); err != nil {
			return err
		}

		got, err := loadCard(ctx, tx, id)
		if err != nil {
			return err
		}

		payload, _ := json.Marshal(map[string]any{"from_status": string(cur.Status), "to_status": string(StatusDeferred), "defer_until": deferUntilPayload})
		if err := writeEvent(ctx, tx, id, got.Revision, "defer", actor, now, payload); err != nil {
			return err
		}

		card = got
		return tx.Commit()
	})
	if err != nil {
		return nil, translateWriteErr(err, "defer card")
	}
	return card, nil
}

// HumanCard atomically adds the "human" label and appends reason as a note
// (when non-empty) in one transaction, flagging the card as needing human
// attention. It does not change status and carries no transition
// restriction; adding the label again is a no-op (labels are idempotent).
func (db *DB) HumanCard(ctx context.Context, id string, actor string, reason string) (*Card, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}

	var card *Card
	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, err := cardRevision(ctx, tx, id); err != nil {
			return err
		}

		now := formatTime(time.Now())
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO labels (card_id, label) VALUES (?, ?)`, id, HumanLabel); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE cards SET updated_at = ?, revision = revision + 1 WHERE id = ?`, now, id); err != nil {
			return err
		}
		if reason != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO notes (card_id, author, body, created_at) VALUES (?, ?, ?, ?)`, id, actor, reason, now); err != nil {
				return err
			}
		}

		got, err := loadCard(ctx, tx, id)
		if err != nil {
			return err
		}

		payload, _ := json.Marshal(map[string]any{"label": HumanLabel, "reason": reason})
		if err := writeEvent(ctx, tx, id, got.Revision, "human", actor, now, payload); err != nil {
			return err
		}

		card = got
		return tx.Commit()
	})
	if err != nil {
		return nil, translateWriteErr(err, "human card")
	}
	return card, nil
}
