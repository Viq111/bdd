package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/viq111/bdd"
)

// defaultBackupFilename is the name `bdd snapshot` uses for its default
// output file, written at the workspace root (not inside .bdd/) so the
// entire .bdd/ directory can be gitignored as a unit.
const defaultBackupFilename = "bdd_backup.sqlite"

// SnapshotResult is the JSON/human result of `bdd snapshot`.
type SnapshotResult struct {
	Path          string `json:"path"`
	SchemaVersion int    `json:"schema_version"`
	CreatedAt     string `json:"created_at"`
}

// runSnapshot implements `bdd snapshot [--output <path>]`, wrapping
// (*bdd.DB).Snapshot: a single, integrity-checked, standalone copy of the
// live database (see docs/snapshot-restore.md).
func runSnapshot(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) > 0 {
		return reportUnknownArg(s, "snapshot", args[0])
	}
	output, _ := flagString(cmd.Flags(), "output")

	ctx := context.Background()
	db, code := openDB(ctx, g, "snapshot", s)
	if db == nil {
		return code
	}
	defer db.Close()

	if output == "" {
		output = filepath.Join(workspaceDir(db.Path()), defaultBackupFilename)
	}

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
func runRestore(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: restore: a snapshot file path is required\n")
		return ExitUsage
	}
	source := args[0]
	if len(args) > 1 {
		s.Errorf("bdd: restore: unexpected argument %q\n", args[1])
		return ExitUsage
	}
	force := flagBool(cmd.Flags(), "force")
	if !force {
		s.Errorf("bdd: restore: refusing to restore %s without --force\n", source)
		return ExitUsage
	}

	ctx := context.Background()
	result, err := bdd.Restore(ctx, bdd.RestoreOptions{
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
