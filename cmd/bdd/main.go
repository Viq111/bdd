// Command bdd is a thin, fast, non-interactive CLI over the bdd library.
package main

import (
	"fmt"
	"os"
)

// version is stamped at release build time via -ldflags "-X main.version=...".
var version = "dev"

const helpText = `bdd is a CLI for tracking small cards.

Usage:
  bdd <command> [flags]

Commands:
  version    Print the bdd version
  help       Show this help text

Run 'bdd help' or 'bdd version' at any time: neither command touches a
workspace or database.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run implements the CLI entry point. version and help are handled entirely
// here, without workspace discovery or opening SQLite, so they stay fast
// even once the rest of the CLI is wired up.
func run(args []string, stdout, stderr *os.File) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, helpText)
		return 0
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "--help", "-h":
		fmt.Fprint(stdout, helpText)
		return 0
	default:
		fmt.Fprintf(stderr, "bdd: unknown command %q\n", args[0])
		fmt.Fprint(stderr, helpText)
		return 2
	}
}
