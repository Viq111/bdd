package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/viq111/bdd"
)

// primeContractText renders the static, human-readable body of `bdd prime
// --full`: the workspace contract an agent needs at session start, built
// from the same commandsReferenceText (internal/cli/cli.go) the top-level
// --help text renders, plus the semantics that don't fit a one-line command
// summary (lifecycle/claim, blocking, and machine-output rules). It never
// names a command commandsReferenceText itself does not list, so it cannot
// advertise something the command tree does not actually implement (plan
// section 19).
func primeContractText() string {
	return `Commands:
` + commandsReferenceText() + `
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
  a card is not ready while any parent is outside a done-category
  status. ` + "`ready`" + ` lists every active-category, unassigned, unclaimed card without
  the ` + "`human`" + ` label; ` + "`ready --explain <id>`" + ` reports exactly why a given card is
  excluded, including which parents are unfinished. ` + "`parents <id>`" + ` and
  ` + "`children <id>`" + ` list a card's edges directly.

Note, memory, rune, and worktree commands:
  ` + "`note <id> [body]`" + ` appends an append-only, task-scoped annotation to one
  card. ` + "`memory create/update/get/list/search/remove`" + ` manage durable, keyed,
  workspace-scoped memories that survive across cards and sessions — this
  command's own memories section below lists what's currently stored.
  ` + "`rune set/get/list/search/enable/disable/remove`" + ` manage rune
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
  <workspace>/bdd_backup.sqlite by default.
  ` + "`restore <file> --force`" + ` installs a snapshot as the workspace database,
  backing up the current one first. Recommended .gitignore for a bdd
  workspace: ignore the entire .bdd/ directory
    .bdd/
  (see docs/snapshot-restore.md for details).
`
}

// primeContractVersion is bumped whenever the shape of the compact `bdd
// prime` manifest (PrimeResult) changes in a way a consuming agent must
// notice: a renamed/removed field, a reordered section, or a changed
// semantic. Purely additive fields do not require a bump.
const primeContractVersion = 3

// primeRequiredBudgetBytes bounds the total size of required-context bodies
// (required runes plus required memories) `bdd prime` will inline.
// Exceeding it fails the command outright (see runPrime) rather than
// silently truncating a mandatory instruction.
const primeRequiredBudgetBytes = 32 * 1024

// primeRules are the compact manifest's non-negotiable invariants: the
// small set of rules that must survive even the most aggressive context
// trimming. Keep this at or under 8 entries.
var primeRules = []string{
	"Discover work via `ready`; it lists every dispatchable, unassigned, unclaimed card without the `human` label.",
	"A card is dispatchable only once every parent is in a done-category status; parent/child edges block children.",
	"`update <id> --claim` claims ownership; claiming a card held by a different actor fails (exit 4), as does claiming outside the active category.",
	"Only `reopen` moves a card back out of a done-category status; never reopen a card implicitly.",
	"Exit codes: 0 success, 1 other failure, 2 usage/validation error, 3 not found, 4 conflict.",
	"`--json` emits one object (or a streamed array); `--silent` trims output to the essential identifier; failures always report on stderr.",
	"Use `bdd show <id>` for a card's full record — there is no `--compact` view.",
	"Runes and memories listed as required below are binding standing instructions; read every one before proceeding.",
}

// PrimeWorkflowStep is one entry of the compact manifest's workflow
// section: the exact command an agent runs to discover, inspect, claim,
// update, or complete work. Argv is a literal argument vector (no shell
// quoting involved) so JSON consumers can exec it directly.
type PrimeWorkflowStep struct {
	Action string   `json:"action"`
	Argv   []string `json:"argv"`
}

var primeWorkflow = []PrimeWorkflowStep{
	{Action: "discover", Argv: []string{"bdd", "ready"}},
	{Action: "inspect", Argv: []string{"bdd", "show", "<id>"}},
	{Action: "claim", Argv: []string{"bdd", "update", "<id>", "--claim"}},
	{Action: "update", Argv: []string{"bdd", "update", "<id>", "--status", "<status>"}},
	{Action: "complete", Argv: []string{"bdd", "close", "<id>"}},
}

