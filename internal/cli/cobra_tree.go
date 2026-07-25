package cli

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// exitError carries a handler's intended exit code through cobra's
// error-returning RunE convention. Run unwraps it after Execute(); every
// other error (a cobra/pflag structural error this package didn't
// translate itself) maps to ExitUsage instead, since those are all
// usage-shaped mistakes cobra itself detected.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("bdd: exit %d", e.code) }

// CmdFunc is a bdd subcommand handler. cmd is the matched, already-flag-parsed
// cobra.Command: handlers read their business flags from cmd.Flags() (typed
// Get*, plus Changed() to preserve the omitted-vs-explicit-empty
// distinction) and their positional arguments from args. cmdName, the
// dotted-free command path used throughout error and result output (e.g.
// "rune set"), is commandName(cmd).
type CmdFunc func(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int

// commandName derives a leaf or group's error/result-message name from its
// position in the command tree ("rune set", "config get", "create", ...),
// the same strings every handler used to hardcode by hand. Deriving it from
// cmd.CommandPath() means the tree definition in buildCommands is the only
// place these names are spelled out.
func commandName(cmd *cobra.Command) string {
	return strings.TrimPrefix(cmd.CommandPath(), "bdd ")
}

// legacyRunE adapts a CmdFunc to cobra's RunE signature: a non-success exit
// code is carried out via exitError rather than by returning a formatted
// error (the handler has already written its own diagnostic to stderr).
func legacyRunE(handler CmdFunc, global GlobalFlags, streams *Streams) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if code := handler(global, cmd, args, streams); code != ExitSuccess {
			return exitError{code}
		}
		return nil
	}
}

var (
	reUnknownFlag      = regexp.MustCompile(`^unknown flag: (--?\S+)$`)
	reUnknownShorthand = regexp.MustCompile(`^unknown shorthand flag: '.' in (-\S+)$`)
)

// flagErrorFunc adapts a pflag parse error to the CLI's established wording
// ("bdd: <cmd>: unknown flag %q") by locating, in rawArgs, the original
// token pflag rejected — preserving any inline "--flag=value" form the
// bare flag name pflag reports lost. rawArgs is the full post-global-flag
// argument list for the whole invocation (see buildRoot): since subcommand
// names never start with "-", scanning it in full is equivalent to scanning
// just the matched leaf's own arguments, without needing to re-derive that
// slice here.
//
// A missing-value error (e.g. "bdd create --title" with nothing after it)
// is adapted the same way, matching the wording ParseGlobalFlags has always
// used for the four global flags ("bdd: flag --title requires a value"; see
// flags.go) rather than pflag's own "flag needs an argument: --title". No
// bdd flag has a shorthand (global -C is stripped before cobra ever parses,
// see registerGlobalFlags), so only the long-form message needs handling.
func flagErrorFunc(streams *Streams, rawArgs []string) func(*cobra.Command, error) error {
	return func(cmd *cobra.Command, err error) error {
		cmdName := commandName(cmd)

		var valueRequired *pflag.ValueRequiredError
		if errors.As(err, &valueRequired) {
			streams.Errorf("bdd: %s: bdd: flag --%s requires a value\n", cmdName, valueRequired.GetSpecifiedName())
			return exitError{ExitUsage}
		}

		name := ""
		if m := reUnknownFlag.FindStringSubmatch(err.Error()); m != nil {
			name = m[1]
		} else if m := reUnknownShorthand.FindStringSubmatch(err.Error()); m != nil {
			name = m[1]
		}

		if name != "" {
			for _, a := range rawArgs {
				if n, _, _ := cutFlagValue(a); n == name {
					streams.Errorf("bdd: %s: unknown flag %q\n", cmdName, a)
					return exitError{ExitUsage}
				}
			}
		}
		streams.Errorf("bdd: %s: %v\n", cmdName, err)
		return exitError{ExitUsage}
	}
}

// newLeaf builds a cobra.Command for a command with no subcommands of its
// own. Flag parsing is enabled: configureFlags (when non-nil) registers the
// command's real business flags on the same FlagSet cobra parses with, so
// there is exactly one definition of each command's flags, used for both
// parsing and generated help/usage text.
func newLeaf(use, short, long, example string, handler CmdFunc, global GlobalFlags, streams *Streams, rawArgs []string, configureFlags func(*pflag.FlagSet)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           use,
		Short:         short,
		Long:          long,
		Example:       example,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          legacyRunE(handler, global, streams),
	}
	if configureFlags != nil {
		configureFlags(cmd.Flags())
	}
	cmd.SetFlagErrorFunc(flagErrorFunc(streams, rawArgs))
	return cmd
}

