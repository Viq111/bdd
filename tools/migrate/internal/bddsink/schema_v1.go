package bddsink

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"

	"github.com/viq111/bdd/internal/schema"
)

type columnSpec struct {
	Name, Type  string
	NotNull, PK int
}

// The schema contract is intentionally exhaustive for every table the sink
// reads or writes. FTS shadow tables are included in the table-set check but
// not column-checked: SQLite owns their physical layout.
var schemaV1 = map[string][]columnSpec{
	"schema_versions":    {{"version", "INTEGER", 0, 1}, {"applied_at", "TEXT", 1, 0}},
	"workspace":          {{"singleton", "INTEGER", 0, 1}, {"prefix", "TEXT", 1, 0}, {"created_at", "TEXT", 1, 0}},
	"status_definitions": {{"name", "TEXT", 0, 1}, {"category", "TEXT", 1, 0}, {"built_in", "INTEGER", 1, 0}},
	"type_definitions":   {{"name", "TEXT", 0, 1}, {"built_in", "INTEGER", 1, 0}},
	"cards":              {{"id", "TEXT", 0, 1}, {"title", "TEXT", 1, 0}, {"worktree", "TEXT", 1, 0}, {"description", "TEXT", 1, 0}, {"reproduction", "TEXT", 1, 0}, {"design", "TEXT", 1, 0}, {"acceptance", "TEXT", 1, 0}, {"status", "TEXT", 1, 0}, {"priority", "INTEGER", 1, 0}, {"card_type", "TEXT", 1, 0}, {"external_ref", "TEXT", 1, 0}, {"assignee", "TEXT", 1, 0}, {"created_by", "TEXT", 1, 0}, {"dispatchable", "INTEGER", 1, 0}, {"created_at", "TEXT", 1, 0}, {"updated_at", "TEXT", 1, 0}, {"started_at", "TEXT", 0, 0}, {"closed_at", "TEXT", 0, 0}, {"defer_until", "TEXT", 0, 0}, {"revision", "INTEGER", 1, 0}, {"owner", "TEXT", 1, 0}},
	"labels":             {{"card_id", "TEXT", 1, 1}, {"label", "TEXT", 1, 2}},
	"card_edges":         {{"parent_id", "TEXT", 1, 1}, {"child_id", "TEXT", 1, 2}, {"created_at", "TEXT", 1, 0}, {"created_by", "TEXT", 0, 0}},
	"notes":              {{"id", "INTEGER", 0, 1}, {"card_id", "TEXT", 1, 0}, {"author", "TEXT", 0, 0}, {"body", "TEXT", 1, 0}, {"created_at", "TEXT", 1, 0}},
	"memories":           {{"key", "TEXT", 0, 1}, {"body", "TEXT", 1, 0}, {"created_by", "TEXT", 0, 0}, {"updated_by", "TEXT", 0, 0}, {"created_at", "TEXT", 1, 0}, {"updated_at", "TEXT", 1, 0}, {"revision", "INTEGER", 1, 0}},
	"runes":              {{"key", "TEXT", 0, 1}, {"kind", "TEXT", 1, 0}, {"title", "TEXT", 1, 0}, {"body", "TEXT", 1, 0}, {"metadata_json", "TEXT", 1, 0}, {"enabled", "INTEGER", 1, 0}, {"protected", "INTEGER", 1, 0}, {"created_by", "TEXT", 0, 0}, {"updated_by", "TEXT", 0, 0}, {"created_at", "TEXT", 1, 0}, {"updated_at", "TEXT", 1, 0}, {"revision", "INTEGER", 1, 0}, {"prime", "TEXT", 1, 0}},
	"events":             {{"id", "INTEGER", 0, 1}, {"subject_kind", "TEXT", 1, 0}, {"subject_key", "TEXT", 1, 0}, {"revision", "INTEGER", 1, 0}, {"action", "TEXT", 1, 0}, {"actor", "TEXT", 0, 0}, {"payload_json", "TEXT", 1, 0}, {"created_at", "TEXT", 1, 0}},
	"config":             {{"key", "TEXT", 0, 1}, {"value", "TEXT", 1, 0}, {"updated_at", "TEXT", 1, 0}, {"updated_by", "TEXT", 0, 0}},
	"cards_fts":          {{"id", "", 0, 0}, {"title", "", 0, 0}, {"description", "", 0, 0}, {"reproduction", "", 0, 0}, {"design", "", 0, 0}, {"acceptance", "", 0, 0}, {"external_ref", "", 0, 0}, {"worktree", "", 0, 0}},
	"notes_fts":          {{"card_id", "", 0, 0}, {"body", "", 0, 0}},
}
var expectedTables = []string{"cards", "cards_fts", "cards_fts_config", "cards_fts_data", "cards_fts_docsize", "cards_fts_idx", "card_edges", "config", "events", "labels", "memories", "notes", "notes_fts", "notes_fts_config", "notes_fts_data", "notes_fts_docsize", "notes_fts_idx", "runes", "schema_versions", "status_definitions", "type_definitions", "workspace"}
var expectedIndexes = []string{"idx_card_edges_child", "idx_card_edges_parent", "idx_cards_assignee", "idx_cards_priority_created", "idx_cards_status_category_priority", "idx_cards_updated_at", "idx_cards_worktree", "idx_labels_label", "idx_memories_updated_at", "idx_notes_card_created", "idx_runes_kind_enabled_updated"}

