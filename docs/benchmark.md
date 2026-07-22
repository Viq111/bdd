# Subprocess latency benchmark harness

Owner: QA (bd bdd-hqo2). Why this exists: `bdd` treats warm-start CLI latency
as a first-class requirement (plan section 7), so the harness and its 10k-card
fixture are built in phase 0, ahead of the CLI commands they benchmark, so
every later change can be checked against the budget.

This card builds the *measurement* harness only. Enforcing the section 7
latency targets as pass/fail is a separate, later card (bd bdd-5re7), which
reuses this harness.

## What it measures

For each of the section 7 commands:

- `version` / `help`
- `show <id>`
- `update <id> --claim`
- `ready --limit 100`
- `search <text> --limit 50`

the harness executes the real, compiled `bdd` binary as a subprocess (never
the Go library directly), so the numbers include process start, flag
parsing, and (once implemented) workspace discovery and SQLite open — the
same overhead a real invocation pays.

For each command it reports:

- a **cold** sample: the very first invocation of that command in the run.
- **p50** / **p95** over `-iterations` warm, timed runs, after `-warmup`
  untimed runs so the OS file cache for the fixture database is populated.

A command that has not been implemented yet (anything except `version`/
`help` today) is detected from cmd/bdd's `unknown command` exit path and
reported with `"status": "unimplemented"` instead of being timed or crashing
the harness. Any other non-zero exit is reported as `"status": "error"` with
the command's stderr, so one broken command never aborts the rest of the
run.

## Reference machine assumptions

Subprocess latency is sensitive to the machine it's measured on. Reports
embed a `host` block (OS, arch, CPU count, Go version) so two reports can be
sanity-checked for comparability, but they are **not** normalized across
machines. Treat numbers as comparable only:

- on the same machine (or a CI runner class pinned to the same instance
  type),
- with no other CPU/disk-heavy process running concurrently,
- on local disk (not a network filesystem) — the fixture and workspace are
  staged under a plain temp directory by default.

"Warm filesystem cache" means the OS page cache for the fixture's SQLite
file, not a dropped-and-repopulated cache: this harness cannot flush OS
caches without root, so `-warmup` runs simply exercise the command enough
times that steady-state caching effects have already kicked in before timed
samples are taken. The `cold` sample is a *process* cold-start proxy (first
invocation of the run), not a disk-cold-cache measurement.

## Usage

```sh
# Generate (or regenerate) the 10k-card fixture:
./tools/build.sh --fixture

# Build bdd and run the full benchmark against it, writing a JSON report:
./tools/build.sh --bench
```

`./tools/build.sh --bench` writes `testdata/bench/report.json` and prints a human-readable
summary table to stderr. Fixture and report files are generated artifacts
(gitignored); regenerate them rather than committing them.

Run the pieces directly for more control:

```sh
go run ./cmd/bddfixture -out fixture.sqlite -manifest fixture.manifest.json \
    -cards 10000 -seed 42

go build -o bin/bdd ./cmd/bdd
go run ./cmd/bddbench -binary bin/bdd -manifest fixture.manifest.json \
    -iterations 50 -warmup 5 -out report.json
```

## The fixture

`internal/fixture` generates a bdd-shaped SQLite database directly via SQL —
it does not use the `bdd` library, since the storage layer (bd bdd-8urh) is
not implemented yet. The schema is QA's best-effort reconstruction of plan
section 18 from the frozen public API (bd bdd-4s2w) and the table list
described in bdd-8urh's card. **Once bdd-8urh lands, reconcile this schema
against the real migrations** so fixtures stay openable by the real `bdd`
binary; see the doc comment on `internal/fixture/schema.go` for specifics.

Generation is deterministic: the same `-seed` and `-cards` always produce a
byte-identical file. By default it creates 10,000 cards with:

- types/statuses/priorities drawn from weighted distributions matching the
  six built-in types and statuses,
- 0-4 labels per card from a 24-label pool,
- parent/child edges among ~15% of cards (after the first 100), always
  pointing from an earlier-created card to a later one, so the graph is
  guaranteed acyclic,
- 0-5 notes per card.

A manifest JSON is written alongside the fixture with a card ID good for
`show`, an unclaimed open card ID good for `update --claim`, and a search
token that matches a handful of descriptions, so the harness (or a test)
never needs to query the fixture directly to find realistic arguments.

## CI

`./tools/build.sh --test` (`go build`, `go vet`, `go test ./...`) is safe to run in any
CI job. `./tools/build.sh --bench` is intended for a dedicated, pinned-hardware CI job
(or manual runs) given the reference-machine caveats above; it is not part
of `./tools/build.sh --test`.
