# Online snapshot and restore

Owner: Programmer (bd bdd-ab8s). Why this exists: git is for point-in-time
backup/restore of consistent SQLite snapshots, never for live WAL-mode
database sync — committing `bdd.sqlite` directly loses recent transactions
sitting in its WAL file. `(*DB).Snapshot` and `Restore` (in the root `bdd`
package) give a single, integrity-checked, standalone snapshot file that is
safe to commit (plan section 21).

## Library API

- `(*DB).Snapshot(ctx, SnapshotOptions) (*SnapshotResult, error)` — produces
  one integrity-checked copy of the live database via `VACUUM INTO`, safe to
  call while other readers and writers hold the database open. Writes to a
  temp file beside the destination, fsyncs, integrity-checks, then
  atomically renames into place. Defaults `Output` to `backup.sqlite`
  alongside the live database.
- `Restore(ctx, RestoreOptions) (*RestoreResult, error)` — validates the
  source snapshot's schema compatibility and integrity before touching
  anything, requires exclusive access to an existing target (fails with
  `ErrBusy` if another process has it open), saves the current database to
  a backup unless `SkipBackup` is set, and installs the snapshot atomically.

These are the primitives the CLI wraps as `bdd snapshot [--output <path>]`
and `bdd restore <snapshot.sqlite> --force`.

## CLI usage

```sh
# Write .bdd/backup.sqlite (default), or an explicit path:
bdd snapshot
bdd snapshot --output /path/to/backup.sqlite

# Install a snapshot as the workspace database. Like `bdd delete`, the
# destructive install is refused without --force. The current target (if
# any) is backed up first unless it doesn't exist yet.
bdd restore .bdd/backup.sqlite --force
```

`bdd prime` (bd bdd-0zmi) echoes the recommended `.gitignore` entries below
in its own output, so an agent priming a session sees them without having
to read this file.

## Recommended `.gitignore` entries for a bdd workspace

A workspace tracking its `.bdd/` directory in git should ignore the live,
mutable database and its WAL sidecars, and any in-progress snapshot/restore
temp files, while still tracking the periodic snapshot itself:

```gitignore
.bdd/bdd.sqlite
.bdd/bdd.sqlite-wal
.bdd/bdd.sqlite-shm
.bdd/*.tmp
```

## Default tracked snapshot convention

By default, `Snapshot` writes to `.bdd/backup.sqlite` and `Restore` backs up
the current database to the same path before installing a new one. Commit
`.bdd/backup.sqlite` to git as the workspace's point-in-time backup; it is
the one `.bdd/*` file meant to be tracked rather than ignored.
