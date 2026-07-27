package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
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

	// NoHooks forces hooks off for this invocation (--no-hooks), regardless
	// of the hooks.enabled config key. See also BDD_NO_HOOKS.
	NoHooks bool
}

// ParseGlobalFlags extracts every recognized global flag from args, in
// whatever order and position they appear, and returns the remaining
// tokens untouched and in their original relative order. Both
// "--flag=value" and "--flag value" are accepted for value-taking flags.
//
// This pass runs ahead of cobra so a global flag may appear before or after
// the subcommand name; cobra's own per-command FlagSet parsing (see
// cobra_tree.go) never sees these five flags.
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
		case "--no-hooks":
			g.NoHooks = true
			i++
			continue
		default:
			rest = append(rest, arg)
			i++
			continue
		}

		if hasInline {
			*target = inline
			i++
			continue
		}
		if i+1 >= len(args) {
			return GlobalFlags{}, nil, fmt.Errorf("bdd: flag %s requires a value", name)
		}
		*target = args[i+1]
		i += 2
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

// reportUnknownArg reports an unrecognized token as an unknown flag if it
// looks like one (leading "-", covering both "--foo" and "--foo=bar"
// forms), or as an unknown positional argument otherwise. This is the
// shared wording convention every bdd subcommand uses so removed or
// mistyped flags (e.g. the removed global --db) are never misreported as
// positional arguments. It stays in use post-cobra-migration for arity
// overflow: cobra's own FlagSet.Parse already rejects unknown flags before
// a handler runs, but a *known*-shaped positional token past a command's
// fixed arity (e.g. a third argument to `close <id> [reason]`) is still the
// handler's job to reject.
func reportUnknownArg(s *Streams, cmd, arg string) int {
	if strings.HasPrefix(arg, "-") {
		s.Errorf("bdd: %s: unknown flag %q\n", cmd, arg)
	} else {
		s.Errorf("bdd: %s: unknown argument %q\n", cmd, arg)
	}
	return ExitUsage
}

// flagString reads a string flag's value and whether it was explicitly
// passed (as opposed to left at its zero-value default), the distinction
// the handwritten "have<Field> bool" pattern used to track by hand.
func flagString(fs *pflag.FlagSet, name string) (value string, changed bool) {
	v, _ := fs.GetString(name)
	return v, fs.Changed(name)
}

// flagBool reads a bool flag's value; bool flags need no changed/omitted
// distinction since their zero value (false) is never itself a meaningful
// "explicitly set" state for any bdd command.
func flagBool(fs *pflag.FlagSet, name string) bool {
	v, _ := fs.GetBool(name)
	return v
}

// flagStringSlice reads a repeatable string flag's values (e.g. --label,
// repeated). A nil/empty result means the flag was never passed.
func flagStringSlice(fs *pflag.FlagSet, name string) []string {
	v, _ := fs.GetStringArray(name)
	return v
}

// flagInt64 reads an int64 flag's value and whether it was explicitly
// passed.
func flagInt64(fs *pflag.FlagSet, name string) (value int64, changed bool) {
	v, _ := fs.GetInt64(name)
	return v, fs.Changed(name)
}
