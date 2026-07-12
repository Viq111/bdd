package schema

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/viq111/bdd/internal/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migration is one numbered schema upgrade.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// migrations holds every embedded migration, sorted ascending by Version.
var migrations = mustLoadMigrations()

func mustLoadMigrations() []Migration {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		panic(fmt.Sprintf("schema: reading embedded migrations: %v", err))
	}

	out := make([]Migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		version, base, err := parseMigrationFilename(name)
		if err != nil {
			panic(fmt.Sprintf("schema: %v", err))
		}
		b, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			panic(fmt.Sprintf("schema: reading migrations/%s: %v", name, err))
		}
		out = append(out, Migration{Version: version, Name: base, SQL: string(b)})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })

	for i, m := range out {
		if m.Version != i+1 {
			panic(fmt.Sprintf("schema: migrations must be numbered contiguously from 1, got version %d at position %d", m.Version, i+1))
		}
	}

	return out
}

// parseMigrationFilename extracts the numeric version prefix from a
// filename of the form "0001_description.sql".
func parseMigrationFilename(name string) (version int, base string, err error) {
	trimmed := strings.TrimSuffix(name, ".sql")
	idx := strings.IndexByte(trimmed, '_')
	if idx <= 0 {
		return 0, "", fmt.Errorf("migration filename %q must start with a numeric prefix (e.g. 0001_initial.sql)", name)
	}
	n, err := strconv.Atoi(trimmed[:idx])
	if err != nil {
		return 0, "", fmt.Errorf("migration filename %q has a non-numeric prefix: %w", name, err)
	}
	return n, trimmed[idx+1:], nil
}

// Migrations returns every embedded migration, ordered ascending by
// version.
func Migrations() []Migration {
	out := make([]Migration, len(migrations))
	copy(out, migrations)
	return out
}

// CurrentVersion is the schema version this build of bdd expects a database
// to be at after every migration has been applied.
func CurrentVersion() int {
	if len(migrations) == 0 {
		return 0
	}
	return migrations[len(migrations)-1].Version
}

// ReadVersion reads the database's schema version via PRAGMA user_version.
// It performs no other database work, per the fast-open budget: normal
// opens must not query schema_versions or otherwise touch the schema.
func ReadVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("schema: reading user_version: %w", err)
	}
	return version, nil
}

// Upgrade applies every migration with a version greater than the
// database's current PRAGMA user_version, in order, each in its own
// transaction that also records the new version (in both PRAGMA
// user_version and the schema_versions table) before committing. Upgrade is
// a no-op if the database is already at CurrentVersion.
func Upgrade(ctx context.Context, db *sql.DB) error {
	current, err := ReadVersion(ctx, db)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if err := applyMigration(ctx, db, m); err != nil {
			return fmt.Errorf("schema: applying migration %d (%s): %w", m.Version, m.Name, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, m Migration) error {
	return sqlite.Retry(ctx, func() error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			return err
		}

		appliedAt := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_versions (version, applied_at) VALUES (?, ?)", m.Version, appliedAt); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.Version)); err != nil {
			return err
		}

		return tx.Commit()
	})
}