// PrimeRequiredEntry is a required-prime rune or memory inlined in full in
// the compact manifest: the whole point of `prime: required` is that a
// caller must not need a follow-up command to read it.
type PrimeRequiredEntry struct {
	Type     string `json:"type"` // "rune" or "memory"
	Key      string `json:"key"`
	Kind     string `json:"kind,omitempty"` // rune kind; absent for memories
	Title    string `json:"title"`          // rune title, or a memory's first line
	Revision int64  `json:"revision"`
	Body     string `json:"body"`
}

// PrimeContextEntry is an optional-prime rune or memory, summarized rather
// than inlined: enough to know it exists and decide whether to fetch it,
// not its full content.
type PrimeContextEntry struct {
	Type     string `json:"type"` // "rune" or "memory"
	Key      string `json:"key"`
	Kind     string `json:"kind,omitempty"` // rune kind; absent for memories
	Title    string `json:"title"`          // rune title, or a memory's first line
	Revision int64  `json:"revision"`
}

// PrimeOmittedRunes reports how many enabled runes prime saw, split by
// prime designation, plus the command to see the ones it left out.
type PrimeOmittedRunes struct {
	Total       int      `json:"total"`
	Required    int      `json:"required"`
	Optional    int      `json:"optional"`
	Never       int      `json:"never"`
	NextCommand []string `json:"next_command"`
}

// PrimeOmittedMemories reports how many memories exist, split by prime
// designation, versus how many optional ones prime actually returned
// (required memories are always returned in full), plus the command to
// see the rest.
type PrimeOmittedMemories struct {
	Total       int      `json:"total"`
	Required    int      `json:"required"`
	Optional    int      `json:"optional"`
	Returned    int      `json:"returned"` // optional entries actually included, after --memory-limit
	NextCommand []string `json:"next_command,omitempty"`
}

// PrimeOmitted is the compact manifest's omission metadata: total and
// returned counts, plus the exact retrieval command for whatever was left
// out, so trimming context never means losing track of what exists.
type PrimeOmitted struct {
	Runes    PrimeOmittedRunes    `json:"runes"`
	Memories PrimeOmittedMemories `json:"memories"`
}

// PrimeResult is the JSON result of the default (compact) `bdd prime`. See
// runPrime for how it's assembled and emitPrime for human rendering.
type PrimeResult struct {
	ContractVersion int     `json:"contract_version"`
	Workspace       string  `json:"workspace"`
	Prefix          *string `json:"prefix,omitempty"`
	SchemaState     string  `json:"schema_state"` // "current" or "upgrade_pending"

	Rules    []string            `json:"rules"`
	Workflow []PrimeWorkflowStep `json:"workflow"`

	RequiredRunes    []PrimeRequiredEntry `json:"required_runes"`
	RequiredMemories []PrimeRequiredEntry `json:"required_memories"`
	OptionalContext  []PrimeContextEntry  `json:"optional_context"`
	Omitted          PrimeOmitted         `json:"omitted"`
}

// PrimeFullResult is the JSON result of `bdd prime --full`: the workspace
// identity plus every current memory, unabridged. Human output renders
// primeContract plus the memories section instead of this structure
// directly.
type PrimeFullResult struct {
	Workspace   string         `json:"workspace"`
	Prefix      *string        `json:"prefix,omitempty"`
	MemoryCount int            `json:"memory_count"`
	MemoryLimit *int           `json:"memory_limit,omitempty"`
	Memories    []MemoryResult `json:"memories"`
}

