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
		"open":            "active",
		"in_progress":     "wip",
		"awaiting_review": "wip",
		"blocked":         "frozen",
		"deferred":        "frozen",
		"closed":          "done",
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

// legacySchemaV1SQL is the 0001_initial.sql seed as it shipped before bd
// bdd-zj9 fixed it: built_in wontfix instead of awaiting_review. Workspaces
// created by that build ran exactly this SQL and are now stuck at schema
// version 1 (or 2) with the wrong built-in status set, since a migration
// that already ran is never re-applied (bd bdd-3wx).
const legacySchemaV1SQL = `
CREATE TABLE schema_versions (
  version    INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE workspace (
  singleton  INTEGER PRIMARY KEY CHECK (singleton = 1),
  prefix     TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE status_definitions (
  name     TEXT PRIMARY KEY,
  category TEXT NOT NULL,
  built_in INTEGER NOT NULL
);

CREATE TABLE type_definitions (
  name     TEXT PRIMARY KEY,
  built_in INTEGER NOT NULL
);

CREATE TABLE cards (
  id           TEXT PRIMARY KEY,
  title        TEXT NOT NULL,
  worktree     TEXT NOT NULL DEFAULT '',
  description  TEXT NOT NULL DEFAULT '',
  reproduction TEXT NOT NULL DEFAULT '',
  design       TEXT NOT NULL DEFAULT '',
  acceptance   TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL REFERENCES status_definitions(name),
  priority     INTEGER NOT NULL DEFAULT 2 CHECK (priority BETWEEN 0 AND 2147483647),
  card_type    TEXT NOT NULL REFERENCES type_definitions(name),
  external_ref TEXT NOT NULL DEFAULT '',
  assignee     TEXT NOT NULL DEFAULT '',
  created_by   TEXT NOT NULL DEFAULT '',
  dispatchable INTEGER NOT NULL DEFAULT 1,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  started_at   TEXT,
  closed_at    TEXT,
  defer_until  TEXT,
  revision     INTEGER NOT NULL DEFAULT 1
);

INSERT INTO status_definitions (name, category, built_in) VALUES
  ('open',        'active', 1),
  ('in_progress', 'wip',    1),
  ('blocked',     'frozen', 1),
  ('deferred',    'frozen', 1),
  ('closed',      'done',   1),
  ('wontfix',     'done',   1);

INSERT INTO type_definitions (name, built_in) VALUES
  ('bug', 1),
  ('task', 1),
  ('feature', 1),
  ('epic', 1),
  ('decision', 1),
  ('chore', 1);
`

// seedLegacyV1 sets up db as a workspace initialized before bd bdd-zj9 and
// already upgraded to schema version 1, bypassing the current (fixed)
// migrations entirely.
func seedLegacyV1(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, legacySchemaV1SQL); err != nil {
		t.Fatalf("seeding legacy schema version 1: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO schema_versions (version, applied_at) VALUES (1, '2020-01-01T00:00:00Z')"); err != nil {
		t.Fatalf("recording legacy schema_versions row: %v", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 1"); err != nil {
		t.Fatalf("setting legacy user_version: %v", err)
	}
}

func TestUpgradeFromLegacyV1MigratesWontfixToAwaitingReview(t *testing.T) {
	db := openMemDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enabling foreign_keys: %v", err)
	}
	seedLegacyV1(t, ctx, db)

	if err := Upgrade(ctx, db); err != nil {
		t.Fatalf("Upgrade() from legacy version 1 error = %v", err)
	}

	rows, err := db.QueryContext(ctx, "SELECT name, category, built_in FROM status_definitions")
	if err != nil {
		t.Fatalf("querying status_definitions: %v", err)
	}
	type def struct {
		category string
		builtIn  int
	}
	got := map[string]def{}
	for rows.Next() {
		var name, category string
		var builtIn int
		if err := rows.Scan(&name, &category, &builtIn); err != nil {
			t.Fatalf("scanning status row: %v", err)
		}
		got[name] = def{category, builtIn}
	}
	rows.Close()

	if _, ok := got["wontfix"]; ok {
		t.Fatalf("status_definitions still has wontfix after Upgrade(): %v", got)
	}
	ar, ok := got["awaiting_review"]
	if !ok {
		t.Fatalf("status_definitions missing awaiting_review after Upgrade(): %v", got)
	}
	if ar.category != "wip" || ar.builtIn != 1 {
		t.Fatalf("awaiting_review = %+v, want category=wip built_in=1", ar)
	}

	v, err := ReadVersion(ctx, db)
	if err != nil {
		t.Fatalf("ReadVersion() error = %v", err)
	}
	if v != CurrentVersion() {
		t.Fatalf("ReadVersion() = %d, want %d", v, CurrentVersion())
	}
}

