package bddsink

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/viq111/bdd/internal/schema"
)

// checkSchema deliberately pins direct SQL to the bdd schema known at build
// time. bdd currently leaves application_id at SQLite's canonical zero.
func checkSchema(ctx context.Context, db *sql.DB) error {
	var applicationID, version int
	if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&applicationID); err != nil {
		return err
	}
	if applicationID != 0 {
		return fmt.Errorf("bdd migration sink: non-bdd application ID %d", applicationID)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != schema.CurrentVersion() {
		return fmt.Errorf("bdd migration sink: unsupported schema version %d (want %d)", version, schema.CurrentVersion())
	}
	for table, columns := range expectedColumns {
		if err := checkColumns(ctx, db, table, columns); err != nil {
			return err
		}
	}
	return nil
}

var expectedColumns = map[string][]string{
	"cards":  {"id", "title", "worktree", "description", "reproduction", "design", "acceptance", "status", "priority", "card_type", "external_ref", "assignee", "created_by", "dispatchable", "created_at", "updated_at", "started_at", "closed_at", "defer_until", "revision"},
	"labels": {"card_id", "label"}, "card_edges": {"parent_id", "child_id", "created_at", "created_by"}, "notes": {"id", "card_id", "author", "body", "created_at"}, "memories": {"key", "body", "created_by", "updated_by", "created_at", "updated_at", "revision"}, "runes": {"key", "kind", "title", "body", "metadata_json", "enabled", "protected", "created_by", "updated_by", "created_at", "updated_at", "revision"}, "events": {"id", "subject_kind", "subject_key", "revision", "action", "actor", "payload_json", "created_at"},
}

func checkColumns(ctx context.Context, db *sql.DB, table string, want []string) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var d any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &d, &pk); err != nil {
			return err
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range want {
		if !got[name] {
			return fmt.Errorf("bdd migration sink: schema contract: table %s lacks column %s", table, name)
		}
	}
	return nil
}