// newGroup builds a cobra.Command for a command with subcommands (config,
// rune, label, memory). A group takes no business flags of its own, so real
// pflag parsing safely gives it a genuine cobra "unknown flag" error and
// automatic -h handling for free. fallback reproduces the pre-migration
// "missing subcommand"/"unknown subcommand" behavior and only runs when no
// child matches.
func newGroup(use, short, long, example string, fallback CmdFunc, global GlobalFlags, streams *Streams, rawArgs []string, children ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:           use,
		Short:         short,
		Long:          long,
		Example:       example,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          legacyRunE(fallback, global, streams),
	}
	cmd.SetFlagErrorFunc(flagErrorFunc(streams, rawArgs))
	cmd.AddCommand(children...)
	return cmd
}

// registerGlobalFlags declares the four flags shared by every bdd
// subcommand (see GlobalFlags in flags.go) on fs purely so cobra's
// generated help text lists them; the values are never read back through
// pflag. Run's pre-cobra ParseGlobalFlags pass (internal/cli/flags.go)
// already strips every occurrence of these flags out of args, wherever
// they appear, before any cobra command sees its argument list, so
// registering them here cannot double-parse or conflict with that pass.
func registerGlobalFlags(fs *pflag.FlagSet) {
	fs.StringP("workspace", "C", "", "resolve the workspace starting from <dir> (default: cwd)")
	fs.String("actor", "", "actor recorded against mutations (see BDD_ACTOR)")
	fs.Bool("json", false, "emit machine-readable JSON instead of human output")
	fs.Bool("silent", false, "emit minimal output and suppress incidental diagnostics")
}

