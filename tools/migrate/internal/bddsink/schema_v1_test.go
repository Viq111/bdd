package bddsink

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/viq111/bdd"
	"github.com/viq111/bdd/internal/sqlite"
	"github.com/viq111/bdd/tools/migrate/internal/mapping"
	"github.com/viq111/bdd/tools/migrate/internal/model"
	"github.com/viq111/bdd/tools/migrate/internal/sourcebd"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyRerunReconcilesSourceTimestamps(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bdd.sqlite")
	initial := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	changed := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	plan := model.Plan{Cards: []model.CardPlan{{
		ID: "src-1", Title: "source", Status: "open", Type: "task", CreatedAt: &initial, UpdatedAt: &initial,
	}}}
	if err := Apply(ctx, path, "src", plan); err != nil {
		t.Fatal(err)
	}

	plan.Cards[0].CreatedAt, plan.Cards[0].UpdatedAt = &changed, &changed
	if err := Apply(ctx, path, "src", plan); err != nil {
		t.Fatalf("changed source timestamp rerun: %v", err)
	}
	checkCardTimestamps(t, ctx, path, changed)

	before := sinkCounts(t, ctx, path)
	if err := Apply(ctx, path, "src", plan); err != nil {
		t.Fatalf("identical timestamp rerun: %v", err)
	}
	if got := sinkCounts(t, ctx, path); got != before {
		t.Fatalf("identical timestamp rerun wrote logical rows: got %#v want %#v", got, before)
	}

	raw, err := sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `UPDATE cards SET created_at='2023-01-01T00:00:00Z', updated_at='2023-01-01T00:00:00Z' WHERE id='src-1'`); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, path, "src", plan); err != nil {
		t.Fatalf("timestamp drift reconciliation: %v", err)
	}
	checkCardTimestamps(t, ctx, path, changed)
}

func TestApplyRerunClearsSourceOwnedOptionalTimestamps(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bdd.sqlite")
	plan := model.Plan{Cards: []model.CardPlan{{
		ID: "src-1", Title: "source", Status: "open", Type: "task",
	}}}
	if err := Apply(ctx, path, "src", plan); err != nil {
		t.Fatal(err)
	}

	raw, err := sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot})
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.ExecContext(ctx, `UPDATE cards SET worktree='native-worktree', closed_at='2025-01-01T00:00:00Z', defer_until='2025-02-01T00:00:00Z' WHERE id='src-1'`)
	if closeErr := raw.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	if err := Apply(ctx, path, "src", plan); err != nil {
		t.Fatalf("source NULL timestamp reconciliation: %v", err)
	}
	raw, err = sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot})
	if err != nil {
		t.Fatal(err)
	}
	var closedAt, deferUntil sql.NullString
	var worktree string
	err = raw.QueryRowContext(ctx, `SELECT worktree, closed_at, defer_until FROM cards WHERE id='src-1'`).Scan(&worktree, &closedAt, &deferUntil)
	if closeErr := raw.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if closedAt.Valid || deferUntil.Valid {
		t.Fatalf("source NULL timestamps = closed_at %#v, defer_until %#v; want both NULL", closedAt, deferUntil)
	}
	if worktree != "native-worktree" {
		t.Fatalf("unrelated destination worktree = %q, want native-worktree", worktree)
	}

	before := sinkCounts(t, ctx, path)
	if err := Apply(ctx, path, "src", plan); err != nil {
		t.Fatalf("identical NULL timestamp rerun: %v", err)
	}
	if got := sinkCounts(t, ctx, path); got != before {
		t.Fatalf("identical NULL timestamp rerun wrote logical rows: got %#v want %#v", got, before)
	}
}

