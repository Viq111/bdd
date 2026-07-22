// Package bddsink applies a canonical migration plan to a version-pinned bdd
// SQLite database.  It is deliberately the only migration package that uses
// bdd's storage schema directly.
package bddsink

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/viq111/bdd"
	"github.com/viq111/bdd/internal/sqlite"
	"github.com/viq111/bdd/tools/migrate/internal/model"
)

const actor = "bdd-migration"

// Apply initializes destination through bdd.Init when it does not exist and
// then writes the complete plan in one SQLite transaction.  A new database is
// initialized at a sibling path and only published after public API checks.
func Apply(ctx context.Context, destination, prefix string, plan model.Plan) (err error) {
	if destination == "" || prefix == "" {
		return fmt.Errorf("bdd migration sink: destination and prefix are required")
	}
	plan.Canonicalize()
	_, statErr := os.Stat(destination)
	newDB := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !newDB {
		return fmt.Errorf("bdd migration sink: stat destination: %w", statErr)
	}
	path := destination
	if newDB {
		path = filepath.Join(filepath.Dir(destination), "."+filepath.Base(destination)+".migration-tmp")
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		defer func() {
			if err != nil {
				for _, suffix := range []string{"", "-wal", "-shm"} {
					_ = os.Remove(path + suffix)
				}
			}
		}()
		db, initErr := bdd.Init(ctx, bdd.InitOptions{DBPath: path, Prefix: prefix})
		if initErr != nil {
			return fmt.Errorf("bdd migration sink: initialize destination: %w", initErr)
		}
		if closeErr := db.Close(); closeErr != nil {
			return closeErr
		}
	}
	if err = apply(ctx, path, plan); err != nil {
		return err
	}
	if err = verifyPublic(ctx, path, plan); err != nil {
		if newDB {
			return err
		}
		return fmt.Errorf("bdd migration sink: transaction committed but verification failed: %w", err)
	}
	if newDB {
		if err = os.Rename(path, destination); err != nil {
			return fmt.Errorf("bdd migration sink: publish destination: %w", err)
		}
	}
	return nil
}

