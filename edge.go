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

// cardRevision returns id's current revision, wrapping a missing row in
// ErrNotFound.
func cardRevision(ctx context.Context, q execer, id string) (int64, error) {
	var rev int64
	err := q.QueryRowContext(ctx, `SELECT revision FROM cards WHERE id = ?`, id).Scan(&rev)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("bdd: card %s: %w", id, ErrNotFound)
		}
		return 0, err
	}
	return rev, nil
}

// wouldCreateCycle reports whether adding the edge parentID -> childID would
// close a cycle: true iff parentID is already reachable by walking forward
// (parent_id -> child_id) from childID, i.e. childID already transitively
// blocks parentID.
func wouldCreateCycle(ctx context.Context, q execer, parentID, childID string) (bool, error) {
	const query = `
WITH RECURSIVE descendants(id) AS (
	SELECT child_id FROM card_edges WHERE parent_id = ?
	UNION
	SELECT ce.child_id FROM card_edges ce JOIN descendants d ON ce.parent_id = d.id
)
SELECT 1 FROM descendants WHERE id = ? LIMIT 1`

	var one int
	err := q.QueryRowContext(ctx, query, childID, parentID).Scan(&one)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// addEdge idempotently records the blocking edge parentID -> childID. It
// rejects self-edges and edges that would close a cycle (ErrCycle), and
// validates that both cards exist before writing (ErrNotFound). Adding an
// edge that already exists is a successful no-op.
func (db *DB) addEdge(ctx context.Context, parentID, childID, actor string) error {
	if parentID == "" || childID == "" {
		return fmt.Errorf("bdd: add edge: parent and child ids are required: %w", ErrInvalidArgument)
	}
	if parentID == childID {
		return fmt.Errorf("bdd: add edge: a card cannot be its own parent: %w", ErrInvalidArgument)
	}

	if err := db.ready(); err != nil {
		return err
	}

	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		parentRev, err := cardRevision(ctx, tx, parentID)
		if err != nil {
			return err
		}
		childRev, err := cardRevision(ctx, tx, childID)
		if err != nil {
			return err
		}

		var exists int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM card_edges WHERE parent_id = ? AND child_id = ?`, parentID, childID).Scan(&exists)
		if err == nil {
			return tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		cyclic, err := wouldCreateCycle(ctx, tx, parentID, childID)
		if err != nil {
			return err
		}
		if cyclic {
			return fmt.Errorf("bdd: add edge %s -> %s: %w", parentID, childID, ErrCycle)
		}

		now := formatTime(time.Now())
		if _, err := tx.ExecContext(ctx, `INSERT INTO card_edges (parent_id, child_id, created_at, created_by) VALUES (?, ?, ?, ?)`, parentID, childID, now, actor); err != nil {
			return err
		}

		childPayload, _ := json.Marshal(map[string]any{"parent_id": parentID})
		if err := writeEvent(ctx, tx, childID, childRev, "add_parent", actor, now, childPayload); err != nil {
			return err
		}
		parentPayload, _ := json.Marshal(map[string]any{"child_id": childID})
		if err := writeEvent(ctx, tx, parentID, parentRev, "add_child", actor, now, parentPayload); err != nil {
			return err
		}

		return tx.Commit()
	})
	return translateWriteErr(err, "add edge")
}

// removeEdge idempotently deletes the blocking edge parentID -> childID.
// Removing an edge that does not exist is a successful no-op.
func (db *DB) removeEdge(ctx context.Context, parentID, childID, actor string) error {
	if parentID == "" || childID == "" {
		return fmt.Errorf("bdd: remove edge: parent and child ids are required: %w", ErrInvalidArgument)
	}

	if err := db.ready(); err != nil {
		return err
	}

	err := sqlite.Retry(ctx, func() error {
		tx, err := db.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		res, err := tx.ExecContext(ctx, `DELETE FROM card_edges WHERE parent_id = ? AND child_id = ?`, parentID, childID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return tx.Commit()
		}

		now := formatTime(time.Now())
		parentRev, err := cardRevision(ctx, tx, parentID)
		if err != nil {
			return err
		}
		childRev, err := cardRevision(ctx, tx, childID)
		if err != nil {
			return err
		}

		childPayload, _ := json.Marshal(map[string]any{"parent_id": parentID})
		if err := writeEvent(ctx, tx, childID, childRev, "remove_parent", actor, now, childPayload); err != nil {
			return err
		}
		parentPayload, _ := json.Marshal(map[string]any{"child_id": childID})
		if err := writeEvent(ctx, tx, parentID, parentRev, "remove_child", actor, now, parentPayload); err != nil {
			return err
		}

		return tx.Commit()
	})
	return translateWriteErr(err, "remove edge")
}

// edgeRefs runs query (which must select id, title, card_type, status and
// take a single id parameter) and returns the matching cards, or an empty
// (non-nil) slice if none match. It first checks that id itself exists.
func (db *DB) edgeRefs(ctx context.Context, id, query string) ([]CardRef, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}

	var exists int
	if err := db.sql.QueryRowContext(ctx, `SELECT 1 FROM cards WHERE id = ?`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("bdd: card %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("bdd: edges: %w", err)
	}

	rows, err := db.sql.QueryContext(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("bdd: edges: %w", err)
	}
	defer rows.Close()

	out := []CardRef{}
	for rows.Next() {
		var r CardRef
		var typ, status string
		if err := rows.Scan(&r.ID, &r.Title, &typ, &status); err != nil {
			return nil, fmt.Errorf("bdd: edges: %w", err)
		}
		r.Type = CardType(typ)
		r.Status = Status(status)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bdd: edges: %w", err)
	}
	return out, nil
}

// queryEdgeIDs runs query (which must select a single TEXT column and take
// one id parameter) and returns the matching values, or an empty (non-nil)
// slice if none match.
func queryEdgeIDs(ctx context.Context, q execer, query, id string) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
