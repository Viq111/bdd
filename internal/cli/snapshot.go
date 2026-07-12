package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/viq111/bdd"
)

// SnapshotResult is the JSON/human result of `bdd snapshot`.
type SnapshotResult struct {
	Path          string `json:"path"`
	SchemaVersion int    `json:"schema_version"`
	CreatedAt     string `json:"created_at"`
}

// runSnapshot implements `bdd snapshot [--output <path>]`, wrapping
// (*bdd.DB).Snapshot: a single, integrity-checked, standalone copy of the
// live database, safe to commit to git (see docs/snapshot-restore.md).
func runSnapshot(g GlobalFlags, args []string, s *Streams) int {
	var output string

	i := 0
	for i < len(args) {
		arg := args[i]
		name, inline, hasInline := cutFlagValue(arg)

		if name == "--output" {
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: snapshot: %v\n", err)
				return ExitUsage
			}
			output = val
			i += consumed
			continue
		}

		s.Errorf("bdd: snapshot: unknown argument %q\n", arg)
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "snapshot", s)
	if db == nil {
		return code
	}
	defer db.Close()

	result, err := db.Snapshot(ctx, bdd.SnapshotOptions{Output: output})
	if err != nil {
		s.Errorf("bdd: snapshot: %v\n", err)
		return ExitCode(err)
	}

	out := SnapshotResult{
		Path:          result.Path,
		SchemaVersion: result.SchemaVersion,
		CreatedAt:     result.CreatedAt.Format(time.RFC3339Nano),
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(out); err != nil {
			s.Errorf("bdd: snapshot: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	if s.Silent {
		fmt.Fprintln(s.Stdout, out.Path)
		return ExitSuccess
	}
	fmt.Fprintf(s.Stdout, "snapshot written to %s (schema version %d)\n", out.Path, out.SchemaVersion)
	fmt.Fprintln(s.Stdout, "commit this file to git as the workspace's point-in-time backup; see the .gitignore entries in `bdd prime` or docs/snapshot-restore.md")
	return ExitSuccess
}

// RestoreResult is the JSON/human result of `bdd restore`.
type RestoreResult struct {
	Path          string `json:"path"`
	BackupPath    string `json:"backup_path,omitempty"`
	SchemaVersion int    `json:"schema_version"`
}

// runRestore implements `bdd restore <snapshot.sqlite> [--force]`, wrapping
// bdd.Restore. Like `bdd delete`, the destructive operation is refused
// without --force: Restore always installs Source as the target database,
// backing up any existing target first.
func runRestore(g GlobalFlags, args []string, s *Streams) int {
	var source string
	var force bool

	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--force" {
			force = true
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			s.Errorf("bdd: restore: unknown flag %q\n", arg)
			return ExitUsage
		}
		if source != "" {
			s.Errorf("bdd: restore: unexpected argument %q\n", arg)
			return ExitUsage
		}
		source = arg
		i++
	}
	if source == "" {
		s.Errorf("bdd: restore: a snapshot file path is required\n")
		return ExitUsage
	}
	if !force {
		s.Errorf("bdd: restore: refusing to restore %s without --force\n", source)
		return ExitUsage
	}

	ctx := context.Background()
	result, err := bdd.Restore(ctx, bdd.RestoreOptions{
		Path:      g.DBPath,
		Workspace: g.Workspace,
		Source:    source,
	})
	if err != nil {
		s.Errorf("bdd: restore: %v\n", err)
		return ExitCode(err)
	}

	out := RestoreResult{
		Path:          result.Path,
		BackupPath:    result.BackupPath,
		SchemaVersion: result.SchemaVersion,
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(out); err != nil {
			s.Errorf("bdd: restore: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	if s.Silent {
		fmt.Fprintln(s.Stdout, out.Path)
		return ExitSuccess
	}
	fmt.Fprintf(s.Stdout, "restored %s from %s (schema version %d)\n", out.Path, source, out.SchemaVersion)
	if out.BackupPath != "" {
		fmt.Fprintf(s.Stdout, "previous database backed up to %s\n", out.BackupPath)
	}
	return ExitSuccess
}
