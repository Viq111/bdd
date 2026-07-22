package cli

import (
	"fmt"
	"strings"
)

// GlobalFlags holds the flags shared by every bdd subcommand.
type GlobalFlags struct {
	// Workspace is the directory to resolve the workspace from (--workspace,
	// -C). Empty means the current working directory.
	Workspace string

	// Actor is the caller-supplied actor override (--actor). Empty means
	// fall through the rest of the precedence chain; see ResolveActor.
	Actor string

	// JSON requests machine-readable output.
	JSON bool

	// Silent requests terse output: no incidental stderr diagnostics on
	// success, and minimal stdout.
	Silent bool
}

// ParseGlobalFlags extracts every recognized global flag from args, in
// whatever order and position they appear, and returns the remaining
// tokens untouched and in their original relative order. Both
// "--flag=value" and "--flag value" are accepted for value-taking flags.
//
// Command-specific parsing runs on the returned remainder, so a global
// flag may appear before or after the subcommand name.
func ParseGlobalFlags(args []string) (GlobalFlags, []string, error) {
	var g GlobalFlags
	rest := make([]string, 0, len(args))

	i := 0
	for i < len(args) {
		arg := args[i]
		name, inline, hasInline := cutFlagValue(arg)

		var target *string
		switch name {
		case "--workspace", "-C":
			target = &g.Workspace
		case "--actor":
			target = &g.Actor
		case "--json":
			g.JSON = true
			i++
			continue
		case "--silent":
			g.Silent = true
			i++
			continue
		default:
			rest = append(rest, arg)
			i++
			continue
		}

		val, consumed, err := flagValue(name, inline, hasInline, args, i)
		if err != nil {
			return GlobalFlags{}, nil, err
		}
		*target = val
		i += consumed
	}

	return g, rest, nil
}

// cutFlagValue splits a "--flag=value" token into its name and value. A
// token with no "=" (including one with no leading "-") is returned as-is
// with hasValue false.
func cutFlagValue(arg string) (name, value string, hasValue bool) {
	if !strings.HasPrefix(arg, "-") {
		return arg, "", false
	}
	if eq := strings.IndexByte(arg, '='); eq >= 0 {
		return arg[:eq], arg[eq+1:], true
	}
	return arg, "", false
}

// flagValue resolves a value-taking flag's value, either from an inline
// "--flag=value" token or from the following token, and reports how many
// tokens (starting at i) it consumed.
func flagValue(name, inline string, hasInline bool, args []string, i int) (value string, consumed int, err error) {
	if hasInline {
		return inline, 1, nil
	}
	if i+1 >= len(args) {
		return "", 0, fmt.Errorf("bdd: flag %s requires a value", name)
	}
	return args[i+1], 2, nil
}

// reportUnknownArg reports an unrecognized token as an unknown flag if it
// looks like one (leading "-", covering both "--foo" and "--foo=bar"
// forms), or as an unknown positional argument otherwise. This is the
// shared wording convention every bdd subcommand uses so removed or
// mistyped flags (e.g. the removed global --db) are never misreported as
// positional arguments.
func reportUnknownArg(s *Streams, cmd, arg string) int {
	if strings.HasPrefix(arg, "-") {
		s.Errorf("bdd: %s: unknown flag %q\n", cmd, arg)
	} else {
		s.Errorf("bdd: %s: unknown argument %q\n", cmd, arg)
	}
	return ExitUsage
}

// firstFlagArg returns the first flag-shaped (leading "-") token in args,
// if any. Commands with a fixed positional arity (e.g. "expects exactly
// one id argument") must check this before their count check: a stray
// removed/mistyped flag like --db changes the token count too, and would
// otherwise be misreported as a wrong-arity error instead of an unknown
// flag.
func firstFlagArg(args []string) (string, bool) {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return arg, true
		}
	}
	return "", false
}
