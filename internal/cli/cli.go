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

// commandsReferenceText renders a one-line-per-command summary of the full
// command tree straight from buildCommands' Use/Short/subcommand
// definitions (internal/cli/cobra_tree.go) plus the two commands Run
// special-cases before ever reaching cobra (version, help). helpText and
// `bdd prime --full` (primeContract in prime.go) both call this instead of
// each hardcoding their own copy, so neither can drift into advertising a
// command the tree doesn't actually implement (plan section 19).
func commandsReferenceText() string {
	return generateCommandsReference(buildCommands(GlobalFlags{}, &Streams{Stdout: io.Discard, Stderr: io.Discard}, nil))
}

// helpTextFor renders the top-level `bdd help`/`bdd`/`bdd --help` text.
func helpTextFor() string {
	return `bdd is a CLI for tracking small cards.

Usage:
  bdd [global flags] <command> [flags]

Global flags:
  --workspace, -C <dir>  Resolve the workspace starting from <dir> (default: cwd)
  --actor <name>          Actor recorded against mutations (see BDD_ACTOR)
  --json                  Emit machine-readable JSON instead of human output
  --silent                Emit minimal output and suppress incidental diagnostics

Commands:
` + commandsReferenceText() + `
Run 'bdd help' or 'bdd version' at any time: neither command touches a
workspace or database.
`
}

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
		fmt.Fprint(stdout, helpTextFor())
		return ExitSuccess
	}

	cmd, cmdArgs := rest[0], rest[1:]

	switch cmd {
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "bdd version %s (%s)\n", version, commit)
		return ExitSuccess
	case "help", "--help", "-h":
		fmt.Fprint(stdout, helpTextFor())
		return ExitSuccess
	}

	streams := &Streams{Stdout: stdout, Stderr: stderr, Stdin: os.Stdin, JSON: global.JSON, Silent: global.Silent}

	// "memory set" is intercepted here, before cobra ever parses cmdArgs,
	// so the create/update guidance fires regardless of what flags follow
	// (e.g. "memory set body --key foo"): "set" isn't a registered leaf, so
	// any flag not known to the "memory" group itself (like --key) would
	// otherwise surface as cobra's own "unknown flag" error rather than
	// this steering message.
	if cmd == "memory" && len(cmdArgs) > 0 && cmdArgs[0] == "set" {
		streams.Errorf("bdd: memory: \"set\" was split into \"create\" (fails if the key exists) and \"update\" (fails if it doesn't)\n")
		return ExitUsage
	}

	execArgs := append([]string{cmd}, cmdArgs...)
	root := buildRoot(global, streams, execArgs)
	root.SetArgs(execArgs)
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
