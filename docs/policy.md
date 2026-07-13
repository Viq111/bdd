# Benchmark and compatibility policy

Owner: Programmer (bd bdd-4m6x). Why this exists: phase 4 (open-source
hardening) requires stating, in one place, what latency `bdd` promises and
on what machine, and what API/CLI stability it promises before and after a
tagged v1 (plan section 24 phase 4).

## Benchmark policy

Fast agent tool calls are a first-class requirement, not a later
optimization (plan section 7). `bdd` promises the following warm-cache
subprocess latencies, measured with `make bench` (see
[`docs/benchmark.md`](benchmark.md) for the harness) against a workspace of
at least 10,000 cards:

| Operation | p50 target | p95 target |
|---|---:|---:|
| `bdd version`, top-level `bdd help` | 5 ms | 10 ms |
| `bdd show <id>` | 10 ms | 20 ms |
| `bdd update <id> --claim` | 12 ms | 25 ms |
| `bdd ready --limit 100` | 15 ms | 30 ms |
| `bdd search <text> --limit 50` | 20 ms | 40 ms |

### Reference machine

These targets are promised on a documented reference machine, not on
arbitrary hardware: a warm OS filesystem cache for the fixture's SQLite
file, local disk (not a network filesystem), and no other CPU/disk-heavy
process running concurrently. `make bench` embeds a `host` block (OS, arch,
CPU count, Go version) in its report so two runs can be sanity-checked for
comparability, but numbers are **not** normalized across machines — treat a
report as meaningful only against another report from the same machine, or
a CI runner class pinned to the same instance type. A `cold` sample (the
first invocation of a command in the run) is also reported for reference,
but is not held to the warm-cache budget.

### Enforcement

`make bench` is a manual/CI-only target (not part of `make test`), because
it depends on the pinned-hardware caveats above. A missed target is tracked
as an ordinary bug against the offending command, not treated as blocking
every unrelated change — see the project's issue tracker for any
currently-open latency regressions before relying on a specific number in
production.

## Compatibility policy

`bdd` has not yet had a tagged v1 release. Until the first tag:

- The public Go API (exported types and functions in the root `bdd`
  package), the CLI's flags and subcommands, its JSON field names and
  shapes, and its exit codes may all still change without a deprecation
  period.
- Parent/child blocking semantics (section 4 vocabulary: parent, child,
  blocks — never dependency/prerequisite), the sentinel error set in
  `errors.go`, and the five-exit-code scheme are treated as frozen design
  decisions and are not expected to change even pre-v1; everything else is
  open to revision based on review findings.

Starting at the first tagged v1 release, `bdd` follows semantic versioning
against two independently-versioned public contracts:

- **The Go module** (`github.com/viq111/bdd`, the root package): a breaking
  change to an exported type, function signature, sentinel error, or
  documented behavior requires a major version bump. `internal/*` packages
  carry no compatibility promise at any version — they are not part of the
  public API and may change freely.
- **The CLI** (`cmd/bdd`): a breaking change to a subcommand's name, flags,
  positional arguments, JSON field names/shapes, or exit code mapping
  requires a major version bump. Adding a new subcommand, a new optional
  flag, or a new JSON field is a minor-version change. Output formatting of
  the human (non-`--json`) renderer is explicitly **not** covered by this
  guarantee — scripts that need a stable contract should use `--json`.

A security fix may break either contract out of band of this policy; such a
release is documented as a breaking change regardless of version number.
