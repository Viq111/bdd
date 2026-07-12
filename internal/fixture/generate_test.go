package fixture

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func generate(t *testing.T, cards int, seed int64) (string, *Manifest) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.sqlite")
	m, err := Generate(Options{Path: path, Cards: cards, Seed: seed})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return path, m
}

func TestGenerateRowCounts(t *testing.T) {
	path, m := generate(t, 500, 7)
	if m.CardCount != 500 {
		t.Errorf("manifest.CardCount = %d, want 500", m.CardCount)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM cards").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 500 {
		t.Errorf("cards row count = %d, want 500", count)
	}

	for _, table := range []string{"labels", "card_edges", "notes"} {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Errorf("table %s is empty, want a realistic non-zero distribution", table)
		}
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	pathA, mA := generate(t, 300, 99)
	pathB, mB := generate(t, 300, 99)

	dataA, err := os.ReadFile(pathA)
	if err != nil {
		t.Fatal(err)
	}
	dataB, err := os.ReadFile(pathB)
	if err != nil {
		t.Fatal(err)
	}
	if string(dataA) != string(dataB) {
		t.Error("same seed and card count did not produce byte-identical fixtures")
	}
	if mA.ShowID != mB.ShowID || mA.ClaimID != mB.ClaimID {
		t.Error("manifests differ for the same seed and card count")
	}
}

func TestGenerateEdgesAreAcyclic(t *testing.T) {
	path, _ := generate(t, 2000, 3)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// created_at ordering is not guaranteed, but insertion order is: every
	// edge's parent_id was inserted before its child_id, so comparing
	// cards' rowid catches any accidental back-edge.
	rows, err := db.Query(`
		SELECT p.rowid, c.rowid
		FROM card_edges e
		JOIN cards p ON p.id = e.parent_id
		JOIN cards c ON c.id = e.child_id
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	edges := 0
	for rows.Next() {
		var parentRowID, childRowID int64
		if err := rows.Scan(&parentRowID, &childRowID); err != nil {
			t.Fatal(err)
		}
		edges++
		if parentRowID >= childRowID {
			t.Fatalf("edge parent rowid %d >= child rowid %d: not a DAG", parentRowID, childRowID)
		}
	}
	if edges == 0 {
		t.Fatal("no edges generated")
	}
}

func TestGenerateRefusesExistingFile(t *testing.T) {
	path, _ := generate(t, 10, 1)
	if _, err := Generate(Options{Path: path, Cards: 10, Seed: 1}); err == nil {
		t.Fatal("Generate did not fail for an existing path")
	}
}

func TestGenerateNoWALSidecars(t *testing.T) {
	path, _ := generate(t, 10, 1)
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); err == nil {
			t.Errorf("unexpected sidecar file %s%s", path, suffix)
		}
	}
}