func apply(ctx context.Context, path string, plan model.Plan) error {
	db, err := sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot, SkipJournalMode: true})
	if err != nil {
		return err
	}
	defer db.Close()
	if err := checkSchema(ctx, db); err != nil {
		return err
	}
	return sqlite.Retry(ctx, func() error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		now := time.Now().UTC().Format(time.RFC3339Nano)
		for _, v := range plan.Workspace.Statuses {
			if err := definition(ctx, tx, "status_definitions", v.Name, v.Category); err != nil {
				return err
			}
		}
		for _, v := range plan.Workspace.Types {
			if err := definition(ctx, tx, "type_definitions", v.Name, ""); err != nil {
				return err
			}
		}
		for _, v := range plan.Cards {
			if err := card(ctx, tx, v, now); err != nil {
				return err
			}
		}
		for _, v := range plan.Notes {
			if err := note(ctx, tx, v, now); err != nil {
				return err
			}
		}
		for _, v := range plan.Edges {
			if err := edge(ctx, tx, v, now); err != nil {
				return err
			}
		}
		for _, v := range plan.Runes {
			if err := rune(ctx, tx, v, now); err != nil {
				return err
			}
		}
		for _, v := range plan.Memories {
			if err := memory(ctx, tx, v, now); err != nil {
				return err
			}
		}
		if err := verifyTx(ctx, tx, plan); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func definition(ctx context.Context, tx *sql.Tx, table, name, category string) error {
	if table == "status_definitions" {
		var old string
		err := tx.QueryRowContext(ctx, `SELECT category FROM status_definitions WHERE name = ?`, name).Scan(&old)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, `INSERT INTO status_definitions (name, category, built_in) VALUES (?, ?, 0)`, name, category)
			return err
		}
		if err != nil {
			return err
		}
		if old != category {
			return fmt.Errorf("bdd migration sink: incompatible status %q", name)
		}
		return nil
	}
	var n string
	err := tx.QueryRowContext(ctx, `SELECT name FROM type_definitions WHERE name = ?`, name).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO type_definitions (name, built_in) VALUES (?, 0)`, name)
		return err
	}
	return err
}

func card(ctx context.Context, tx *sql.Tx, v model.CardPlan, now string) error {
	created, updated := timestamp(v.CreatedAt, now), timestamp(v.UpdatedAt, timestamp(v.CreatedAt, now))
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM cards WHERE id = ?`, v.ID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO cards (id,title,worktree,description,reproduction,design,acceptance,status,priority,card_type,external_ref,assignee,created_by,dispatchable,created_at,updated_at,started_at,closed_at,defer_until,revision) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?, ?,NULL,?,?,1)`, v.ID, v.Title, v.Worktree, v.Description, v.Reproduction, v.Design, v.Acceptance, v.Status, v.Priority, v.Type, v.ExternalRef, v.Assignee, v.Creator, 1, created, updated, timeValue(v.ClosedAt), timeValue(v.DeferUntil))
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		// Phase 2 preserves destination-owned cards; later rerun reconciliation
		// only touches records marked by a migration provenance event.
		var owned int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM events WHERE subject_kind='card' AND subject_key=? AND action='migration.card' LIMIT 1`, v.ID).Scan(&owned)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("bdd migration sink: destination-owned card ID collision %q", v.ID)
		}
		if err != nil {
			return err
		}
	}
	for _, label := range v.Labels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO labels (card_id,label) VALUES (?,?) ON CONFLICT(card_id,label) DO NOTHING`, v.ID, label); err != nil {
			return err
		}
	}
	return provenance(ctx, tx, "card", v.ID, 1, "migration.card", map[string]any{"source_system": "beads", "source_kind": "issue", "source_id": v.ID, "hash_version": v.HashVersion, "hash": v.Hash}, now)
}

func note(ctx context.Context, tx *sql.Tx, v model.NotePlan, now string) error {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM events WHERE subject_kind='note' AND subject_key=? AND action='migration.note' LIMIT 1`, v.SourceKey).Scan(&n)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO notes (card_id,author,body,created_at) VALUES (?,?,?,?)`, v.CardID, nullString(v.Author), v.Body, timestamp(v.CreatedAt, now)); err != nil {
		return err
	}
	return provenance(ctx, tx, "note", v.SourceKey, 1, "migration.note", map[string]any{"source_system": "beads", "source_kind": v.SourceKind, "source_id": v.SourceID, "source_key": v.SourceKey, "hash_version": v.HashVersion, "hash": v.Hash}, now)
}
func edge(ctx context.Context, tx *sql.Tx, v model.EdgePlan, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO card_edges (parent_id,child_id,created_at,created_by) VALUES (?,?,?,?) ON CONFLICT(parent_id,child_id) DO NOTHING`, v.ParentID, v.ChildID, now, actor)
	return err
}
func rune(ctx context.Context, tx *sql.Tx, v model.RunePlan, now string) error {
	metadata, err := json.Marshal(v.Metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO runes (key,kind,title,body,metadata_json,enabled,protected,created_by,updated_by,created_at,updated_at,revision) VALUES (?,?,?,?,?,?,?,?,?,?,?,1) ON CONFLICT(key) DO UPDATE SET title=excluded.title,body=excluded.body,metadata_json=excluded.metadata_json,enabled=excluded.enabled,protected=excluded.protected,updated_by=excluded.updated_by,updated_at=excluded.updated_at,revision=runes.revision+1 WHERE runes.kind<>excluded.kind OR runes.title<>excluded.title OR runes.body<>excluded.body OR runes.metadata_json<>excluded.metadata_json OR runes.enabled<>excluded.enabled OR runes.protected<>excluded.protected`, v.Key, v.Kind, v.Title, v.Body, string(metadata), boolInt(v.Enabled), boolInt(v.Protected), actor, actor, now, now)
	return err
}
func memory(ctx context.Context, tx *sql.Tx, v model.MemoryPlan, now string) error {
	created := timestamp(v.CreatedAt, now)
	_, err := tx.ExecContext(ctx, `INSERT INTO memories (key,body,created_by,updated_by,created_at,updated_at,revision) VALUES (?,?,?,?,?,?,1) ON CONFLICT(key) DO UPDATE SET body=excluded.body,updated_by=excluded.updated_by,updated_at=excluded.updated_at,revision=memories.revision+1 WHERE memories.body<>excluded.body`, v.Key, v.Body, v.Actor, v.Actor, created, now)
	return err
}
func provenance(ctx context.Context, tx *sql.Tx, kind, key string, revision int64, action string, payload any, now string) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO events (subject_kind,subject_key,revision,action,actor,payload_json,created_at) SELECT ?,?,?,?,?,?,? WHERE NOT EXISTS (SELECT 1 FROM events WHERE subject_kind=? AND subject_key=? AND action=?)`, kind, key, revision, action, actor, string(b), now, kind, key, action)
	return err
}
func timestamp(v *time.Time, fallback string) string {
	if v == nil {
		return fallback
	}
	return v.UTC().Format(time.RFC3339Nano)
}
func timeValue(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.UTC().Format(time.RFC3339Nano)
}
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