// buildRoot constructs the full cobra command tree rooted at a synthetic
// "bdd" command that is never itself executed (Run already handles the
// no-args and version/help fast paths before reaching cobra). Its only
// purposes are to carry the four global flags as persistent flags, so
// every subcommand's generated help lists them under "Global Flags:", and
// to give cobra's own Find() a real tree to walk for unknown-command and
// unknown-flag errors. It is built fresh on every Run call (never a
// package-level var) so it never carries stale global/streams closures or
// output writers across calls. rawArgs is the exact slice about to be
// handed to root.Execute() (see Run), threaded down to every leaf/group so
// their FlagErrorFunc can recover the original, inline-value-preserving
// flag token cobra's own parse error strips out.
func buildRoot(global GlobalFlags, streams *Streams, rawArgs []string) *cobra.Command {
	root := &cobra.Command{
		Use:           "bdd",
		Short:         "bdd is a CLI for tracking small cards.",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	registerGlobalFlags(root.PersistentFlags())
	root.AddCommand(buildCommands(global, streams, rawArgs)...)
	return root
}

// buildCommands constructs the full set of top-level cobra.Commands. It is
// built fresh on every Run call (never a package-level var) so it never
// carries stale global/streams closures or output writers across calls.
func buildCommands(global GlobalFlags, streams *Streams, rawArgs []string) []*cobra.Command {
	cmds := []*cobra.Command{
		newLeaf("init [path]", "Create a new workspace database",
			"Create a new bdd workspace database at <path> (default: the current\ndirectory, or --workspace's directory).",
			"  bdd init\n  bdd init --prefix acme ./acme-project",
			runInit, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.String("prefix", "", "workspace ID prefix (derived from the directory name if omitted)")
			}),

		newLeaf("status", "Show the resolved workspace, database, and schema state",
			"Print the resolved workspace directory, database path, and schema version.\n--upgrade applies any pending schema migration first.",
			"  bdd status\n  bdd status --upgrade",
			runStatus, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.Bool("upgrade", false, "apply a pending schema migration before reporting status")
			}),

		newGroup("config", "Read or write workspace configuration",
			"Manage key/value configuration entries stored in the workspace database.",
			"  bdd config get status.custom\n  bdd config set status.custom \"triage:active\"",
			runConfig, global, streams, rawArgs,
			newLeaf("get <key>", "Print a configuration value", "Print the value stored for <key>.",
				"  bdd config get status.custom", runConfigGet, global, streams, rawArgs, nil),
			newLeaf("set <key> <value>", "Set a configuration value", "Set <key> to <value>, creating or overwriting the entry.",
				`  bdd config set status.custom "triage:active"`, runConfigSet, global, streams, rawArgs, nil),
			newLeaf("unset <key>", "Remove a configuration value", "Remove the configuration entry for <key>.",
				"  bdd config unset status.custom", runConfigUnset, global, streams, rawArgs, nil),
			newLeaf("list", "List all configuration entries", "List every configuration key/value pair.",
				"  bdd config list", runConfigList, global, streams, rawArgs, nil),
		),

		newLeaf("statuses", "List built-in and custom statuses",
			"List every status known to the workspace, built-in and custom, with its category.",
			"  bdd statuses", runStatuses, global, streams, rawArgs, nil),

		newLeaf("types", "List built-in and custom card types",
			"List every card type known to the workspace, built-in and custom.",
			"  bdd types", runTypes, global, streams, rawArgs, nil),

		newGroup("memory", "Manage durable, keyed, workspace-scoped memories",
			"Create, read, list, search, and remove durable, keyed, workspace-scoped\nmemories.",
			`  bdd memory create "prefer small PRs" --key style
  bdd memory get style
  bdd memory search style`,
			runMemory, global, streams, rawArgs,
			newLeaf("create [body]", "Create a new memory",
				"Create a new durable, keyed, workspace-scoped memory. --key is required and\nmust not already exist. Supply the body as a positional argument, or pipe\nit via --stdin.",
				`  bdd memory create "prefer small PRs" --key style
  echo "prefer small PRs" | bdd memory create --key style --stdin`,
				runMemoryCreate, global, streams, rawArgs, func(f *pflag.FlagSet) {
					f.String("key", "", "memory key (required)")
					f.String("prime", "", "how `bdd prime` surfaces this memory: required or optional (default)")
					f.Bool("stdin", false, "read the body from stdin instead of a positional argument")
				}),
			newLeaf("update <key> [body]", "Update an existing memory",
				"Update the memory stored at <key>, which must already exist. Supply the\nbody as a positional argument, or pipe it via --stdin.",
				`  bdd memory update style "prefer small PRs, always"
  echo "prefer small PRs, always" | bdd memory update style --stdin`,
				runMemoryUpdate, global, streams, rawArgs, func(f *pflag.FlagSet) {
					f.String("prime", "", "how `bdd prime` surfaces this memory: required or optional (default)")
					f.Bool("stdin", false, "read the body from stdin instead of a positional argument")
				}),
			newLeaf("get <key>", "Show a memory's full record",
				"Print the full body of the memory stored at <key>.",
				"  bdd memory get style", runMemoryGet, global, streams, rawArgs, nil),
			newLeaf("list", "List all memories",
				"List every memory.",
				"  bdd memory list", runMemoryList, global, streams, rawArgs, nil),
			newLeaf("search <query>", "Search memories by text",
				"List memories whose key or body contains <query>.",
				"  bdd memory search style", runMemorySearch, global, streams, rawArgs, nil),
			newLeaf("remove <key>", "Delete a memory",
				"Delete the memory stored at <key>.",
				"  bdd memory remove style", runMemoryRemove, global, streams, rawArgs, nil),
		),

		newGroup("rune", "Manage rune records",
			"Manage rune records: reusable, keyed documents (checklists, prompts,\nreference material) attached to the workspace rather than any one card.",
			`  bdd rune set doc/review-checklist --kind doc --title "Review checklist" --body "..."
  bdd rune list --kind doc`,
			runRune, global, streams, rawArgs,
			newLeaf("set <key>", "Create or update a rune",
				"Create or update the rune at <key>. Supply the body directly with --body,\nor read it from a file with --body-file.",
				`  bdd rune set doc/review-checklist --kind doc --title "Review checklist" --body "..."`,
				runRuneSet, global, streams, rawArgs, func(f *pflag.FlagSet) {
					f.String("kind", "", "rune kind (e.g. doc, prompt, checklist)")
					f.String("title", "", "rune title")
					f.String("body", "", "rune body text")
					f.String("body-file", "", "read the body from this file instead of --body")
					f.String("metadata", "", "JSON metadata object")
					f.String("prime", "", "how `bdd prime` surfaces this rune: required, optional (default), or never")
					f.Bool("protected", false, "require --force to mutate or remove this rune later")
					f.Bool("create-only", false, "fail if the rune already exists")
					f.Int64("if-revision", 0, "fail unless the rune is currently at this revision")
					f.Bool("force", false, "acknowledge overwriting a protected rune")
				}),
			newLeaf("get <key>", "Get a rune's full record", "Print the full record for the rune at <key>.",
				"  bdd rune get doc/review-checklist", runRuneGet, global, streams, rawArgs, nil),
			newLeaf("list", "List runes", "List rune summaries, optionally filtered by kind.",
				"  bdd rune list --kind doc", runRuneList, global, streams, rawArgs, func(f *pflag.FlagSet) {
					f.String("kind", "", "only list runes of this kind")
					f.Bool("all", false, "include disabled runes")
				}),
			newLeaf("search <text>", "Search runes by text", "Search rune titles and bodies for <text>.",
				"  bdd rune search checklist", runRuneSearch, global, streams, rawArgs, func(f *pflag.FlagSet) {
					f.String("kind", "", "only search runes of this kind")
					f.Bool("all", false, "include disabled runes")
				}),
			newLeaf("enable <key>", "Enable a rune", "Mark the rune at <key> enabled.",
				"  bdd rune enable doc/review-checklist", func(g GlobalFlags, cmd *cobra.Command, a []string, s *Streams) int {
					return runRuneSetEnabled(g, cmd, a, s, true)
				}, global, streams, rawArgs, func(f *pflag.FlagSet) {
					f.Bool("force", false, "acknowledge enabling a protected rune")
				}),
			newLeaf("disable <key>", "Disable a rune", "Mark the rune at <key> disabled.",
				"  bdd rune disable doc/review-checklist --force", func(g GlobalFlags, cmd *cobra.Command, a []string, s *Streams) int {
					return runRuneSetEnabled(g, cmd, a, s, false)
				}, global, streams, rawArgs, func(f *pflag.FlagSet) {
					f.Bool("force", false, "acknowledge disabling a protected rune")
				}),
			newLeaf("remove <key>", "Delete a rune", "Delete the rune at <key>.",
				"  bdd rune remove doc/review-checklist --force", runRuneRemove, global, streams, rawArgs, func(f *pflag.FlagSet) {
					f.Bool("force", false, "acknowledge removing a protected rune")
				}),
		),

		newLeaf("create [title]", "Create a new card",
			"Create a new card. Supply the title as a positional argument or --title.\nRequired text fields vary by --type; a field can also be read from a file\nwith --<field>-file, or from stdin with --stdin when exactly one required\ntext field is still unset.",
			`  bdd create "Fix login bug" --type bug --priority P1 --reproduce "steps..." --acceptance "..."`,
			runCardCreate, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.String("title", "", "card title (alternative to the positional argument)")
				f.String("type", "", "card type")
				f.String("priority", "", "priority, e.g. P1 or 1")
				f.String("description", "", "description text")
				f.String("description-file", "", "read description from this file")
				f.String("reproduce", "", "reproduction steps text")
				f.String("reproduce-file", "", "read reproduction steps from this file")
				f.String("design", "", "design text")
				f.String("design-file", "", "read design from this file")
				f.String("acceptance", "", "acceptance criteria text")
				f.String("acceptance-file", "", "read acceptance criteria from this file")
				f.String("worktree", "", "worktree path to associate with the card")
				f.StringArray("label", nil, "label to add (repeatable)")
				f.StringArray("parent", nil, "parent card id to block on (repeatable)")
				f.String("notes", "", "initial note text")
				f.Bool("stdin", false, "read the one still-unset required text field from stdin")
			}),

		newLeaf("show <id>", "Show a card's full record",
			"Print a card's full record, including its notes.",
			"  bdd show bdd-a1b", runCardShow, global, streams, rawArgs, nil),

		newLeaf("list", "List cards matching filters",
			"List cards, optionally filtered by status, type, label, or parent/child edge.",
			"  bdd list --status in_progress --limit 20", runCardList, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.StringArray("status", nil, "only cards with this status (repeatable)")
				f.StringArray("status-category", nil, "only cards whose status has this category (repeatable)")
				f.StringArray("type", nil, "only cards of this type (repeatable)")
				f.StringArray("label", nil, "only cards with this label (repeatable)")
				f.Bool("all", false, "include done-category cards")
				f.String("parent", "", "only cards blocked by this parent id")
				f.String("child", "", "only cards blocking this child id")
				f.String("description-like", "", "only cards whose description contains this text")
				f.String("sort", "", "sort field")
				f.Bool("reverse", false, "reverse the sort order")
				f.String("limit", "", "maximum number of cards to return (default 20; 0 = no limit)")
			}),

		newLeaf("search <query>", "Search cards by text",
			"Search card titles and text fields for <query>.",
			`  bdd search "login bug" --limit 10`, runCardSearch, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.StringArray("status", nil, "only cards with this status (repeatable)")
				f.Bool("all", false, "include cards outside the active status category")
				f.StringArray("label", nil, "only cards with this label (repeatable)")
				f.String("limit", "", "maximum number of cards to return (default 20; 0 = no limit)")
			}),

		newLeaf("ready [id]", "List ready cards",
			"List every active-category, unassigned, unclaimed card without the human label.\n--explain [id] reports exactly why a given card (or every matching card) is\nexcluded.",
			"  bdd ready --limit 10\n  bdd ready --explain bdd-a1b", runCardReady, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.StringArray("label", nil, "only cards with this label (repeatable)")
				f.String("limit", "", "maximum number of cards to return (default 20; 0 = no limit)")
				f.Bool("explain", false, "report exclusion reasons instead of listing ready cards")
			}),

		newLeaf("update <id>", "Update a card's fields, status, labels, or edges",
			"Update one or more fields of the card at <id>, claim it, or adjust its\nlabels and parent/child edges. At least one of --claim or a field flag is\nrequired.",
			"  bdd update bdd-a1b --claim\n  bdd update bdd-a1b --status in_progress --add-label urgent",
			runCardUpdate, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.Bool("claim", false, "claim the card for the calling actor")
				f.String("title", "", "new title")
				f.String("type", "", "new type")
				f.String("status", "", "new status")
				f.String("priority", "", "new priority, e.g. P1 or 1")
				f.String("description", "", "new description text")
				f.String("reproduce", "", "new reproduction steps text")
				f.String("design", "", "new design text")
				f.String("acceptance", "", "new acceptance criteria text")
				f.String("external-ref", "", "new external reference")
				f.String("worktree", "", "new worktree path")
				f.Bool("clear-worktree", false, "clear the worktree field")
				f.StringArray("add-label", nil, "label to add (repeatable)")
				f.StringArray("remove-label", nil, "label to remove (repeatable)")
				f.StringArray("add-parent", nil, "parent card id to add (repeatable)")
				f.StringArray("remove-parent", nil, "parent card id to remove (repeatable)")
				f.StringArray("add-child", nil, "child card id to add (repeatable)")
				f.StringArray("remove-child", nil, "child card id to remove (repeatable)")
			}),

		newLeaf("note <id> [body]", "Append a note to a card",
			"Append an append-only note to the card at <id>. Supply the body as a\npositional argument, or pipe it via --stdin.",
			`  bdd note bdd-a1b "investigated, root cause is X"`, runCardNote, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.Bool("stdin", false, "read the body from stdin instead of a positional argument")
			}),

		newLeaf("close <id> [reason]", "Close a card",
			"Close the card at <id>, optionally recording a reason.",
			`  bdd close bdd-a1b "fixed in v1.2"`, runCardClose, global, streams, rawArgs, nil),

		newLeaf("reopen <id>", "Reopen a done-category card",
			"Move the card at <id> back out of a done-category status.",
			"  bdd reopen bdd-a1b", runCardReopen, global, streams, rawArgs, nil),

		newLeaf("defer <id>", "Defer a card",
			"Move the card at <id> to a deferred status, optionally until a given time.",
			"  bdd defer bdd-a1b --until 2026-08-01T00:00:00Z", runCardDefer, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.String("until", "", "RFC3339 timestamp to defer until")
			}),

		newLeaf("human <id> [reason]", "Flag a card as needing human attention",
			"Flag the card at <id> as needing human attention, optionally recording why.",
			`  bdd human bdd-a1b "needs product decision"`, runCardHuman, global, streams, rawArgs, nil),

		newLeaf("parents <id>", "List a card's blocking parents",
			"List the cards that block the card at <id>.",
			"  bdd parents bdd-a1b", runCardParents, global, streams, rawArgs, nil),

		newLeaf("children <id>", "List a card's blocked children",
			"List the cards blocked by the card at <id>.",
			"  bdd children bdd-a1b", runCardChildren, global, streams, rawArgs, nil),

		newLeaf("history <id>", "Show a card's audit trail",
			"Print every audit-trail event recorded for the card at <id> (creation,\nupdates, notes, edges, deletion), oldest first.",
			"  bdd history bdd-a1b", runCardHistory, global, streams, rawArgs, nil),

		newGroup("label", "Manage a card's labels",
			"Add, remove, or list the labels on a card.",
			"  bdd label add bdd-a1b urgent\n  bdd label list bdd-a1b",
			runCardLabel, global, streams, rawArgs,
			newLeaf("add <id> <label>", "Add a label to a card", "Add <label> to the card at <id>.",
				"  bdd label add bdd-a1b urgent", func(g GlobalFlags, cmd *cobra.Command, a []string, s *Streams) int {
					return runCardLabelMutate(g, cmd, a, s, true)
				}, global, streams, rawArgs, nil),
			newLeaf("remove <id> <label>", "Remove a label from a card", "Remove <label> from the card at <id>.",
				"  bdd label remove bdd-a1b urgent", func(g GlobalFlags, cmd *cobra.Command, a []string, s *Streams) int {
					return runCardLabelMutate(g, cmd, a, s, false)
				}, global, streams, rawArgs, nil),
			newLeaf("list <id>", "List a card's labels", "List the labels on the card at <id>.",
				"  bdd label list bdd-a1b", runCardLabelList, global, streams, rawArgs, nil),
		),

		newLeaf("delete <id>", "Hard-delete a card and its edges",
			"Permanently delete the card at <id> and its parent/child edges. Requires\n--force to acknowledge the destructive, irreversible operation.",
			"  bdd delete bdd-a1b --force", runCardDelete, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.Bool("force", false, "acknowledge the destructive delete")
			}),

		newLeaf("snapshot", "Write an integrity-checked backup of the live database",
			"Write a point-in-time, integrity-checked backup of the live database.",
			"  bdd snapshot --output bdd_backup.sqlite", runSnapshot, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.String("output", "", "backup file path (default: <workspace>/bdd_backup.sqlite)")
			}),

		newLeaf("restore <snapshot.sqlite>", "Install a snapshot as the workspace database",
			"Install <snapshot.sqlite> as the workspace database, backing up any\nexisting database first. Requires --force to acknowledge the destructive\noperation.",
			"  bdd restore bdd_backup.sqlite --force", runRestore, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.Bool("force", false, "acknowledge the destructive restore")
			}),

		newLeaf("prime", "Print a compact session-start manifest",
			"Print a compact bootstrap manifest (identity, invariant rules, workflow\ncommands, required-rune bodies, and optional-context summaries) for an\nagent to load at session start. --full reproduces the previous full prose\ncontract instead.",
			"  bdd prime\n  bdd prime --memory-limit 20\n  bdd prime --full", runPrime, global, streams, rawArgs, func(f *pflag.FlagSet) {
				f.String("memory-limit", "", "cap the number of memories printed")
				f.Bool("no-memories", false, "skip loading memories entirely")
				f.Bool("full", false, "print the previous full prose contract instead of the compact manifest")
			}),
	}

	return cmds
}

// generateCommandsReference renders a one-line-per-command summary of cmds
// (each top-level command's Use, or for a group, its name plus a
// "|"-joined list of its subcommand names, and its Short text), plus the
// two commands Run handles before ever building a cobra tree. See
// commandsReferenceText (cli.go) for how this replaces a hand-maintained,
// driftable copy.
func generateCommandsReference(cmds []*cobra.Command) string {
	type row struct{ left, right string }
	rows := make([]row, 0, len(cmds)+2)

	for _, c := range cmds {
		left := c.Use
		if children := c.Commands(); len(children) > 0 {
			names := make([]string, 0, len(children))
			for _, sub := range children {
				names = append(names, strings.SplitN(sub.Use, " ", 2)[0])
			}
			left = c.Name() + " " + strings.Join(names, "|")
		}
		rows = append(rows, row{left, c.Short})
	}
	rows = append(rows, row{"version", "Print the bdd version"})
	rows = append(rows, row{"help", "Show this help text"})

	width := 0
	for _, r := range rows {
		if len(r.left) > width {
			width = len(r.left)
		}
	}

	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s   %s\n", width, r.left, r.right)
	}
	return b.String()
}
