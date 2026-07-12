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
  init [--prefix <prefix>] [path]   Create a new workspace database
  status [--upgrade]                Show the resolved workspace, database, and schema state
  config get|set|unset|list         Read or write workspace configuration
  statuses                          List built-in and custom statuses
  types                             List built-in and custom card types
  remember [body] [--key <key>] [--stdin]   Create or update a memory
  memories [query]                  List memories, optionally filtered by text
  recall <key>                      Show a memory's full record
  forget <key>                      Delete a memory
  rune put|show|list|search|enable|disable|remove|export   Manage rune records
  version                           Print the bdd version
  help                              Show this help text

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
	default:
		fmt.Fprintf(stderr, "bdd: unknown command %q\n", cmd)
		fmt.Fprint(stderr, helpText)
		return ExitUsage
	}
}