func checkCardTimestamps(t *testing.T, ctx context.Context, path string, want time.Time) {
	t.Helper()
	db, err := bdd.Open(ctx, bdd.OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.GetCard(ctx, "src-1")
	if err != nil || !got.CreatedAt.Equal(want) || !got.UpdatedAt.Equal(want) {
		t.Fatalf("GetCard() timestamps = %#v, %v; want %s", got, err, want.Format(time.RFC3339Nano))
	}
}

func TestApplyRerunReconcilesOnlyBeadsManagedRecords(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bdd.sqlite")
	plan := model.Plan{
		Cards: []model.CardPlan{
			{ID: "src-1", Title: "one", Status: "open", Type: "task", Labels: []string{"old"}},
			{ID: "src-2", Title: "two", Status: "open", Type: "task"},
		},
		Notes:    []model.NotePlan{{CardID: "src-1", SourceKey: "src-1/comment/a", SourceKind: "comment", SourceID: "a", Body: "first"}},
		Edges:    []model.EdgePlan{{ParentID: "src-1", ChildID: "src-2"}},
		Runes:    []model.RunePlan{{Key: "role/source", Kind: "role", Title: "Source", Enabled: true, Protected: true, Metadata: map[string]string{"legacy_bd_id": "role-source"}}},
		Memories: []model.MemoryPlan{{Key: "source/memory", Body: "one", Actor: actor}},
	}
	if warnings, err := ApplyWithWarnings(ctx, path, "src", plan); err != nil || len(warnings) != 0 {
		t.Fatalf("initial ApplyWithWarnings() = %v, %v", warnings, err)
	}
	before := sinkCounts(t, ctx, path)
	beforeProjection := logicalProjection(t, ctx, path)
	if warnings, err := ApplyWithWarnings(ctx, path, "src", plan); err != nil || len(warnings) != 0 {
		t.Fatalf("identical rerun = %v, %v", warnings, err)
	}
	if got := sinkCounts(t, ctx, path); got != before {
		t.Fatalf("identical rerun wrote logical rows: got %#v want %#v", got, before)
	}
	if got := logicalProjection(t, ctx, path); !bytes.Equal(got, beforeProjection) {
		t.Fatalf("identical rerun changed logical projection:\n got %s\nwant %s", got, beforeProjection)
	}

	raw, err := sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot})
	if err != nil {
		t.Fatal(err)
	}
	// worktree is not a source field in this plan and therefore must survive a
	// source-authority update to other mapped fields.
	if _, err := raw.ExecContext(ctx, `UPDATE cards SET title='local edit',worktree='native-worktree' WHERE id='src-1'`); err != nil {
		t.Fatal(err)
	}
	if warnings, err := ApplyWithWarnings(ctx, path, "src", plan); err != nil || len(warnings) != 0 {
		t.Fatalf("source-authority rerun = %v, %v", warnings, err)
	}
	check, err := bdd.Open(ctx, bdd.OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	managed, err := check.GetCard(ctx, "src-1")
	check.Close()
	if err != nil || managed.Title != "one" || managed.Worktree != "native-worktree" {
		t.Fatalf("source authority/worktree preservation = %#v, %v", managed, err)
	}
	// The prior source-authority rerun preserved a destination-only worktree.
	// Subsequent identical reruns must not turn that preserved value into a
	// changed source projection and rewrite the card or related logical rows.
	beforePreservedRerun := sinkCounts(t, ctx, path)
	if warnings, err := ApplyWithWarnings(ctx, path, "src", plan); err != nil || len(warnings) != 0 {
		t.Fatalf("first preserved-worktree rerun = %v, %v", warnings, err)
	}
	afterFirstPreservedRerun := sinkCounts(t, ctx, path)
	if afterFirstPreservedRerun != beforePreservedRerun {
		t.Fatalf("first preserved-worktree rerun wrote logical rows: got %#v want %#v", afterFirstPreservedRerun, beforePreservedRerun)
	}
	if warnings, err := ApplyWithWarnings(ctx, path, "src", plan); err != nil || len(warnings) != 0 {
		t.Fatalf("second preserved-worktree rerun = %v, %v", warnings, err)
	}
	if got := sinkCounts(t, ctx, path); got != afterFirstPreservedRerun {
		t.Fatalf("second preserved-worktree rerun wrote logical rows: got %#v want %#v", got, afterFirstPreservedRerun)
	}
	if _, err := raw.ExecContext(ctx, `INSERT INTO cards (id,title,worktree,description,reproduction,design,acceptance,status,priority,card_type,external_ref,assignee,created_by,owner,dispatchable,created_at,updated_at,started_at,closed_at,defer_until,revision) VALUES ('native','native','','','','','','open',2,'task','','','','',1,'2020-01-01T00:00:00Z','2020-01-01T00:00:00Z',NULL,NULL,NULL,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `INSERT INTO card_edges (parent_id,child_id,created_at,created_by) VALUES ('src-1','native','2020-01-01T00:00:00Z','native')`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `INSERT INTO cards (id,title,worktree,description,reproduction,design,acceptance,status,priority,card_type,external_ref,assignee,created_by,owner,dispatchable,created_at,updated_at,started_at,closed_at,defer_until,revision) VALUES ('collision','native','','','','','','open',2,'task','','','','',1,'2020-01-01T00:00:00Z','2020-01-01T00:00:00Z',NULL,NULL,NULL,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `INSERT INTO runes (key,kind,title,body,metadata_json,enabled,protected,created_by,updated_by,created_at,updated_at,revision) VALUES ('role/collision','role','native','','{"legacy_bd_id":"other"}',1,0,'native','native','2020-01-01T00:00:00Z','2020-01-01T00:00:00Z',1)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	changed := plan
	changed.Cards = []model.CardPlan{
		{ID: "collision", Title: "source collision", Status: "open", Type: "task"},
		{ID: "src-1", Title: "one from source", Status: "open", Type: "task", Labels: []string{"new"}},
		{ID: "src-3", Title: "three", Status: "open", Type: "task"},
	}
	changed.Notes = append(changed.Notes, model.NotePlan{CardID: "src-1", SourceKey: "src-1/comment/b", SourceKind: "comment", SourceID: "b", Body: "second"})
	changed.Edges = []model.EdgePlan{{ParentID: "src-1", ChildID: "src-3"}}
	changed.Memories = []model.MemoryPlan{{Key: "source/memory", Body: "two", Actor: actor}}
	// Omitting source records must not delete or rewrite their destination
	// counterparts. Keep snapshots that include their complete rows and card
	// provenance before applying a plan that removes all three.
	removedCard := projection(t, ctx, path, []projectionQuery{
		{"cards", `SELECT quote(id),quote(title),quote(worktree),quote(description),quote(reproduction),quote(design),quote(acceptance),quote(status),quote(priority),quote(card_type),quote(external_ref),quote(assignee),quote(created_by),quote(owner),quote(dispatchable),quote(created_at),quote(updated_at),quote(started_at),quote(closed_at),quote(defer_until),quote(revision) FROM cards WHERE id='src-2' ORDER BY id`},
		{"labels", `SELECT quote(card_id),quote(label) FROM labels WHERE card_id='src-2' ORDER BY card_id,label`},
		{"events", `SELECT quote(id),quote(subject_kind),quote(subject_key),quote(revision),quote(action),quote(actor),quote(payload_json),quote(created_at) FROM events WHERE subject_kind='card' AND subject_key='src-2' ORDER BY id`},
	})
	removedRune := projection(t, ctx, path, []projectionQuery{{"runes", `SELECT quote(key),quote(kind),quote(title),quote(body),quote(metadata_json),quote(enabled),quote(protected),quote(created_by),quote(updated_by),quote(created_at),quote(updated_at),quote(revision) FROM runes WHERE key='role/source' ORDER BY key`}})
	removedMemory := projection(t, ctx, path, []projectionQuery{{"memories", `SELECT quote(key),quote(body),quote(created_by),quote(updated_by),quote(created_at),quote(updated_at),quote(revision) FROM memories WHERE key='source/memory' ORDER BY key`}})
	changed.Runes = []model.RunePlan{{Key: "role/collision", Kind: "role", Title: "collision", Enabled: true, Protected: true, Metadata: map[string]string{"legacy_bd_id": "role-collision"}}}
	changed.Memories = nil
	warnings, err := ApplyWithWarnings(ctx, path, "src", changed)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 || warnings[0].SourceID != "collision" || warnings[1].SourceID != "role-collision" {
		t.Fatalf("collision warnings = %#v", warnings)
	}
	db, err := bdd.Open(ctx, bdd.OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.GetCard(ctx, "src-1")
	if err != nil || got.Title != "one from source" || got.Worktree != "native-worktree" || len(got.Labels) != 1 || got.Labels[0] != "new" {
		t.Fatalf("managed card after rerun = %#v, %v", got, err)
	}
	if _, err := db.GetCard(ctx, "src-2"); err != nil { // source removal never deletes.
		t.Fatal(err)
	}
	if got := projection(t, ctx, path, []projectionQuery{
		{"cards", `SELECT quote(id),quote(title),quote(worktree),quote(description),quote(reproduction),quote(design),quote(acceptance),quote(status),quote(priority),quote(card_type),quote(external_ref),quote(assignee),quote(created_by),quote(owner),quote(dispatchable),quote(created_at),quote(updated_at),quote(started_at),quote(closed_at),quote(defer_until),quote(revision) FROM cards WHERE id='src-2' ORDER BY id`},
		{"labels", `SELECT quote(card_id),quote(label) FROM labels WHERE card_id='src-2' ORDER BY card_id,label`},
		{"events", `SELECT quote(id),quote(subject_kind),quote(subject_key),quote(revision),quote(action),quote(actor),quote(payload_json),quote(created_at) FROM events WHERE subject_kind='card' AND subject_key='src-2' ORDER BY id`},
	}); !bytes.Equal(got, removedCard) {
		t.Fatalf("removed source card changed destination record:\n got %s\nwant %s", got, removedCard)
	}
	if got := projection(t, ctx, path, []projectionQuery{{"runes", `SELECT quote(key),quote(kind),quote(title),quote(body),quote(metadata_json),quote(enabled),quote(protected),quote(created_by),quote(updated_by),quote(created_at),quote(updated_at),quote(revision) FROM runes WHERE key='role/source' ORDER BY key`}}); !bytes.Equal(got, removedRune) {
		t.Fatalf("removed source rune changed destination record:\n got %s\nwant %s", got, removedRune)
	}
	if got := projection(t, ctx, path, []projectionQuery{{"memories", `SELECT quote(key),quote(body),quote(created_by),quote(updated_by),quote(created_at),quote(updated_at),quote(revision) FROM memories WHERE key='source/memory' ORDER BY key`}}); !bytes.Equal(got, removedMemory) {
		t.Fatalf("removed source memory changed destination record:\n got %s\nwant %s", got, removedMemory)
	}
	if native, err := db.GetCard(ctx, "native"); err != nil || native.Title != "native" || len(native.Parents) != 1 || native.Parents[0].ID != "src-1" {
		t.Fatalf("destination-only card/edge changed: %#v, %v", native, err)
	}
	if collision, err := db.GetCard(ctx, "collision"); err != nil || collision.Title != "native" {
		t.Fatalf("destination collision overwritten: %#v, %v", collision, err)
	}
	notes, err := db.Notes(ctx, "src-1")
	if err != nil || len(notes) != 2 {
		t.Fatalf("notes after changed rerun = %#v, %v", notes, err)
	}
	if warnings, err := ApplyWithWarnings(ctx, path, "src", changed); err != nil || len(warnings) != 2 {
		t.Fatalf("changed plan no-op rerun = %v, %v", warnings, err)
	}
}

type counts struct{ Events, Notes, Edges, Revisions int }

func sinkCounts(t *testing.T, ctx context.Context, path string) counts {
	t.Helper()
	db, err := sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got counts
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM events`).Scan(&got.Events); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notes`).Scan(&got.Notes); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM card_edges`).Scan(&got.Edges); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT coalesce(sum(revision),0) FROM cards`).Scan(&got.Revisions); err != nil {
		t.Fatal(err)
	}
	return got
}

// logicalProjection is a byte-stable snapshot of every logical table that a
// migration can affect. It intentionally retains complete rows (not merely
// counts), including individual revisions, provenance payloads, and times.
func logicalProjection(t *testing.T, ctx context.Context, path string) []byte {
	t.Helper()
	return projection(t, ctx, path, []projectionQuery{
		{"workspace", `SELECT quote(singleton),quote(prefix),quote(created_at) FROM workspace ORDER BY singleton`},
		{"status_definitions", `SELECT quote(name),quote(category),quote(built_in) FROM status_definitions ORDER BY name`},
		{"type_definitions", `SELECT quote(name),quote(built_in) FROM type_definitions ORDER BY name`},
		{"cards", `SELECT quote(id),quote(title),quote(worktree),quote(description),quote(reproduction),quote(design),quote(acceptance),quote(status),quote(priority),quote(card_type),quote(external_ref),quote(assignee),quote(created_by),quote(owner),quote(dispatchable),quote(created_at),quote(updated_at),quote(started_at),quote(closed_at),quote(defer_until),quote(revision) FROM cards ORDER BY id`},
		{"labels", `SELECT quote(card_id),quote(label) FROM labels ORDER BY card_id,label`},
		{"card_edges", `SELECT quote(parent_id),quote(child_id),quote(created_at),quote(created_by) FROM card_edges ORDER BY parent_id,child_id`},
		{"notes", `SELECT quote(id),quote(card_id),quote(author),quote(body),quote(created_at) FROM notes ORDER BY id`},
		{"memories", `SELECT quote(key),quote(body),quote(created_by),quote(updated_by),quote(created_at),quote(updated_at),quote(revision) FROM memories ORDER BY key`},
		{"runes", `SELECT quote(key),quote(kind),quote(title),quote(body),quote(metadata_json),quote(enabled),quote(protected),quote(created_by),quote(updated_by),quote(created_at),quote(updated_at),quote(revision) FROM runes ORDER BY key`},
		{"events", `SELECT quote(id),quote(subject_kind),quote(subject_key),quote(revision),quote(action),quote(actor),quote(payload_json),quote(created_at) FROM events ORDER BY id`},
		{"config", `SELECT quote(key),quote(value),quote(updated_at),quote(updated_by) FROM config ORDER BY key`},
	})
}

type projectionQuery struct{ table, query string }

func projection(t *testing.T, ctx context.Context, path string, queries []projectionQuery) []byte {
	t.Helper()
	db, err := sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var out []byte
	for _, q := range queries {
		rows, err := db.QueryContext(ctx, q.query)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(values))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			row := append([]any{q.table}, values...)
			encoded, err := json.Marshal(row)
			if err != nil {
				rows.Close()
				t.Fatal(err)
			}
			out = append(out, encoded...)
			out = append(out, '\n')
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestApplyInitialImportIsPubliclyReadable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dst := filepath.Join(dir, "bdd.sqlite")
	p := model.Plan{Cards: []model.CardPlan{{ID: "ocp-123", Title: "imported", Status: "open", Type: "task", Owner: "source-owner", Labels: []string{"release"}}, {ID: "ocp-124", Title: "blocked", Status: "open", Type: "task"}}, Runes: []model.RunePlan{{Key: "role/programmer", Kind: "role", Title: "Programmer", Body: "body", Enabled: true, Protected: true, Metadata: map[string]string{"legacy_bd_id": "orcha-role"}}}, Memories: []model.MemoryPlan{{Key: "ocp/memory", Body: "state", Actor: "bdd-migration"}}, Notes: []model.NotePlan{{CardID: "ocp-123", SourceKey: "ocp-123/comment/1", SourceKind: "comment", SourceID: "1", Body: "note"}}, Edges: []model.EdgePlan{{ParentID: "ocp-123", ChildID: "ocp-124"}}}
	if err := Apply(ctx, dst, "ocp", p); err != nil {
		t.Fatal(err)
	}
	db, err := bdd.Open(ctx, bdd.OpenOptions{Path: dst})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c, err := db.GetCard(ctx, "ocp-123")
	if err != nil || c.Title != "imported" || c.Owner != "source-owner" || len(c.Labels) != 1 || len(c.Children) != 1 || c.Children[0].ID != "ocp-124" {
		t.Fatalf("card=%#v err=%v", c, err)
	}
	if _, err := db.GetRune(ctx, "role/programmer"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Recall(ctx, "ocp/memory"); err != nil {
		t.Fatal(err)
	}
	notes, err := db.Notes(ctx, "ocp-123")
	if err != nil || len(notes) != 1 || notes[0].Body != "note" {
		t.Fatalf("notes=%#v err=%v", notes, err)
	}
	// Exercise the normal store query surface against rows that were inserted
	// through the migration sink rather than bdd's create path.
	if cards, err := db.ListCards(ctx, bdd.ListOptions{}); err != nil || len(cards) != 2 {
		t.Fatalf("ListCards() = %#v, %v", cards, err)
	}
	if cards, err := db.ReadyCards(ctx, bdd.ReadyOptions{}); err != nil || len(cards) != 1 || cards[0].ID != "ocp-123" {
		t.Fatalf("ReadyCards() = %#v, %v", cards, err)
	}
	if cards, err := db.SearchCards(ctx, bdd.SearchOptions{Query: "imported"}); err != nil || len(cards) != 1 || cards[0].ID != "ocp-123" {
		t.Fatalf("SearchCards() = %#v, %v", cards, err)
	}
	beforeRune, err := db.GetRune(ctx, "role/programmer")
	if err != nil {
		t.Fatal(err)
	}
	beforeMemory, err := db.Recall(ctx, "ocp/memory")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, dst, "ocp", p); err != nil {
		t.Fatal(err)
	}
	db, err = bdd.Open(ctx, bdd.OpenOptions{Path: dst})
	if err != nil {
		t.Fatal(err)
	}
	afterRune, err := db.GetRune(ctx, "role/programmer")
	if err != nil || afterRune.Revision != beforeRune.Revision {
		t.Fatalf("rune after rerun=%#v err=%v", afterRune, err)
	}
	afterMemory, err := db.Recall(ctx, "ocp/memory")
	if err != nil || afterMemory.Revision != beforeMemory.Revision {
		t.Fatalf("memory after rerun=%#v err=%v", afterMemory, err)
	}
}
func TestSchemaContractRejectsDoctoredDatabase(t *testing.T) {
	ctx := context.Background()
	for name, doctor := range map[string]func(*sql.DB) error{
		"missing-column": func(db *sql.DB) error {
			_, err := db.ExecContext(ctx, "ALTER TABLE cards RENAME COLUMN title TO broken_title")
			return err
		},
		"extra-column": func(db *sql.DB) error {
			_, err := db.ExecContext(ctx, "ALTER TABLE cards ADD COLUMN unexpected TEXT")
			return err
		},
		"missing-index": func(db *sql.DB) error { _, err := db.ExecContext(ctx, "DROP INDEX idx_labels_label"); return err },
		"wrong-index-columns": func(db *sql.DB) error {
			if _, err := db.ExecContext(ctx, "DROP INDEX idx_labels_label"); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, "CREATE INDEX idx_labels_label ON labels(card_id)")
			return err
		},
		"wrong-index-unique": func(db *sql.DB) error {
			if _, err := db.ExecContext(ctx, "DROP INDEX idx_labels_label"); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, "CREATE UNIQUE INDEX idx_labels_label ON labels(label)")
			return err
		},
		"wrong-index-predicate": func(db *sql.DB) error {
			if _, err := db.ExecContext(ctx, "DROP INDEX idx_labels_label"); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, "CREATE INDEX idx_labels_label ON labels(label) WHERE label <> ''")
			return err
		},
		"extra-table": func(db *sql.DB) error {
			_, err := db.ExecContext(ctx, "CREATE TABLE unexpected_contract_drift (id INTEGER)")
			return err
		},
		"nullable-title": func(db *sql.DB) error {
			if _, err := db.ExecContext(ctx, "PRAGMA writable_schema = ON"); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, "UPDATE sqlite_master SET sql = replace(sql, 'title        TEXT NOT NULL', 'title        TEXT') WHERE type='table' AND name='cards'"); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, "PRAGMA writable_schema = OFF")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bdd.sqlite")
			db, err := bdd.Init(ctx, bdd.InitOptions{DBPath: path, Prefix: "ocp"})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			raw, err := sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot})
			if err != nil {
				t.Fatal(err)
			}
			if err := doctor(raw); err != nil {
				t.Fatal(err)
			}
			if name == "nullable-title" {
				if err := raw.Close(); err != nil {
					t.Fatal(err)
				}
				raw, err = sqlite.Open(ctx, path, sqlite.Options{Pool: sqlite.PoolOneShot})
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = checkSchema(ctx, raw); err == nil {
				t.Fatal("checkSchema accepted doctored schema")
			}
			raw.Close()
		})
	}
}

func TestSupportedFixturesImportThroughPublicAPI(t *testing.T) {
	ctx := context.Background()
	for _, fixture := range []string{"orcha-bd-1.0.3.jsonl", "ocp-bd-1.0.3.jsonl"} {
		t.Run(fixture, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", fixture))
			if err != nil {
				t.Fatal(err)
			}
			records, err := sourcebd.ParseJSONL(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			plan, err := mapping.Map(records, mapping.Config{})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "bdd.sqlite")
			if err := Apply(ctx, path, "fixture", plan); err != nil {
				t.Fatal(err)
			}
			db, err := bdd.Open(ctx, bdd.OpenOptions{Path: path})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			for _, card := range plan.Cards {
				got, err := db.GetCard(ctx, card.ID)
				if err != nil || got.Title != card.Title || string(got.Status) != card.Status {
					t.Fatalf("card %q = %#v, %v", card.ID, got, err)
				}
			}
			for _, rune := range plan.Runes {
				if _, err := db.GetRune(ctx, rune.Key); err != nil {
					t.Fatal(err)
				}
			}
			for _, memory := range plan.Memories {
				if _, err := db.Recall(ctx, memory.Key); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
