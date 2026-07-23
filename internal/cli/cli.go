// Package cli implements the shared plumbing for the bdd command: global
// flag parsing, actor resolution, exit-code mapping, stream discipline, and
// JSON rendering conventions, plus the individual subcommands built on top
// of it.
package cli

import (
	"errors"
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
  memory set|get|list|search|remove   Manage durable, keyed, workspace-scoped memories
  rune set|get|list|search|enable|disable|remove   Manage rune records
  create [title] [flags]            Create a new card
  show <id>                         Show a card's full record
  list [flags]                      List cards matching filters
  search <query> [flags]            Search cards by text
  ready [flags]                     List ready cards; --explain [id] to see exclusions
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
func Run(args []string, stdout, stderr io.Writer, version, commit string) int {
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
		fmt.Fprintf(stdout, "bdd version %s (%s)\n", version, commit)
		return ExitSuccess
	case "help", "--help", "-h":
		fmt.Fprint(stdout, helpText)
		return ExitSuccess
	}

	streams := &Streams{Stdout: stdout, Stderr: stderr, Stdin: os.Stdin, JSON: global.JSON, Silent: global.Silent}

	root := buildRoot(global, streams)
	root.SetArgs(append([]string{cmd}, cmdArgs...))
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.Execute(); err != nil {
		var ee exitError
		if errors.As(err, &ee) {
			return ee.code
		}
		fmt.Fprintln(stderr, err)
		return ExitUsage
	}
	return ExitSuccess
}