func TestUpgradeFromLegacyV1PreservesWontfixStillInUse(t *testing.T) {
	db := openMemDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enabling foreign_keys: %v", err)
	}
	seedLegacyV1(t, ctx, db)

	now := "2020-01-01T00:00:00Z"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO cards (id, title, status, card_type, created_at, updated_at, closed_at)
		VALUES ('qa-1', 'legacy wontfix card', 'wontfix', 'bug', ?, ?, ?)`, now, now, now); err != nil {
		t.Fatalf("inserting legacy wontfix card: %v", err)
	}

	if err := Upgrade(ctx, db); err != nil {
		t.Fatalf("Upgrade() from legacy version 1 error = %v", err)
	}

	var category string
	var builtIn int
	if err := db.QueryRowContext(ctx, "SELECT category, built_in FROM status_definitions WHERE name = 'wontfix'").Scan(&category, &builtIn); err != nil {
		t.Fatalf("querying wontfix status_definitions row: %v", err)
	}
	if category != "done" || builtIn != 0 {
		t.Fatalf("wontfix = (category=%q, built_in=%d), want (done, 0) so the in-use card keeps a valid status", category, builtIn)
	}

	var cardStatus string
	if err := db.QueryRowContext(ctx, "SELECT status FROM cards WHERE id = 'qa-1'").Scan(&cardStatus); err != nil {
		t.Fatalf("querying card status: %v", err)
	}
	if cardStatus != "wontfix" {
		t.Fatalf("card status = %q, want wontfix (untouched)", cardStatus)
	}
}

// TestUpgradeFromV2PreservesCustomWontfixStatus covers a workspace that
// already ran the fixed 0001/0002 migrations (so its built-in set is
// awaiting_review, not wontfix) but separately defined its own custom
// wontfix status. Migration 3's UPDATE/DELETE must key off built_in = 1 so
// it only ever touches the retired legacy built-in definition, never a
// user-defined status that happens to share the name.
func TestUpgradeFromV2PreservesCustomWontfixStatus(t *testing.T) {
	db := openMemDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enabling foreign_keys: %v", err)
	}

	ms := Migrations()
	for _, m := range ms {
		if m.Version > 2 {
			break
		}
		if _, err := db.ExecContext(ctx, m.SQL); err != nil {
			t.Fatalf("applying migration %d: %v", m.Version, err)
		}
	}
	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 2"); err != nil {
		t.Fatalf("setting user_version to 2: %v", err)
	}

	if _, err := db.ExecContext(ctx, "INSERT INTO status_definitions (name, category, built_in) VALUES ('wontfix', 'done', 0)"); err != nil {
		t.Fatalf("inserting custom wontfix status: %v", err)
	}

	if err := Upgrade(ctx, db); err != nil {
		t.Fatalf("Upgrade() from version 2 error = %v", err)
	}

	var category string
	var builtIn int
	if err := db.QueryRowContext(ctx, "SELECT category, built_in FROM status_definitions WHERE name = 'wontfix'").Scan(&category, &builtIn); err != nil {
		t.Fatalf("querying wontfix status_definitions row: %v", err)
	}
	if category != "done" || builtIn != 0 {
		t.Fatalf("wontfix = (category=%q, built_in=%d), want (done, 0): custom status must survive migration 3 untouched", category, builtIn)
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
