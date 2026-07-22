# bdd

`bdd` is a lean, agent-oriented card tracker: a typed Go library over a
single SQLite file, plus a thin, fast, non-interactive CLI (`cmd/bdd`) over
that library. It tracks small units of work ("cards"), durable
workspace-scoped notes ("memories"), and durable named instruction records
("runes") — with no server, no network dependency, and warm-start CLI
latency treated as a first-class requirement.

## Contents

- [Installation](#installation)
- [Quick start](#quick-start)
- [Workspace layout](#workspace-layout)
- [CLI contract](#cli-contract)
- [Parent/child blocking semantics](#parentchild-blocking-semantics)
- [Card types and creation-time required fields](#card-types-and-creation-time-required-fields)
- [Statuses and categories](#statuses-and-categories)
- [Memories](#memories)
- [Runes](#runes)
- [Worktree field](#worktree-field)
- [Git backup and restore](#git-backup-and-restore)
- [Go library](#go-library)
- [More documentation](#more-documentation)

## Installation

```sh
go install github.com/viq111/bdd/cmd/bdd@latest
```

Or build from a clone:

```sh
git clone https://github.com/viq111/bdd
cd bdd
./tools/build.sh   # writes bin/bdd and bin/bdd-migration
```

`bdd` is a single native binary with no runtime dependencies (SQLite is
linked in via `modernc.org/sqlite`, a CGO-free driver).

## Quick start

```sh
bdd init --prefix demo .
DEMO_BUG=$(bdd create "Cache corruption" --type bug \
  --reproduce "Start two writers and interrupt one during compaction" \
  --acceptance "The previous cache remains readable" --silent)

bdd create "Ship the fix" --type task --acceptance "Released" --parent "$DEMO_BUG"

bdd ready
bdd update "$DEMO_BUG" --claim --actor alice
bdd show "$DEMO_BUG"
```

## Workspace layout

`bdd init [--prefix <prefix>] [path]` creates:

```text
<workspace>/.bdd/
  bdd.sqlite
```

All workspace configuration — the ID prefix, custom statuses, custom types,
and memories — lives in that one SQLite file; there is no separate
JSON/YAML config file to parse before opening the database. The CLI starts
at `--workspace`/`-C` (default: the current directory) and walks upward
looking for `.bdd/bdd.sqlite`, stopping at the filesystem root. `--db <path>`
opens an explicit database file instead, taking precedence over discovery.
`bdd status` prints the resolved workspace and database path plus schema
state; `bdd status --upgrade` applies pending schema migrations (a normal
open only compares schema versions and never runs migrations itself).

## CLI contract

### Global flags

| Flag | Meaning |
|---|---|
| `--workspace, -C <dir>` | Resolve the workspace starting from `<dir>` (default: cwd) |
| `--db <path>` | Use this database file instead of workspace discovery |
| `--actor <name>` | Actor recorded against mutations |
| `--json` | Emit machine-readable JSON instead of human output |
| `--silent` | Emit minimal output and suppress incidental diagnostics on success |

Global flags may appear before or after the subcommand, and every flag
accepts both `--flag value` and `--flag=value`. `bdd version`/`--version`
and `bdd help`/`--help` bypass workspace and database resolution entirely —
they work even outside a `bdd` workspace.

The actor recorded against a mutation is resolved with a fixed precedence:
an explicit `--actor` flag, then the `BDD_ACTOR` environment variable, then
`git config user.name`, then the OS username, else empty.

### JSON conventions

Under `--json`, a command that returns a single result emits exactly one
JSON object with no enclosing envelope (`show`, `create`, `update`, `config
get`, `status`, `init`, `prime`, `snapshot`, ...). A command that returns a
plural, list-shaped result streams a single JSON array (`list`, `search`,
`ready`, `types`, `statuses`, `config list`, `parents`, `children`, `label
list`) — an empty result is `[]`, never `null`. JSON and human output are
never mixed in the same invocation. `--silent` trims stdout to the essential
identifier (e.g. `create --silent` prints just the new card's ID) and
suppresses incidental stderr diagnostics on success. Failures always report
on stderr as a plain-text message, regardless of `--json`/`--silent` —
there is no structured JSON error envelope.

```sh
$ bdd --json show demo-3otk3e
{"id":"demo-3otk3e","title":"Cache corruption","type":"bug","status":"open", ...}

$ bdd --json list --status closed
[]
```

### Exit codes

| Code | Meaning |
|---:|---|
| 0 | Success |
| 1 | Other failure (I/O errors, unexpected schema state, and the like) |
| 2 | Usage or validation error: bad flags, missing required fields, malformed arguments |
| 3 | The requested card, note, memory, rune, or database does not exist |
| 4 | Conflict: an illegal state transition, a claim on an already-claimed card, a cycle, or a create-only operation that already exists |

These are the only exit codes a command returns. No TTY prompts are ever
issued; every input comes from flags, positional arguments, `--stdin`, or a
`--*-file` flag.

## Parent/child blocking semantics

`bdd` uses exactly one relationship vocabulary, and it is intentionally
narrow:

- A **parent** must reach a done-category status before its child can become
  ready.
- A **child** is blocked until every one of its parents is done.
- A card may have multiple parents and multiple children; edges form a
  directed acyclic graph.

There are **no** `dependency`, `dependent`, `prerequisite`, `up`, or `down`
aliases anywhere in the CLI or the Go API. This is deliberate: in `bdd`,
parent/child always means execution blocking, never an organizational
container or epic grouping — use labels for grouping instead.

```sh
# cardA is blocked by cardB (equivalently: cardB blocks cardA)
bdd update cardA --add-parent cardB
bdd update cardB --add-child cardA      # the same edge, expressed from the other side

# remove it
bdd update cardA --remove-parent cardB
bdd update cardB --remove-child cardA

# set parents at creation time
bdd create "New child" --parent cardA --parent cardB

# read edges
bdd parents cardA
bdd children cardB
```

Rules: add/remove is idempotent, self-edges are rejected, any addition that
would create a cycle is rejected (`ErrCycle`), every referenced card is
validated before anything is written, and a multi-edge `update` is
all-or-nothing. Closing or deleting a parent is never inferred from a child
operation.

## Card types and creation-time required fields

Card creation is one-shot: every type-specific input must be explicitly
acknowledged at creation time. There is no draft state, `validate` command,
or warning mode.

| Type | Required fields beyond title |
|---|---|
| `bug` | `reproduction`, `acceptance` |
| `task` | `acceptance` |
| `feature` | `acceptance` |
| `epic` | `acceptance` |
| `decision` | `description`, `design` |
| `chore` | none |

Custom types (via `config set types.custom`) carry no extra required-field
rules. Every card always requires a non-empty title and a registered type.

Validation checks **presence**, not content — an explicitly empty value
satisfies the requirement. This is the explicit-empty acknowledgment
pattern: a caller distinguishes "I omitted this field" from "I considered
this field and it's intentionally blank" by passing an empty string rather
than nothing at all:

```sh
$ bdd create "Cache corruption" --type bug
bdd: create: missing required field(s): reproduction, acceptance
bdd: create: explicitly pass --reproduce "" and --acceptance "" to acknowledge

$ bdd create "Cache corruption" --type bug --reproduce "" --acceptance "N/A"
demo-...   # accepted: both fields were explicitly acknowledged, one left blank
```

On failure, stdout stays empty, every missing field is listed in one
response (not just the first), the CLI exits `2`, and nothing is written —
validation runs before the database is even opened. In the Go API this is
expressed with pointer-typed fields on `CreateCard`: `nil` means omitted,
a non-nil pointer to `""` means explicitly empty. `UpdateCard` reuses the
same convention for its own purpose: `nil` leaves a field unchanged, a
non-nil pointer to `""` clears it. Required fields are a creation-time
contract only — later updates may clear or replace any textual field
freely, including changing the card's type.

## Statuses and categories

Built-in statuses, each with a fixed category:

| Status | Category | Ready by status? |
|---|---|---|
| `open` | `active` | yes |
| `in_progress` | `wip` | no |
| `awaiting_review` | `wip` | no |
| `blocked` | `frozen` | no |
| `deferred` | `frozen` | no |
| `closed` | `done` | no |

Categories have fixed semantics: `active` is eligible for `ready` once every
other predicate passes, `wip` and `frozen` are visible but never ready, and
`done` satisfies parent/child blocking edges. A workspace can add custom
statuses (each must declare one of the four categories) and custom types:

```sh
bdd config set status.custom "qa_testing:wip,ready_to_ship:active,on_hold:frozen"
bdd config set types.custom "incident,experiment"
bdd statuses
bdd types
```

A card is **ready** (returned by `bdd ready`) exactly when: its status
category is `active`, it is dispatchable, its assignee is empty, it lacks
the `human` label, every parent has reached a done-category status, and it
matches every requested label filter. Results sort by priority ascending,
then creation time, then ID; `--limit 0` means unlimited. `bdd ready
--explain <id>` reports which of these predicates a specific card fails.

## Memories

A memory is a durable, workspace-scoped, named piece of knowledge that
survives sessions and agent/account rotation — unlike a card `note`, which
is append-only and scoped to one card, a memory is keyed and updatable.

```sh
bdd memory set "Always run the race tests" --key testing-race
bdd memory list          # list every memory
bdd memory search "race" # search key and body, case-insensitively
bdd memory get testing-race
bdd memory remove testing-race
```

Without `--key`, `memory set` derives a readable slug plus a short content
hash and prints the generated key, so repeated calls with identical
untitled content converge on one record instead of piling up duplicates.
Every write or delete is audited. `bdd prime` includes all memories by
default in a compact section at session start; `--memory-limit <n>` and
`--no-memories` control how much context that costs.

## Runes

A rune is a durable, human-keyed record of standing instruction: a role,
policy, prompt, or convention. A rune is **not** a card — it has no claim,
priority, scheduling, blocking, readiness, worktree, or close operation, and
`show`/`list`/`search`/`ready` never return runes; they live in a separate
namespace addressed only through `bdd rune`.

```sh
bdd rune put role/programmer \
  --kind role --title "Programmer" --body-file programmer.md --protected

bdd rune show role/programmer
bdd rune list --kind role
bdd rune search "Go implementation"
bdd rune disable role/programmer
bdd rune enable role/programmer
bdd rune remove role/programmer --force
bdd rune export role/programmer --format markdown
```

Keys follow the `<kind>/<name>` grammar (lowercase; the key's first segment
must equal the rune's kind), so a role can be addressed by a stable name
instead of a random card ID. `put` creates or atomically updates a rune;
`--create-only` rejects an existing key, and `--if-revision <n>` gives
optimistic concurrency (a stale revision fails without writing). `disable`
is the reversible way to retire a rune — there is no close operation — and
a disabled rune stays directly readable via `show`, appearing in
`list`/`search` only with `--all`. Protected runes require `--force` for
any update, enable/disable, or removal, and `remove` leaves a tombstone
audit event behind.

## Worktree field

`Worktree` is a plain, first-class mutable string field on a card — a piece
of metadata, not a launched process or a worktree manager. `bdd` does not
require the path to exist and never rewrites it during restore.

```sh
bdd create "Implement cache" --worktree .worktrees/cache
bdd update bdd-abc --worktree .worktrees/cache-v2
bdd update bdd-abc --clear-worktree
```

Human `bdd show <id>` prints it near the top, immediately after identity,
status, and priority, and annotates it with "(not present locally)" when
the path is set but absent on disk (without treating that as an error).
JSON and the Go API always expose the field as `worktree`. Prefer a path
relative to the workspace root so it stays valid across clones and restored
backups; `search` matches worktree text along with the rest of a card's
fields.

## Git backup and restore

`bdd` never uses `-wal`/`-shm` files as a backup format, and never commits a
live `bdd.sqlite` directly while WAL mode is active — recent committed data
can still be sitting in its WAL sidecar. Instead:

```sh
bdd snapshot                              # writes .bdd/backup.sqlite by default
bdd snapshot --output /path/to/backup.sqlite
bdd restore .bdd/backup.sqlite --force    # like delete, refused without --force
```

`snapshot` produces one integrity-checked, standalone copy via `VACUUM
INTO` — safe to call while other readers/writers hold the database open —
fsyncs it, integrity-checks it, and atomically renames it into place.
`restore` requires exclusive access to the target, validates the source
snapshot's schema compatibility and integrity before touching anything,
backs up the current database first (the Go API can opt out via
`SkipBackup`; the CLI always backs up), and installs the new one
atomically. Full detail, including the default tracked-snapshot convention,
is in [`docs/snapshot-restore.md`](docs/snapshot-restore.md).

Recommended `.gitignore` for a workspace that tracks `.bdd/` in git:

```gitignore
.bdd/bdd.sqlite
.bdd/bdd.sqlite-wal
.bdd/bdd.sqlite-shm
.bdd/*.tmp
```

Commit `.bdd/backup.sqlite` itself — it's the one `.bdd/*` file meant to be
tracked rather than ignored. `bdd prime` echoes these same entries so an
agent priming a session sees them without reading this file.

## Go library

```go
import (
    "context"

    "github.com/viq111/bdd"
)

ctx := context.Background()

db, err := bdd.Open(ctx, bdd.OpenOptions{Workspace: "."})
if err != nil {
    // ...
}
defer db.Close()

acceptance := "Released"
card, err := db.CreateCard(ctx, bdd.CreateCard{
    Title:      "Ship the fix",
    Type:       bdd.CardTypeTask,
    Acceptance: &acceptance,
    CreatedBy:  "alice",
})
```

See the package doc comment (`go doc github.com/viq111/bdd`) and the
runnable `Example*` functions in `example_test.go` for `Open`, `CreateCard`,
`ClaimCard`, `ReadyCards`, `Remember`, `PutRune`, and `Snapshot`. Every
public method accepts a `context.Context`, returned slices are
deterministically ordered, and mutations return the resulting object so
callers never need a second read. Errors support `errors.Is` against the
sentinels in `errors.go` (`ErrNotFound`, `ErrAlreadyExists`,
`ErrInvalidArgument`, `ErrInvalidTransition`, `ErrClaimed`, `ErrCycle`,
`ErrBusy`, `ErrSchemaTooNew`, `ErrSchemaTooOld`); a `*ValidationError`
additionally lists every field that failed validation, not just the first.

## More documentation

- [`docs/snapshot-restore.md`](docs/snapshot-restore.md) — snapshot/restore
  library API and CLI usage in full.
- [`docs/benchmark.md`](docs/benchmark.md) — the subprocess latency
  benchmark harness and 10k-card fixture.
- [`docs/policy.md`](docs/policy.md) — the benchmark policy (promised
  latency, reference machine) and compatibility policy (what API/CLI
  stability is promised pre-v1 and post-v1).
- [`docs/release.md`](docs/release.md) — cross-platform release builds,
  version stamping, and the repeatable procedure for cutting a tagged
  release.