// runPrime implements `bdd prime [--memory-limit <n>] [--no-memories]
// [--full]`: the command agents run at session start to load the workspace
// contract and current context. By default it prints a compact manifest
// (identity, invariant rules, workflow commands, required-rune bodies, and
// optional-context summaries); `--full` reproduces the previous full prose
// contract instead. It must stay fast and deterministic (plan section 7
// latency discipline).
func runPrime(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) > 0 {
		return reportUnknownArg(s, "prime", args[0])
	}

	fs := cmd.Flags()
	limitRaw, haveLimit := flagString(fs, "memory-limit")
	noMemories := flagBool(fs, "no-memories")
	full := flagBool(fs, "full")

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

	var prefix *string
	if onDisk > 0 {
		if p, err := db.Prefix(ctx); err == nil {
			prefix = &p
		}
	}

	memories, memErr := loadPrimeMemories(ctx, db, noMemories, upgradePending)
	if memErr != nil {
		s.Errorf("bdd: prime: %v\n", memErr)
		return ExitCode(memErr)
	}

	if full {
		return runPrimeFull(s, db.Path(), prefix, upgradePending, memories, haveLimit, limit, noMemories)
	}
	return runPrimeCompact(ctx, db, s, prefix, upgradePending, memories, haveLimit, limit)
}

// loadPrimeMemories fetches every memory unless memories are unreachable
// (a pending schema upgrade); --no-memories is applied by the caller when
// deciding what to show, not here, so omission metadata can still report
// an accurate total.
func loadPrimeMemories(ctx context.Context, db *bdd.DB, noMemories, upgradePending bool) ([]bdd.Memory, error) {
	if upgradePending {
		return nil, nil
	}
	if noMemories {
		return nil, nil
	}
	return db.Memories(ctx, bdd.MemoryQuery{})
}