type indexSpec struct {
	Table           string
	Columns         []string
	Unique, Partial bool
}

var indexV1 = map[string]indexSpec{
	"idx_cards_status_category_priority": {"cards", []string{"status", "priority", "created_at"}, false, false},
	"idx_cards_priority_created":         {"cards", []string{"priority", "created_at"}, false, false},
	"idx_cards_assignee":                 {"cards", []string{"assignee"}, false, false}, "idx_cards_updated_at": {"cards", []string{"updated_at"}, false, false}, "idx_cards_worktree": {"cards", []string{"worktree"}, false, false},
	"idx_labels_label": {"labels", []string{"label"}, false, false}, "idx_card_edges_child": {"card_edges", []string{"child_id"}, false, false}, "idx_card_edges_parent": {"card_edges", []string{"parent_id"}, false, false},
	"idx_notes_card_created": {"notes", []string{"card_id", "created_at"}, false, false}, "idx_memories_updated_at": {"memories", []string{"updated_at"}, false, false}, "idx_runes_kind_enabled_updated": {"runes", []string{"kind", "enabled", "updated_at"}, false, false},
}

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
	if err := exactNames(ctx, db, "table", expectedTables); err != nil {
		return fmt.Errorf("bdd migration sink: schema contract tables: %w", err)
	}
	if err := exactNames(ctx, db, "index", expectedIndexes); err != nil {
		return fmt.Errorf("bdd migration sink: schema contract indexes: %w", err)
	}
	for name, want := range indexV1 {
		if err := checkIndex(ctx, db, name, want); err != nil {
			return err
		}
	}
	for table, want := range schemaV1 {
		if err := checkColumns(ctx, db, table, want); err != nil {
			return err
		}
	}
	return nil
}
func checkIndex(ctx context.Context, db *sql.DB, name string, want indexSpec) error {
	rows, err := db.QueryContext(ctx, "PRAGMA index_list("+want.Table+")")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var seq, unique, partial int
		var gotName, origin string
		if err := rows.Scan(&seq, &gotName, &unique, &origin, &partial); err != nil {
			return err
		}
		if gotName != name {
			continue
		}
		found = true
		if (unique != 0) != want.Unique || (partial != 0) != want.Partial || origin != "c" {
			return fmt.Errorf("bdd migration sink: index %s properties mismatch", name)
		}
		break
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("bdd migration sink: missing index %s on %s", name, want.Table)
	}
	rows, err = db.QueryContext(ctx, "PRAGMA index_xinfo("+name+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var seq, cid, desc, key int
		var column sql.NullString
		var coll string
		if err := rows.Scan(&seq, &cid, &column, &desc, &coll, &key); err != nil {
			return err
		}
		if key != 0 {
			got = append(got, column.String)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !reflect.DeepEqual(got, want.Columns) {
		return fmt.Errorf("bdd migration sink: index %s columns got %v want %v", name, got, want.Columns)
	}
	return nil
}
func exactNames(ctx context.Context, db *sql.DB, kind string, want []string) error {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type=? AND name NOT LIKE 'sqlite_%' ORDER BY name`, kind)
	if err != nil {
		return err
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return err
		}
		got = append(got, n)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	copyWant := append([]string(nil), want...)
	sort.Strings(copyWant)
	if fmt.Sprint(got) != fmt.Sprint(copyWant) {
		return fmt.Errorf("got %v, want %v", got, copyWant)
	}
	return nil
}
func checkColumns(ctx context.Context, db *sql.DB, table string, want []columnSpec) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	var got []columnSpec
	for rows.Next() {
		var cid int
		var c columnSpec
		var defaultValue any
		if err := rows.Scan(&cid, &c.Name, &c.Type, &c.NotNull, &defaultValue, &c.PK); err != nil {
			return err
		}
		got = append(got, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		return fmt.Errorf("bdd migration sink: schema contract columns for %s: got %#v, want %#v", table, got, want)
	}
	return nil
}
