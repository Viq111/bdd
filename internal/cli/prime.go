package cli

import (
	"context"
	"fmt"

	"github.com/viq111/bdd"
)

// primeContract is the static, human-readable body of `bdd prime`: the
// workspace contract an agent needs at session start, generated from the
// same commandsReference (internal/cli/cli.go) the top-level --help text
// renders, plus the semantics that don't fit a one-line command summary
// (lifecycle/claim, blocking, and machine-output rules). It never names a
// command commandsReference itself does not list, so it cannot advertise
// something Run's switch does not implement (plan section 19).
const primeContract = `Commands:
` + commandsReference + `
Lifecycle and claim:
  A card's status has a category (active, done, deferred, ...). ` + "`update <id> --claim`" + `
  moves an active-category card to in_progress and assigns it to the calling
  actor; claiming again as the same actor is a no-op, claiming a card held by
  a different actor fails (exit 4), and claiming outside the active category
  fails as an invalid transition (exit 4). ` + "`close`" + `, ` + "`reopen`" + `, ` + "`defer`" + `, and
  ` + "`human`" + ` move a card between statuses; only ` + "`reopen`" + ` can move a card back out
  of a done-category status.

Parent/child blocking:
  Parent/child edges (set via ` + "`create --parent <id>`" + ` and ` + "`update --add-parent/`" + `
  ` + "`--remove-parent/--add-child/--remove-child <id>`" + `) form an acyclic blocking graph:
  a card is not dispatchable while any parent is outside a done-category
  status. ` + "`ready`" + ` lists every dispatchable, unassigned, unclaimed card without
  the ` + "`human`" + ` label; ` + "`ready --explain <id>`" + ` reports exactly why a given card is
  excluded, including which parents are unfinished. ` + "`parents <id>`" + ` and
  ` + "`children <id>`" + ` list a card's edges directly.

Note, memory, rune, and worktree commands:
  ` + "`note <id> [body]`" + ` appends an append-only, task-scoped annotation to one
  card. ` + "`memory set/get/list/search/remove`" + ` manage durable, keyed,
  workspace-scoped memories that survive across cards and sessions — this
  command's own memories section below lists what's currently stored.
  ` + "`rune put/show/list/search/enable/disable/remove/export`" + ` manage rune
  records. A card's worktree is a plain field, not a launched process: set it
  with ` + "`create --worktree <path>`" + ` or ` + "`update --worktree <path>`" + `
  (` + "`--clear-worktree`" + ` to unset).

Machine-output rules:
  ` + "`--json`" + ` emits one JSON object per result (or a streamed JSON array for a
  list-shaped command), never mixed with human-readable text. ` + "`--silent`" + `
  suppresses incidental stderr diagnostics on success and trims stdout to
  the essential identifier. Failures always report on stderr regardless of
  ` + "`--json`" + `/` + "`--silent`" + `, with one of five exit codes: 0 success, 1 other
  failure, 2 usage/validation error, 3 not found, 4 conflict (invalid
  transition, already claimed, cycle, already exists).

Snapshot and restore:
  ` + "`snapshot [--output <path>]`" + ` writes a point-in-time backup to
  .bdd/backup.sqlite by default; commit that file to git. ` + "`restore <file> --force`" + `
  installs a snapshot as the workspace database, backing up the current one
  first. Recommended .gitignore for a bdd workspace:
    .bdd/bdd.sqlite
    .bdd/bdd.sqlite-wal
    .bdd/bdd.sqlite-shm
    .bdd/*.tmp
  (leave .bdd/backup.sqlite tracked — see docs/snapshot-restore.md).
`

// PrimeResult is the JSON result of `bdd prime`. Human output renders
// primeContract plus the memories section instead of this structure
// directly.
type PrimeResult struct {
	Workspace   string         `json:"workspace"`
	Prefix      *string        `json:"prefix"`
	MemoryCount int            `json:"memory_count"`
	MemoryLimit *int           `json:"memory_limit,omitempty"`
	Memories    []MemoryResult `json:"memories"`
}

