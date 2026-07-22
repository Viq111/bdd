package bddsink

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/viq111/bdd"
	"github.com/viq111/bdd/tools/migrate/internal/model"
)

func verifyTx(ctx context.Context, tx *sql.Tx, plan model.Plan) error {
	for _, v := range plan.Cards {
		var id string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM cards WHERE id=?`, v.ID).Scan(&id); err != nil {
			return fmt.Errorf("bdd migration sink: missing imported card %q: %w", v.ID, err)
		}
	}
	for _, v := range plan.Runes {
		var key string
		if err := tx.QueryRowContext(ctx, `SELECT key FROM runes WHERE key=?`, v.Key).Scan(&key); err != nil {
			return fmt.Errorf("bdd migration sink: missing imported rune %q: %w", v.Key, err)
		}
	}
	for _, v := range plan.Memories {
		var key string
		if err := tx.QueryRowContext(ctx, `SELECT key FROM memories WHERE key=?`, v.Key).Scan(&key); err != nil {
			return fmt.Errorf("bdd migration sink: missing imported memory %q: %w", v.Key, err)
		}
	}
	for _, v := range plan.Notes {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE subject_kind='note' AND subject_key=? AND action='migration.note'`, v.SourceKey).Scan(&count); err != nil || count != 1 {
			return fmt.Errorf("bdd migration sink: note provenance %q count %d: %w", v.SourceKey, count, err)
		}
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("bdd migration sink: foreign_key_check failed")
	}
	return rows.Err()
}

func verifyPublic(ctx context.Context, path string, plan model.Plan) error {
	db, err := bdd.Open(ctx, bdd.OpenOptions{Path: path})
	if err != nil {
		return err
	}
	defer db.Close()
	for _, v := range plan.Cards {
		c, e := db.GetCard(ctx, v.ID)
		if e != nil {
			return e
		}
		if c.ID != v.ID || c.Title != v.Title || string(c.Status) != v.Status || string(c.Type) != v.Type {
			return fmt.Errorf("bdd migration sink: public card projection mismatch for %q", v.ID)
		}
	}
	for _, v := range plan.Runes {
		r, e := db.GetRune(ctx, v.Key)
		if e != nil {
			return e
		}
		if r.Key != v.Key || r.Body != v.Body {
			return fmt.Errorf("bdd migration sink: public rune projection mismatch for %q", v.Key)
		}
	}
	for _, v := range plan.Memories {
		m, e := db.Recall(ctx, v.Key)
		if e != nil {
			return e
		}
		if m.Body != v.Body {
			return fmt.Errorf("bdd migration sink: public memory projection mismatch for %q", v.Key)
		}
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer raw.Close()
	var result string
	if err = raw.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("bdd migration sink: integrity_check = %s", result)
	}
	return nil
}
