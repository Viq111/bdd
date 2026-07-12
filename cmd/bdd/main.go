// Command bdd is a thin, fast, non-interactive CLI over the bdd library.
package main

import (
	"os"

	"github.com/viq111/bdd/internal/cli"
)

// version is stamped at release build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run implements the CLI entry point, delegating flag parsing, dispatch,
// and rendering to internal/cli.
func run(args []string, stdout, stderr *os.File) int {
	return cli.Run(args, stdout, stderr, version)
}
