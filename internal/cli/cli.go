// Package cli implements the shared plumbing for the bdd command: global
// flag parsing, actor resolution, exit-code mapping, stream discipline, and
// JSON rendering conventions, plus the individual subcommands built on top
// of it.
package cli

import (
	"fmt"
	"io"
	"os"
)

// commandsReference is the single source of truth for the CLI's supported
// command set: helpText renders it verbatim, and `bdd prime` (internal/cli/
// prime.go) derives its command-set section from it, so the two can never
// drift apart into `prime` advertising a command Run's switch below does
// not actually implement (plan section 19).
const commandsReference = `  init [--prefix <prefix>] [path]   Create a new workspace database
  status [--upgrade]                Show the resolved workspace, database, and schema state
  config get|set|unset|list         Read or write workspace configuration
  statuses                          List built-in and custom statuses
  types                             List built-in and custom card types
  remember [body] [--key <key>] [--stdin]   Create or update a memory
  memories [query]                  List memories, optionally filtered by text
  recall <key>                      Show a memory's full record
  forget <key>                      Delete a memory
  rune put|show|list|search|enable|disable|remove|export   Manage rune records
  create [title] [flags]            Create a new card
  show <id>                         Show a card's full record
  list [flags]                      List cards matching filters
  search <query> [flags]            Search cards by text
  ready [flags]                     List dispatchable cards; --explain [id] to see exclusions
  update <id> [flags]               Update a card's fields, status, labels, or edges
  note <id> [body] [--stdin]        Append a note to a card
  close <id> [reason]               Close a card
  reopen <id>                       Reopen a done-category card
  defer <id> [--until <time>]       Defer a card
  human <id> [reason]               Flag a card as needing human attention
  parents <id>                      List a card's blocking parents
  children <id>                     List a card's blocked children
  label add|remove|list <id> [l]    Manage a card's labels
  delete <id> --force               Hard-delete a card and its edges
  snapshot [--output <path>]        Write an integrity-checked backup of the live database
  restore <snapshot.sqlite> --force   Install a snapshot as the workspace database
  prime [--memory-limit <n>] [--no-memories]   Print the workspace contract and memories for session start
  version                           Print the bdd version
  help                              Show this help text
`

const helpText = `bdd is a CLI for tracking small cards.

Usage:
  bdd [global flags] <command> [flags]

Global flags:
  --workspace, -C <dir>  Resolve the workspace starting from <dir> (default: cwd)
  --db <path>             Use this database file instead of workspace discovery
  --actor <name>          Actor recorded against mutations (see BDD_ACTOR)
  --json                  Emit machine-readable JSON instead of human output
  --silent                Emit minimal output and suppress incidental diagnostics

Commands:
` + commandsReference + `
Run 'bdd help' or 'bdd version' at any time: neither command touches a
workspace or database.
`

// Run implements the CLI entry point. It never returns an error: every
// failure is reported on stderr and reflected in the returned exit code.
// version and help parse and print without workspace discovery or opening
// SQLite, keeping that fast path intact regardless of what global flags
// precede them.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	global, rest, err := ParseGlobalFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitUsage
	}

	if len(rest) == 0 {
		fmt.Fprint(stdout, helpText)
		return ExitSuccess
	}

	cmd, cmdArgs := rest[0], rest[1:]

	switch cmd {
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, version)
		return ExitSuccess
	case "help", "--help", "-h":
		fmt.Fprint(stdout, helpText)
		return ExitSuccess
	}

	streams := &Streams{Stdout: stdout, Stderr: stderr, Stdin: os.Stdin, JSON: global.JSON, Silent: global.Silent}

	switch cmd {
	case "init":
		return runInit(global, cmdArgs, streams)
	case "status":
		return runStatus(global, cmdArgs, streams)
	case "config":
		return runConfig(global, cmdArgs, streams)
	case "statuses":
		return runStatuses(global, cmdArgs, streams)
	case "types":
		return runTypes(global, cmdArgs, streams)
	case "remember":
		return runRemember(global, cmdArgs, streams)
	case "memories":
		return runMemories(global, cmdArgs, streams)
	case "recall":
		return runRecall(global, cmdArgs, streams)
	case "forget":
		return runForget(global, cmdArgs, streams)
	case "rune":
		return runRune(global, cmdArgs, streams)
	case "create":
		return runCardCreate(global, cmdArgs, streams)
	case "show":
		return runCardShow(global, cmdArgs, streams)
	case "list":
		return runCardList(global, cmdArgs, streams)
	case "search":
		return runCardSearch(global, cmdArgs, streams)
	case "ready":
		return runCardReady(global, cmdArgs, streams)
	case "update":
		return runCardUpdate(global, cmdArgs, streams)
	case "note":
		return runCardNote(global, cmdArgs, streams)
	case "close":
		return runCardClose(global, cmdArgs, streams)
	case "reopen":
		return runCardReopen(global, cmdArgs, streams)
	case "defer":
		return runCardDefer(global, cmdArgs, streams)
	case "human":
		return runCardHuman(global, cmdArgs, streams)
	case "parents":
		return runCardParents(global, cmdArgs, streams)
	case "children":
		return runCardChildren(global, cmdArgs, streams)
	case "label":
		return runCardLabel(global, cmdArgs, streams)
	case "delete":
		return runCardDelete(global, cmdArgs, streams)
	case "snapshot":
		return runSnapshot(global, cmdArgs, streams)
	case "restore":
		return runRestore(global, cmdArgs, streams)
	case "prime":
		return runPrime(global, cmdArgs, streams)
	default:
		fmt.Fprintf(stderr, "bdd: unknown command %q\n", cmd)
		fmt.Fprint(stderr, helpText)
		return ExitUsage
	}
}
