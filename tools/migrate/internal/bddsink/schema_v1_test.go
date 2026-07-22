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

func TestApplyInitialImportIsPubliclyReadable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dst := filepath.Join(dir, "bdd.sqlite")
	p := model.Plan{Cards: []model.CardPlan{{ID: "ocp-123", Title: "imported", Status: "open", Type: "task", Labels: []string{"release"}}, {ID: "ocp-124", Title: "blocked", Status: "open", Type: "task"}}, Runes: []model.RunePlan{{Key: "role/programmer", Kind: "role", Title: "Programmer", Body: "body", Enabled: true, Protected: true, Metadata: map[string]string{"legacy_bd_id": "orcha-role"}}}, Memories: []model.MemoryPlan{{Key: "ocp/memory", Body: "state", Actor: "bdd-migration"}}, Notes: []model.NotePlan{{CardID: "ocp-123", SourceKey: "ocp-123/comment/1", SourceKind: "comment", SourceID: "1", Body: "note"}}, Edges: []model.EdgePlan{{ParentID: "ocp-123", ChildID: "ocp-124"}}}
	if err := Apply(ctx, dst, "ocp", p); err != nil {
		t.Fatal(err)
	}
	db, err := bdd.Open(ctx, bdd.OpenOptions{Path: dst})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c, err := db.GetCard(ctx, "ocp-123")
	if err != nil || c.Title != "imported" || len(c.Labels) != 1 || len(c.Children) != 1 || c.Children[0].ID != "ocp-124" {
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