// runPrime implements `bdd prime [--memory-limit <n>] [--no-memories]`: the
// command agents run at session start to load the workspace contract and
// current memories. It must stay fast and deterministic (plan section 7
// latency discipline) — one DB open, one Memories query, no per-card work.
func runPrime(g GlobalFlags, args []string, s *Streams) int {
	var limitRaw string
	var haveLimit, noMemories bool

	i := 0
	for i < len(args) {
		arg := args[i]
		name, inline, hasInline := cutFlagValue(arg)

		switch name {
		case "--memory-limit":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: prime: %v\n", err)
				return ExitUsage
			}
			limitRaw, haveLimit = val, true
			i += consumed
			continue
		case "--no-memories":
			noMemories = true
			i++
			continue
		}

		s.Errorf("bdd: prime: unknown argument %q\n", arg)
		return ExitUsage
	}
	if haveLimit && noMemories {
		s.Errorf("bdd: prime: cannot combine --memory-limit with --no-memories\n")
		return ExitUsage
	}

	var limit int
	if haveLimit {
		n, err := parseLimit(limitRaw)
		if err != nil {
			s.Errorf("bdd: prime: --memory-limit %v\n", err)
			return ExitUsage
		}
		limit = n
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "prime", s)
	if db == nil {
		return code
	}
	defer db.Close()

	onDisk, current, err := db.SchemaVersions(ctx)
	if err != nil {
		s.Errorf("bdd: prime: %v\n", err)
		return ExitCode(err)
	}
	upgradePending := onDisk < current

	result := PrimeResult{
		Workspace: workspaceDir(db.Path()),
	}
	if onDisk > 0 {
		if prefix, err := db.Prefix(ctx); err == nil {
			result.Prefix = &prefix
		}
	}

	if !noMemories && !upgradePending {
		memories, err := db.Memories(ctx, bdd.MemoryQuery{})
		if err != nil {
			s.Errorf("bdd: prime: %v\n", err)
			return ExitCode(err)
		}
		result.MemoryCount = len(memories)
		if haveLimit {
			result.MemoryLimit = &limit
			if len(memories) > limit {
				memories = memories[:limit]
			}
		}
		result.Memories = make([]MemoryResult, 0, len(memories))
		for _, m := range memories {
			result.Memories = append(result.Memories, toMemoryResult(&m))
		}
	}

	return emitPrime(s, result, upgradePending)
}

func emitPrime(s *Streams, r PrimeResult, upgradePending bool) int {
	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(r); err != nil {
			s.Errorf("bdd: prime: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}

	fmt.Fprintf(s.Stdout, "workspace: %s\n", r.Workspace)
	if r.Prefix != nil {
		fmt.Fprintf(s.Stdout, "prefix:    %s\n", *r.Prefix)
	}
	if upgradePending {
		fmt.Fprintln(s.Stdout, "⚠ schema upgrade pending — run `bdd status --upgrade`")
	}
	fmt.Fprintln(s.Stdout)

	fmt.Fprint(s.Stdout, primeContract)

	fmt.Fprintln(s.Stdout)
	if r.Memories == nil {
		if upgradePending {
			fmt.Fprintln(s.Stdout, "Memories: skipped (schema upgrade pending)")
		} else {
			fmt.Fprintln(s.Stdout, "Memories: skipped (--no-memories)")
		}
		return ExitSuccess
	}
	if r.MemoryLimit != nil && r.MemoryCount > *r.MemoryLimit {
		fmt.Fprintf(s.Stdout, "Memories (%d of %d, --memory-limit %d):\n", len(r.Memories), r.MemoryCount, *r.MemoryLimit)
	} else {
		fmt.Fprintf(s.Stdout, "Memories (%d):\n", r.MemoryCount)
	}
	if len(r.Memories) == 0 {
		fmt.Fprintln(s.Stdout, "  (none)")
		return ExitSuccess
	}
	for _, m := range r.Memories {
		fmt.Fprintf(s.Stdout, "  %s: %s\n", m.Key, firstLine(m.Body))
	}
	return ExitSuccess
}
