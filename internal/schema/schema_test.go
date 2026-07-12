package schema

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrationsAreContiguousFromOne(t *testing.T) {
	ms := Migrations()
	if len(ms) == 0 {
		t.Fatal("Migrations() returned none")
	}
	for i, m := range ms {
		if m.Version != i+1 {
			t.Fatalf("migration at index %d has version %d, want %d", i, m.Version, i+1)
		}
	}
	if CurrentVersion() != ms[len(ms)-1].Version {
		t.Fatalf("CurrentVersion() = %d, want %d", CurrentVersion(), ms[len(ms)-1].Version)
	}
}

func TestReadVersionOnFreshDatabaseIsZero(t *testing.T) {
	db := openMemDB(t)
	v, err := ReadVersion(context.Background(), db)
	if err != nil {
		t.Fatalf("ReadVersion() error = %v", err)
	}
	if v != 0 {
		t.Fatalf("ReadVersion() = %d, want 0", v)
	}
}

func TestUpgradeReachesCurrentVersion(t *testing.T) {
	db := openMemDB(t)
	ctx := context.Background()

	if err := Upgrade(ctx, db); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}

	v, err := ReadVersion(ctx, db)
	if err != nil {
		t.Fatalf("ReadVersion() error = %v", err)
	}
	if v != CurrentVersion() {
		t.Fatalf("ReadVersion() = %d, want %d", v, CurrentVersion())
	}
}

func TestUpgradeIsIdempotent(t *testing.T) {
	db := openMemDB(t)
	ctx := context.Background()

	if err := Upgrade(ctx, db); err != nil {
		t.Fatalf("first Upgrade() error = %v", err)
	}
	if err := Upgrade(ctx, db); err != nil {
		t.Fatalf("second Upgrade() error = %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM schema_versions").Scan(&n); err != nil {
		t.Fatalf("counting schema_versions: %v", err)
	}
	if n != len(Migrations()) {
		t.Fatalf("schema_versions has %d rows, want %d (no re-application)", n, len(Migrations()))
	}
}

func TestUpgradeSeedsBuiltinStatusesAndTypes(t *testing.T) {
	db := openMemDB(t)
	ctx := context.Background()
	if err := Upgrade(ctx, db); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}

	wantStatuses := map[string]string{
		"open":        "active",
		"in_progress": "wip",
		"blocked":     "frozen",
		"deferred":    "frozen",
		"closed":      "done",
		"wontfix":     "done",
	}
	rows, err := db.QueryContext(ctx, "SELECT name, category FROM status_definitions")
	if err != nil {
		t.Fatalf("querying status_definitions: %v", err)
	}
	got := map[string]string{}
	for rows.Next() {
		var name, category string
		if err := rows.Scan(&name, &category); err != nil {
			t.Fatalf("scanning status row: %v", err)
		}
		got[name] = category
	}
	rows.Close()
	if len(got) != len(wantStatuses) {
		t.Fatalf("got %d statuses, want %d: %v", len(got), len(wantStatuses), got)
	}
	for name, category := range wantStatuses {
		if got[name] != category {
			t.Fatalf("status %q has category %q, want %q", name, got[name], category)
		}
	}

	wantTypes := []string{"bug", "task", "feature", "epic", "decision", "chore"}
	var typeCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM type_definitions").Scan(&typeCount); err != nil {
		t.Fatalf("counting type_definitions: %v", err)
	}
	if typeCount != len(wantTypes) {
		t.Fatalf("got %d types, want %d", typeCount, len(wantTypes))
	}
	for _, name := range wantTypes {
		var exists int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM type_definitions WHERE name = ?", name).Scan(&exists); err != nil {
			t.Fatalf("checking type %q: %v", name, err)
		}
		if exists != 1 {
			t.Fatalf("type %q missing from type_definitions", name)
		}
	}
}

func TestUpgradeAppliesOnlyPendingMigrations(t *testing.T) {
	db := openMemDB(t)
	ctx := context.Background()

	// Simulate a database already at CurrentVersion() by running Upgrade
	// once, then dropping the row in schema_versions to prove a second
	// Upgrade call consults PRAGMA user_version (the fast path) rather than
	// scanning schema_versions to decide what is pending.
	if err := Upgrade(ctx, db); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM schema_versions"); err != nil {
		t.Fatalf("clearing schema_versions: %v", err)
	}

	if err := Upgrade(ctx, db); err != nil {
		t.Fatalf("second Upgrade() error = %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM schema_versions").Scan(&n); err != nil {
		t.Fatalf("counting schema_versions: %v", err)
	}
	if n != 0 {
		t.Fatalf("schema_versions has %d rows, want 0 (Upgrade re-ran already-applied migrations)", n)
	}
}
