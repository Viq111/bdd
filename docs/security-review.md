# Security review and fuzzing (bd bdd-ifik)

Owner: Programmer. Why this exists: phase 4 open-source hardening requires a
security pass over external inputs before the v1 tag (plan section 24 phase
4). This documents what was reviewed, what fuzz targets now cover it, and
what was fixed versus filed for later.

## Findings

### 1. SQLite DSN query-parameter injection via caller-supplied paths (fixed)

`internal/sqlite.Open` (and the `acquireExclusive` helper in `snapshot.go`,
which opens a `database/sql` connection directly) passed a filesystem path
straight through as the `database/sql` DSN string. modernc.org/sqlite splits
a plain DSN at its first `?` and parses the remainder as connection query
parameters, including `_pragma` (runs an arbitrary `PRAGMA <value>`
statement immediately after opening) and `vfs` (selects an alternate VFS).

Every one of this project's DSN-producing paths ultimately traces back to a
CLI-flag- or library-caller-supplied filesystem path: `--workspace`
discovery, `snapshot --output`, and `restore <source>`/the restore target.
Most of those call sites `os.Stat` the literal path (including any `?`
suffix) before opening it, which incidentally blocks the attack when the
target must already exist — but `bdd init` creates a *new* file, so
`os.Stat` observes `IsNotExist` on the full string and falls through to
`sqlite.Open` with the injected suffix intact. Confirmed exploitable before
the fix, via the now-removed `--db` flag (bd bdd-0wx9):

```
$ bdd --db "/tmp/x.sqlite?_pragma=journal_mode(delete)" init
Initialized bdd workspace at /tmp/x.sqlite?_pragma=journal_mode(delete) ...
$ ls /tmp   # file created as the truncated name, "x.sqlite" — not what
            # os.Stat above just checked and reported nonexistent
```

