# Online snapshot and restore

Owner: Programmer. Why this exists: point-in-time backup/restore needs a
consistent SQLite snapshot, never a copy of the live WAL-mode database —
copying `bdd.sqlite` directly loses recent transactions sitting in its WAL
file. `(*DB).Snapshot` and `Restore` (in the root `bdd` package) give a
single, integrity-checked, standalone snapshot file instead.

## Library API

- `(*DB).Snapshot(ctx, SnapshotOptions) (*SnapshotResult, error)` — produces
  one integrity-checked copy of the live database via `VACUUM INTO`, safe to
  call while other readers and writers hold the database open. Writes to a
  temp file beside the destination, fsyncs, integrity-checks, then
  atomically renames into place. The library itself stays path-agnostic and
  defaults `Output` to `backup.sqlite` alongside the live database; the CLI
  (`bdd snapshot`) resolves and passes an explicit `Output` at the workspace
  root instead (see CLI usage below).
- `Restore(ctx, RestoreOptions) (*RestoreResult, error)` — validates the
  source snapshot's schema compatibility and integrity before touching
  anything, requires exclusive access to an existing target (fails with
  `ErrBusy` if another process has it open), saves the current database to
  a backup unless `SkipBackup` is set, and installs the snapshot atomically.

These are the primitives the CLI wraps as `bdd snapshot [--output <path>]`
and `bdd restore <snapshot.sqlite> --force`.

## CLI usage

```sh
# Write <workspace>/bdd_backup.sqlite (default), or an explicit path:
bdd snapshot
bdd snapshot --output /path/to/backup.sqlite

# Install a snapshot as the workspace database. Like `bdd delete`, the
# destructive install is refused without --force. The current target (if
# any) is backed up first unless it doesn't exist yet.
bdd restore bdd_backup.sqlite --force
```

`bdd prime --full` echoes the recommended `.gitignore` entries below in its
own output, so an agent reading the full prose contract sees them without
having to read this file. The default compact manifest omits this to stay
small.

## Recommended `.gitignore` entries for a bdd workspace

A workspace should ignore the entire `.bdd/` directory — the live, mutable
database, its WAL sidecars, and any in-progress snapshot/restore temp files
all live there, and none of it needs to be tracked:

```gitignore
.bdd/
```

## Default snapshot location

By default, `bdd snapshot` writes to `<workspace>/bdd_backup.sqlite`, outside
`.bdd/`, so the whole `.bdd/` directory can be gitignored without carving out
an exception for the backup file. `Restore` accepts that file (or any other
snapshot path) as its source.