func runPrimeFull(s *Streams, dbPath string, prefix *string, upgradePending bool, memories []bdd.Memory, haveLimit bool, limit int, noMemories bool) int {
	result := PrimeFullResult{
		Workspace: workspaceDir(dbPath),
		Prefix:    prefix,
	}
	if !noMemories && !upgradePending {
		result.MemoryCount = len(memories)
		shown := memories
		if haveLimit {
			result.MemoryLimit = &limit
			if len(shown) > limit {
				shown = shown[:limit]
			}
		}
		result.Memories = make([]MemoryResult, 0, len(shown))
		for _, m := range shown {
			result.Memories = append(result.Memories, toMemoryResult(&m))
		}
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(result); err != nil {
			s.Errorf("bdd: prime: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}

	fmt.Fprintf(s.Stdout, "workspace: %s\n", result.Workspace)
	if result.Prefix != nil {
		fmt.Fprintf(s.Stdout, "prefix:    %s\n", *result.Prefix)
	}
	if upgradePending {
		fmt.Fprintln(s.Stdout, "⚠ schema upgrade pending — run `bdd status --upgrade`")
	}
	fmt.Fprintln(s.Stdout)

	fmt.Fprint(s.Stdout, primeContractText())

	fmt.Fprintln(s.Stdout)
	if result.Memories == nil {
		if upgradePending {
			fmt.Fprintln(s.Stdout, "Memories: skipped (schema upgrade pending)")
		} else {
			fmt.Fprintln(s.Stdout, "Memories: skipped (--no-memories)")
		}
		return ExitSuccess
	}
	if result.MemoryLimit != nil && result.MemoryCount > *result.MemoryLimit {
		fmt.Fprintf(s.Stdout, "Memories (%d of %d, --memory-limit %d):\n", len(result.Memories), result.MemoryCount, *result.MemoryLimit)
	} else {
		fmt.Fprintf(s.Stdout, "Memories (%d):\n", result.MemoryCount)
	}
	if len(result.Memories) == 0 {
		fmt.Fprintln(s.Stdout, "  (none)")
		return ExitSuccess
	}
	for _, m := range result.Memories {
		fmt.Fprintf(s.Stdout, "  %s: %s\n", m.Key, firstLine(m.Body))
	}
	return ExitSuccess
}

func runPrimeCompact(ctx context.Context, db *bdd.DB, s *Streams, prefix *string, upgradePending bool, memories []bdd.Memory, haveLimit bool, limit int) int {
	result := PrimeResult{
		ContractVersion:  primeContractVersion,
		Workspace:        workspaceDir(db.Path()),
		Prefix:           prefix,
		Rules:            primeRules,
		Workflow:         primeWorkflow,
		RequiredRunes:    []PrimeRequiredEntry{},
		RequiredMemories: []PrimeRequiredEntry{},
		OptionalContext:  []PrimeContextEntry{},
	}
	if upgradePending {
		result.SchemaState = "upgrade_pending"
	} else {
		result.SchemaState = "current"
	}

	var requiredRuneKeys []string
	if !upgradePending {
		summaries, err := db.ListRunes(ctx, bdd.RuneQuery{})
		if err != nil {
			s.Errorf("bdd: prime: %v\n", err)
			return ExitCode(err)
		}

		for _, r := range summaries {
			switch r.Prime {
			case bdd.RunePrimeRequired:
				result.Omitted.Runes.Required++
				requiredRuneKeys = append(requiredRuneKeys, r.Key)
			case bdd.RunePrimeNever:
				result.Omitted.Runes.Never++
			default:
				result.Omitted.Runes.Optional++
				result.OptionalContext = append(result.OptionalContext, PrimeContextEntry{
					Type: "rune", Key: r.Key, Kind: r.Kind, Title: r.Title, Revision: r.Revision,
				})
			}
		}
		result.Omitted.Runes.Total = len(summaries)
		result.Omitted.Runes.NextCommand = []string{"bdd", "rune", "list", "--all"}
		sort.Strings(requiredRuneKeys)
	}

	var requiredMemoryKeys []string
	result.Omitted.Memories.Total = len(memories)
	var optionalMemories []bdd.Memory
	for _, m := range memories {
		if m.Prime == bdd.MemoryPrimeRequired {
			result.Omitted.Memories.Required++
			requiredMemoryKeys = append(requiredMemoryKeys, m.Key)
			continue
		}
		result.Omitted.Memories.Optional++
		optionalMemories = append(optionalMemories, m)
	}
	sort.Strings(requiredMemoryKeys)

	shown := optionalMemories
	if haveLimit && len(shown) > limit {
		shown = shown[:limit]
	}
	for _, m := range shown {
		result.OptionalContext = append(result.OptionalContext, PrimeContextEntry{
			Type: "memory", Key: m.Key, Title: firstLine(m.Body), Revision: m.Revision,
		})
	}
	result.Omitted.Memories.Returned = len(shown)
	if result.Omitted.Memories.Returned < result.Omitted.Memories.Optional {
		result.Omitted.Memories.NextCommand = []string{"bdd", "memory", "list"}
	}

	requiredRunes, requiredMemories, totalBytes, err := fetchRequiredContext(ctx, db, requiredRuneKeys, memories, requiredMemoryKeys)
	if err != nil {
		s.Errorf("bdd: prime: %v\n", err)
		return ExitCode(err)
	}
	if totalBytes > primeRequiredBudgetBytes {
		s.Errorf("bdd: prime: required context bodies exceed the prime budget (%d > %d bytes); fetch them individually:\n", totalBytes, primeRequiredBudgetBytes)
		for _, key := range requiredRuneKeys {
			s.Errorf("  bdd rune get %s\n", key)
		}
		for _, key := range requiredMemoryKeys {
			s.Errorf("  bdd memory get %s\n", key)
		}
		return ExitOther
	}
	result.RequiredRunes = requiredRunes
	result.RequiredMemories = requiredMemories

	return emitPrimeCompact(s, result)
}

// fetchRequiredContext reads the full record for each required-prime rune
// key (in key order) and looks up each required-prime memory key against
// the already-loaded memories slice, returning their combined total body
// size alongside them so the caller can enforce the prime budget before
// committing to inlining any of them.
func fetchRequiredContext(ctx context.Context, db *bdd.DB, requiredRuneKeys []string, memories []bdd.Memory, requiredMemoryKeys []string) ([]PrimeRequiredEntry, []PrimeRequiredEntry, int, error) {
	runes := make([]PrimeRequiredEntry, 0, len(requiredRuneKeys))
	total := 0
	for _, key := range requiredRuneKeys {
		r, err := db.GetRune(ctx, key)
		if err != nil {
			return nil, nil, 0, err
		}
		total += len(r.Body)
		runes = append(runes, PrimeRequiredEntry{
			Type: "rune", Key: r.Key, Kind: r.Kind, Title: r.Title, Revision: r.Revision, Body: r.Body,
		})
	}

	byKey := make(map[string]bdd.Memory, len(memories))
	for _, m := range memories {
		byKey[m.Key] = m
	}
	mems := make([]PrimeRequiredEntry, 0, len(requiredMemoryKeys))
	for _, key := range requiredMemoryKeys {
		m := byKey[key]
		total += len(m.Body)
		mems = append(mems, PrimeRequiredEntry{
			Type: "memory", Key: m.Key, Title: firstLine(m.Body), Revision: m.Revision, Body: m.Body,
		})
	}

	return runes, mems, total, nil
}

func emitPrimeCompact(s *Streams, r PrimeResult) int {
	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(r); err != nil {
			s.Errorf("bdd: prime: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}

	fmt.Fprintf(s.Stdout, "bdd prime contract v%d\n", r.ContractVersion)
	fmt.Fprintf(s.Stdout, "workspace: %s\n", r.Workspace)
	if r.Prefix != nil {
		fmt.Fprintf(s.Stdout, "prefix:    %s\n", *r.Prefix)
	}
	fmt.Fprintf(s.Stdout, "schema:    %s\n", r.SchemaState)
	if r.SchemaState == "upgrade_pending" {
		fmt.Fprintln(s.Stdout, "⚠ schema upgrade pending — run `bdd status --upgrade`; context below is skipped until then")
	}

	fmt.Fprintln(s.Stdout)
	fmt.Fprintln(s.Stdout, "Rules:")
	for _, rule := range r.Rules {
		fmt.Fprintf(s.Stdout, "  - %s\n", rule)
	}

	fmt.Fprintln(s.Stdout)
	fmt.Fprintln(s.Stdout, "Workflow:")
	for _, step := range r.Workflow {
		fmt.Fprintf(s.Stdout, "  %-9s %s\n", step.Action+":", strings.Join(step.Argv, " "))
	}

	fmt.Fprintln(s.Stdout)
	fmt.Fprintln(s.Stdout, "Context:")
	if len(r.RequiredRunes) == 0 && len(r.RequiredMemories) == 0 && len(r.OptionalContext) == 0 {
		fmt.Fprintln(s.Stdout, "  (none)")
	}
	for _, e := range r.RequiredRunes {
		fmt.Fprintf(s.Stdout, "  [required] %s %s (rev %d): %s\n", e.Type, e.Key, e.Revision, e.Title)
		for _, line := range strings.Split(e.Body, "\n") {
			fmt.Fprintf(s.Stdout, "      %s\n", line)
		}
	}
	for _, e := range r.RequiredMemories {
		fmt.Fprintf(s.Stdout, "  [required] %s %s (rev %d): %s\n", e.Type, e.Key, e.Revision, e.Title)
		for _, line := range strings.Split(e.Body, "\n") {
			fmt.Fprintf(s.Stdout, "      %s\n", line)
		}
	}
	for _, e := range r.OptionalContext {
		fmt.Fprintf(s.Stdout, "  [optional] %s %s (rev %d): %s\n", e.Type, e.Key, e.Revision, e.Title)
	}

	fmt.Fprintln(s.Stdout)
	fmt.Fprintln(s.Stdout, "Omitted:")
	fmt.Fprintf(s.Stdout, "  runes:    %d enabled (%d required, %d optional, %d never) — %s\n",
		r.Omitted.Runes.Total, r.Omitted.Runes.Required, r.Omitted.Runes.Optional, r.Omitted.Runes.Never,
		strings.Join(r.Omitted.Runes.NextCommand, " "))
	if r.Omitted.Memories.Returned < r.Omitted.Memories.Optional {
		fmt.Fprintf(s.Stdout, "  memories: %d total (%d required, %d of %d optional shown) — %s\n",
			r.Omitted.Memories.Total, r.Omitted.Memories.Required, r.Omitted.Memories.Returned, r.Omitted.Memories.Optional,
			strings.Join(r.Omitted.Memories.NextCommand, " "))
	} else {
		fmt.Fprintf(s.Stdout, "  memories: %d total (%d required, %d of %d optional shown)\n",
			r.Omitted.Memories.Total, r.Omitted.Memories.Required, r.Omitted.Memories.Returned, r.Omitted.Memories.Optional)
	}

	return ExitSuccess
}