**Fix**: added `sqlite.ValidatePath`, which rejects any path containing `?`
(the only character modernc.org/sqlite treats specially in a plain DSN, per
`conn.go`'s `strings.IndexRune(dsn, '?')` split). Wired into
`sqlite.Open` (`internal/sqlite/sqlite.go`) and `acquireExclusive`
(`snapshot.go`), the only two places that hand a path to `database/sql`.
Legitimate SQLite filenames have no need for a literal `?`, so this is a
correctness-neutral, contained fix rather than an attempt at lossy
percent-encoding. Covered by `TestOpenRejectsQueryStringInjection`
(`internal/sqlite/sqlite_test.go`).

`internal/fixture/generate.go`'s dev-only fixture generator (used by `make
fixture`, never by an end user or agent) has the same shape of call but is
out of scope: it takes its path from a developer-supplied `-out` flag on a
tool that already assumes a trusted local invoker.

### 2. Unsanitized control characters in human-readable output (fixed)

Card titles, descriptions, notes, assignees, and labels are arbitrary text
supplied by whoever creates or updates a card — including via `--stdin` or
`--description-file`, which accept any bytes. `internal/cli/card_result.go`
printed these fields straight to `os.Stdout` with `fmt.Fprintf("%s", ...)` in
the human-readable renderer (`bdd show`/`bdd list`/`bdd ready`, but not
`--json`, which encoding/json already escapes safely). A card title or note
body containing raw ANSI/terminal escape sequences (ESC, OSC, etc.) would
reach the terminal of whoever ran `show`/`list` unescaped — cursor
manipulation, title-bar spoofing, or worse depending on the terminal
emulator. This is a real risk in an agent-oriented workflow where card
content routinely originates from another (possibly less trusted) agent or
external system, not just the person at the keyboard.

**Fix**: added `sanitizeForTerminal` (`internal/cli/output.go`), which maps
C0 control bytes and DEL (excluding `\n`/`\t`, which are structurally
meaningful in multi-line fields) to the replacement character before a
field reaches any human-readable `Fprintf`. Applied in `renderCard`,
`printTextField`, `renderCardSummaryLine`, the notes block in `emitShow`,
and the `ready --explain` reasons line (`card_list.go`), which interpolates
a card's assignee/status into free text. Covered by
`TestShowSanitizesControlCharsInTitle`
(`internal/cli/card_test.go`), which also asserts the JSON path is
untouched.

### 3. Text fields accept unvalidated encoding, unlike labels (filed, not fixed)

`validateLabels` (`card.go`) rejects a label unless it is valid UTF-8 and
within `MaxLabelBytes`. No equivalent check exists for title, description,
reproduction, design, acceptance, notes, external_ref, or worktree — all of
which can be populated from arbitrary file or stdin bytes
(`--description-file`, `--stdin`) with no encoding validation at all.
Storing invalid UTF-8 in these `TEXT` columns doesn't corrupt SQLite (it has
no column-level encoding constraint), but `encoding/json.Marshal` silently
replaces invalid byte sequences with U+FFFD on the way out through `--json`
output — a caller reading JSON gets quietly mangled data with no error
signal to explain why the round trip didn't preserve their input.

Deciding how to fix this (reject at write time like labels, replace at
write time, or leave storage permissive and only sanitize on the way out)
is a design decision affecting the public `CreateCard`/`UpdateCard`
contract and touches existing test fixtures, so it's filed as a follow-up
rather than fixed inline here: bd bdd-l4v.

### 4. Unbounded stdin/file reads (documented, out of scope)

`--stdin` (create/update/note/memory set) and `--description-file` and
friends read the full input with `io.ReadAll`/`os.ReadFile` with no size
cap. This is a real resource-exhaustion vector for a maliciously large
input, but the task's scope explicitly excludes "DoS-hardening beyond input
validation" — noted here so it isn't mistaken for an oversight, not
actioned.

### 5. Path handling and workspace discovery: no issues found

`discoverDatabase` (`bdd.go`) walks upward from the resolved absolute
directory via repeated `filepath.Dir`, terminating correctly at the
filesystem root (`parent == dir`) with no possibility of an infinite loop.
`Init`/`Open`/`Snapshot`/`Restore` all resolve relative paths through
`filepath.Abs`/`filepath.Join` before use; no unsanitized path
concatenation or traversal pattern was found. `--workspace`,
`snapshot --output`, and `restore`'s source/target paths all funnel through
this same, reviewed set of helpers.

### 6. Snapshot/restore atomicity: no issues found

`snapshotInto` (`snapshot.go`) writes to a reserved temp path beside the
destination via `VACUUM INTO` (a fresh, consistent copy taken without
blocking concurrent readers/writers), integrity-checks it, fsyncs, then
installs it with a single `os.Rename` in the same directory (same
filesystem, atomic). `Restore` stages the source into a private temp file
before it does anything else — specifically so the common case of
`BackupPath` and `Source` defaulting to the same path
(`<workspace>/bdd_backup.sqlite`) can't let the backup step clobber what
was validated — then takes an OS-level exclusive SQLite lock
(`acquireExclusive`, `PRAGMA locking_mode = EXCLUSIVE`) across the backup
and the final rename, so a concurrent opener gets `SQLITE_BUSY`/`ErrBusy`
rather than racing the swap. There is no window in which a partially
written file is live at the target path: the target is either the old,
complete file or the new one, never a partial write, at every point an
observer could look.

### 7. SQL parameterization: no issues found

Every `INSERT`/`UPDATE`/`DELETE`/`SELECT` built from data outside this
package's own literals binds values with `?` placeholders
(`mutation.go`, `edge.go`, `query.go`, `config.go`, `rune.go`, `memory.go`,
`lifecycle.go`). The handful of places that build SQL text with string
concatenation (`query.go`'s `listOrderBy`, `searchMatchCondition`;
`config.go`'s `customDefinitionNames`/`definitionInUse`) only ever splice in
column/table names sourced from hardcoded Go literals or a fixed
`map[string]string` whitelist (`listSortColumns`) — never a value that
traces back to CLI/library-caller input. No string-concatenated SQL built
from external input was found anywhere in the codebase.

## Fuzz targets added

Short, CI-friendly fuzz targets (a few seconds each; see `./tools/build.sh --fuzz-short`)
for every "in scope" area from the task:

- `FuzzParseGlobalFlags`, `FuzzRun` (`internal/cli/cli_fuzz_test.go`) — CLI
  argument parsing, `FuzzRun` driving the entire `Run` entry point (every
  subcommand's flag parser) against an isolated per-iteration workspace.
  `FuzzRun` redirects `os.Stdin` to `/dev/null` for its duration so a
  fuzzed `--stdin` flag can't block the fuzzer on the test binary's
  inherited stdin.
- `FuzzCreateCardDecode`, `FuzzUpdateCardDecode` (`mutation_fuzz_test.go`) —
  decodes arbitrary JSON directly into `CreateCard`/`UpdateCard` and runs
  their validation, explicitly checking that the tri-state
  missing-vs-explicitly-empty distinction (section 10) survives
  `encoding/json`'s decoder: a present `""` key must decode to a non-nil
  pointer, an absent key must decode to nil.
- `FuzzParseStatusCustom`, `FuzzParseTypesCustom` (`config_fuzz_test.go`) —
  the `status.custom`/`types.custom` config grammars.
- `FuzzCycleDetection` (`edge_fuzz_test.go`) — drives random
  `AddParent`/`RemoveParent` sequences over a small fixed card set against
  a real (temp-file) database and independently DFS-walks the resulting
  edge graph after every case to confirm `wouldCreateCycle` (`edge.go`)
  never let a cycle through.

## Running fuzzing locally

Short run (a few seconds per target, safe for CI — see `./tools/build.sh --fuzz-short`):

```
./tools/build.sh --fuzz-short
```

Longer, exploratory runs (minutes to hours) should be run individually and
outside CI:

```
go test -run '^$' -fuzz '^FuzzRun$'                  -fuzztime 5m .
go test -run '^$' -fuzz '^FuzzCreateCardDecode$'      -fuzztime 5m .
go test -run '^$' -fuzz '^FuzzCycleDetection$'        -fuzztime 5m .
```

(swap `.` for `./internal/cli` when targeting `FuzzParseGlobalFlags`/
`FuzzRun`). A discovered failing input is written under
`testdata/fuzz/<FuzzName>/` in the corresponding package and replays
automatically on the next `go test`/`go test -fuzz` run — commit it if the
failure represents a real bug fix's regression test.

`FuzzRun` creates and deletes real files under `t.TempDir()` (workspace
databases, snapshots) as the process's own user for every fuzz execution;
run long local sessions in a container or otherwise disposable environment
rather than directly against a machine you care about, since a fuzzer-found
argument combination that happens to name a path outside the temp
directory (e.g. via `--workspace`) would still be honored like any other CLI
invocation.
