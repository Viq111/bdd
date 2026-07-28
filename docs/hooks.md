# Hooks

Owner: Programmer. Why this exists: consumers like orcha discover card state
changes today by polling (`bd ready --json` every 5s, `bd show <id> --json`
every 5s per live assignment) and rely on the coding agent to create
follow-up work (e.g. a review card) by hand. Hooks let a status or label
write fire an external command synchronously, turning the write itself into
the trigger instead.

Hooks are a **wake signal, not a complete event stream**: a hook fires, but
bdd makes no delivery guarantee (a crashed process, a `kill -9`, a
`bdd restore` mid-flight all skip it silently) and computes no derived state
for the consumer. A hook handler should re-read authoritative state (`bdd
show`, `bdd ready`) rather than trust the payload as the sole source of
truth.

**CLI-only.** Hooks fire from `internal/cli`, not from the root `bdd`
package. A Go program that imports `github.com/viq111/bdd` directly and
calls its mutation methods bypasses hooks entirely; only invocations through
the `bdd` binary fire them. This keeps `exec.Command` out of the library.
orcha only drives cards through the CLI, so this limitation doesn't affect
it — but note it before wiring anything else against the library API
directly.

## `.bdd/hooks.yaml`

Hooks are configured in a `hooks.yaml` file next to the workspace's
`bdd.sqlite`, at `<workspace>/.bdd/hooks.yaml`. A missing file means no
hooks. Presence alone does not activate it — see "Enabling hooks" below.

```yaml
version: 1
hooks:
  - event: status-change
    to_status: [awaiting_review]
    command: ["orcha", "hook", "card-status-change"]
    timeout: 15s

  - event: label-change
    added: [merge-ready]
    issue_type: [task]
    command: ["./scripts/notify.sh"]
```

Top-level keys:

- `version` (required) — must be `1`.
- `hooks` (optional) — a list of hook entries, evaluated in file order.

Each hook entry:

- `event` (required) — `status-change` or `label-change`.
- `command` (required) — a non-empty argv list. Run directly via
  `exec.Command`, never through a shell: no globbing, piping, or `$VAR`
  expansion.
- `timeout` (optional) — a Go duration string (e.g. `10s`, `1m`). Defaults to
  `10s`.
- `issue_type` (optional) — restricts the hook to cards of the listed
  type(s). Omitted matches any type.
- `from_status`, `to_status` (optional, `status-change` only) — restrict by
  the status transition. Omitted matches any status. Each is OR'ed on its
  own values; `from_status` and `to_status` are AND'ed against each other.
- `added`, `removed` (optional, `label-change` only) — restrict by which
  labels the event added or removed. Omitted matches any delta.

A hook entry mixing `from_status`/`to_status` into a `label-change` hook, or
`added`/`removed` into a `status-change` hook, fails to load.

## Enabling hooks

`hooks.yaml` is versioned and can run arbitrary commands, so a fresh clone
never auto-executes it just because the file exists. Hooks only fire when:

1. The `hooks.enabled` config key is set to a truthy value:
   `bdd config set hooks.enabled true`.
2. Neither `--no-hooks` nor `BDD_NO_HOOKS=1` is set for the invocation.
3. The process wasn't itself started by a hook (see "Re-entrancy guard"
   below).

`bdd status` reports whether hooks are present, enabled, and actually active
for the current invocation.

## When hooks fire

Hooks fire from the CLI handlers that can change a card's status or labels:
`bdd create`, `bdd update` (including `--claim`), `bdd close`, `bdd reopen`,
`bdd defer`, `bdd label add`, and `bdd label remove`. Events are derived by
diffing the card's state before the mutation against its state after:

- `bdd create` emits a `status-change` with `from` empty and `to` the
  card's initial status. Labels passed at create time ride along in the
  event's `card.labels` but do not additionally emit a `label-change`.
- `bdd update --status=X --add-label=Y` can emit both events in one
  invocation; `status-change` always fires before `label-change`.
- A mutation that doesn't actually change status or labels (e.g. adding a
  label the card already has, which is idempotent) fires nothing.
- Hooks never fire when the mutation fails. The pre-read used to compute the
  diff happens before the mutation; firing happens strictly after it
  succeeds.

Firing is **synchronous**: the CLI command doesn't return until every
matching hook has run (or timed out). This is intentional — e.g. `bdd update
--status=awaiting_review` should only return once a hook that creates a
review card has actually created it, so the next command an agent runs
already sees it.

Not hooked: `bdd restore` (a wholesale database swap), `bdd delete`, notes,
config, memory, and rune mutations, and any change to assignee, dependency
edges, priority, or another field that isn't a status or label.

## Stdin JSON contract

Each matching hook receives one JSON object on stdin:

```json
{
  "version": 1,
  "event": "status-change",
  "workspace": "/abs/path",
  "database": "/abs/path/.bdd/bdd.sqlite",
  "actor": "viq111",
  "timestamp": "2026-07-27T10:00:00Z",
  "card": {
    "id": "bdd-a1b", "title": "...", "type": "task",
    "status": "awaiting_review", "priority": 2, "assignee": "...",
    "labels": ["..."], "revision": 12
  },
  "status_change": { "from": "in_progress", "to": "awaiting_review" }
}
```

A `label-change` event replaces `status_change` with:

```json
"label_change": { "added": ["merge-ready"], "removed": [] }
```

`card` is always the card's state *after* the mutation.

## Environment variables

Every hook process also gets these variables, alongside stdin:

| Variable | Meaning |
| --- | --- |
| `BDD_HOOK_EVENT` | `status-change` or `label-change` |
| `BDD_HOOK_CARD_ID` | the card's ID |
| `BDD_HOOK_FROM_STATUS` | set for `status-change`, empty otherwise |
| `BDD_HOOK_TO_STATUS` | set for `status-change`, empty otherwise |
| `BDD_HOOK_LABELS_ADDED` | comma-separated, set for `label-change` |
| `BDD_HOOK_LABELS_REMOVED` | comma-separated, set for `label-change` |
| `BDD_WORKSPACE` | absolute workspace root |
| `BDD_DB` | absolute path to `bdd.sqlite` |
| `BDD_HOOK_DEPTH` | `1` — see re-entrancy guard below |

The hook's working directory is the workspace root.

## Re-entrancy guard

A hook process starts with `BDD_HOOK_DEPTH=1` set. Any `bdd` invocation that
observes `BDD_HOOK_DEPTH` already set in its environment fires no hooks of
its own, regardless of `hooks.enabled` or `hooks.yaml`. This means a hook
that itself shells out to `bdd` (e.g. to add a note or flip another card's
status) terminates instead of recursing.

## Failure semantics

Hook failure is **advisory**: a non-zero exit or a timeout is logged to
stderr as

```
bdd: hook <event> [<command>]: <error>
```

and `bdd` still exits `0`. The status or label write already committed by
the time any hook runs, so failing the command's exit code would misreport a
write that succeeded. This mirrors `orcha hook pretooluse`, which always
exits `0` so a dead daemon never blocks the agent.

Hooks matching the same event run sequentially, in the order they appear in
`hooks.yaml`. One hook's failure or timeout doesn't prevent the next
matching hook from running.
