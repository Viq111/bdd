package bddsink

import (
	"bytes"
	"context"
	"database/sql"
	"github.com/viq111/bdd"
	"github.com/viq111/bdd/internal/sqlite"
	"github.com/viq111/bdd/tools/migrate/internal/mapping"
	"github.com/viq111/bdd/tools/migrate/internal/model"
	"github.com/viq111/bdd/tools/migrate/internal/sourcebd"
	"os"
	"path/filepath"
	"testing"
)

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
	if warnings, err := ApplyWithWarnings(ctx, path, "src", plan); err != nil || len(warnings) != 0 {
		t.Fatalf("identical rerun = %v, %v", warnings, err)
	}
	if got := sinkCounts(t, ctx, path); got != before {
		t.Fatalf("identical rerun wrote logical rows: got %#v want %#v", got, before)
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
	changed.Runes = append(changed.Runes, model.RunePlan{Key: "role/collision", Kind: "role", Title: "collision", Enabled: true, Protected: true, Metadata: map[string]string{"legacy_bd_id": "role-collision"}})
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
