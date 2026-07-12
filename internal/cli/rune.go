package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/viq111/bdd"
)

// RuneResult is the JSON/human result of `bdd rune put` and `bdd rune show`.
type RuneResult struct {
	Key       string `json:"key"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Metadata  string `json:"metadata"`
	Enabled   bool   `json:"enabled"`
	Protected bool   `json:"protected"`
	Revision  int64  `json:"revision"`
	CreatedBy string `json:"created_by"`
	UpdatedBy string `json:"updated_by"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toRuneResult(r *bdd.Rune) RuneResult {
	return RuneResult{
		Key:       r.Key,
		Kind:      r.Kind,
		Title:     r.Title,
		Body:      r.Body,
		Metadata:  r.Metadata,
		Enabled:   r.Enabled,
		Protected: r.Protected,
		Revision:  r.Revision,
		CreatedBy: r.CreatedBy,
		UpdatedBy: r.UpdatedBy,
		CreatedAt: r.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt: r.UpdatedAt.Format(time.RFC3339Nano),
	}
}

// RuneSummaryResult is one entry of the JSON/human result of `bdd rune list`
// and `bdd rune search`.
type RuneSummaryResult struct {
	Key       string `json:"key"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Enabled   bool   `json:"enabled"`
	Protected bool   `json:"protected"`
	Revision  int64  `json:"revision"`
}

func toRuneSummaryResult(r bdd.RuneSummary) RuneSummaryResult {
	return RuneSummaryResult{
		Key: r.Key, Kind: r.Kind, Title: r.Title,
		Enabled: r.Enabled, Protected: r.Protected, Revision: r.Revision,
	}
}

// runRune implements `bdd rune put|show|list|search|enable|disable|remove|export`.
func runRune(g GlobalFlags, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: rune: missing subcommand (put, show, list, search, enable, disable, remove, export)\n")
		return ExitUsage
	}
	sub, rest := args[0], args[1:]

	switch sub {
	case "put":
		return runRunePut(g, rest, s)
	case "show":
		return runRuneShow(g, rest, s)
	case "list":
		return runRuneList(g, rest, s)
	case "search":
		return runRuneSearch(g, rest, s)
	case "enable":
		return runRuneSetEnabled(g, rest, s, true, "rune enable")
	case "disable":
		return runRuneSetEnabled(g, rest, s, false, "rune disable")
	case "remove":
		return runRuneRemove(g, rest, s)
	case "export":
		return runRuneExport(g, rest, s)
	default:
		s.Errorf("bdd: rune: unknown subcommand %q\n", sub)
		return ExitUsage
	}
}

// runRunePut implements `bdd rune put <key> [--kind <kind>] [--title <title>]
// [--body <body>|--body-file <path>] [--metadata <json>] [--protected]
// [--create-only] [--if-revision <n>] [--force]`.
func runRunePut(g GlobalFlags, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: rune put: key is required\n")
		return ExitUsage
	}
	key := args[0]
	rest := args[1:]

	var kind, title, body, bodyFile, metadata string
	var haveTitle, haveBody, haveMetadata bool
	var protected, createOnly, force bool
	var expectedRevision *int64

	i := 0
	for i < len(rest) {
		arg := rest[i]
		name, inline, hasInline := cutFlagValue(arg)

		switch name {
		case "--kind":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: rune put: %v\n", err)
				return ExitUsage
			}
			kind = val
			i += consumed
			continue
		case "--title":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: rune put: %v\n", err)
				return ExitUsage
			}
			title, haveTitle = val, true
			i += consumed
			continue
		case "--body":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: rune put: %v\n", err)
				return ExitUsage
			}
			body, haveBody = val, true
			i += consumed
			continue
		case "--body-file":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: rune put: %v\n", err)
				return ExitUsage
			}
			bodyFile = val
			i += consumed
			continue
		case "--metadata":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: rune put: %v\n", err)
				return ExitUsage
			}
			metadata, haveMetadata = val, true
			i += consumed
			continue
		case "--if-revision":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: rune put: %v\n", err)
				return ExitUsage
			}
			n, parseErr := strconv.ParseInt(val, 10, 64)
			if parseErr != nil {
				s.Errorf("bdd: rune put: --if-revision must be an integer, got %q\n", val)
				return ExitUsage
			}
			expectedRevision = &n
			i += consumed
			continue
		case "--protected":
			protected = true
			i++
			continue
		case "--create-only":
			createOnly = true
			i++
			continue
		case "--force":
			force = true
			i++
			continue
		default:
			s.Errorf("bdd: rune put: unknown flag %q\n", arg)
			return ExitUsage
		}
	}

	if bodyFile != "" {
		if haveBody {
			s.Errorf("bdd: rune put: cannot combine --body and --body-file\n")
			return ExitUsage
		}
		data, err := os.ReadFile(bodyFile)
		if err != nil {
			s.Errorf("bdd: rune put: reading %s: %v\n", bodyFile, err)
			return ExitOther
		}
		body, haveBody = string(data), true
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "rune put", s)
	if db == nil {
		return code
	}
	defer db.Close()

	if code, blocked := checkRuneForceRequired(ctx, db, key, force, s, "rune put"); blocked {
		return code
	}

	mutation := bdd.RuneMutation{}
	if haveTitle {
		mutation.Title = &title
	}
	if haveBody {
		mutation.Body = &body
	}
	if haveMetadata {
		mutation.Metadata = &metadata
	}
	if protected {
		t := true
		mutation.Protected = &t
	}

	actor := ResolveActor(g.Actor)
	r, err := db.PutRune(ctx, bdd.PutRune{
		Key: key, Kind: kind, Mutation: mutation,
		CreateOnly: createOnly, ExpectedRevision: expectedRevision,
		Actor: actor, Force: force,
	})
	if err != nil {
		s.Errorf("bdd: rune put: %v\n", err)
		return ExitCode(err)
	}
	return emitRune(s, "rune put", r)
}

// runRuneShow implements `bdd rune show <key>`.
func runRuneShow(g GlobalFlags, args []string, s *Streams) int {
	if len(args) != 1 {
		s.Errorf("bdd: rune show: expected exactly one key argument\n")
		return ExitUsage
	}
	key := args[0]

	ctx := context.Background()
	db, code := openDB(ctx, g, "rune show", s)
	if db == nil {
		return code
	}
	defer db.Close()

	r, err := db.GetRune(ctx, key)
	if err != nil {
		s.Errorf("bdd: rune show: %v\n", err)
		return ExitCode(err)
	}
	return emitRune(s, "rune show", r)
}

// runRuneList implements `bdd rune list [--kind <kind>] [--all]`.
func runRuneList(g GlobalFlags, args []string, s *Streams) int {
	kind, all, code := parseRuneListFlags(args, s, "rune list")
	if code != ExitSuccess {
		return code
	}

	ctx := context.Background()
	db, dbCode := openDB(ctx, g, "rune list", s)
	if db == nil {
		return dbCode
	}
	defer db.Close()

	summaries, err := db.ListRunes(ctx, bdd.RuneQuery{Kind: kind, All: all})
	if err != nil {
		s.Errorf("bdd: rune list: %v\n", err)
		return ExitCode(err)
	}
	return emitRuneSummaries(s, "rune list", summaries)
}

// runRuneSearch implements `bdd rune search <text> [--kind <kind>] [--all]`.
func runRuneSearch(g GlobalFlags, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: rune search: text is required\n")
		return ExitUsage
	}
	text := args[0]
	kind, all, code := parseRuneListFlags(args[1:], s, "rune search")
	if code != ExitSuccess {
		return code
	}

	ctx := context.Background()
	db, dbCode := openDB(ctx, g, "rune search", s)
	if db == nil {
		return dbCode
	}
	defer db.Close()

	summaries, err := db.SearchRunes(ctx, bdd.RuneQuery{Text: text, Kind: kind, All: all})
	if err != nil {
		s.Errorf("bdd: rune search: %v\n", err)
		return ExitCode(err)
	}
	return emitRuneSummaries(s, "rune search", summaries)
}

// parseRuneListFlags parses the [--kind <kind>] [--all] flags shared by
// `rune list` and `rune search`.
func parseRuneListFlags(args []string, s *Streams, cmdName string) (kind string, all bool, code int) {
	i := 0
	for i < len(args) {
		arg := args[i]
		name, inline, hasInline := cutFlagValue(arg)

		switch name {
		case "--kind":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: %s: %v\n", cmdName, err)
				return "", false, ExitUsage
			}
			kind = val
			i += consumed
			continue
		case "--all":
			all = true
			i++
			continue
		default:
			s.Errorf("bdd: %s: unknown flag %q\n", cmdName, arg)
			return "", false, ExitUsage
		}
	}
	return kind, all, ExitSuccess
}

// runRuneSetEnabled implements `bdd rune enable|disable <key> [--force]`.
func runRuneSetEnabled(g GlobalFlags, args []string, s *Streams, enabled bool, cmdName string) int {
	var key string
	var force bool

	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--force" {
			force = true
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			s.Errorf("bdd: %s: unknown flag %q\n", cmdName, arg)
			return ExitUsage
		}
		if key != "" {
			s.Errorf("bdd: %s: unexpected argument %q\n", cmdName, arg)
			return ExitUsage
		}
		key = arg
		i++
	}
	if key == "" {
		s.Errorf("bdd: %s: key is required\n", cmdName)
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, cmdName, s)
	if db == nil {
		return code
	}
	defer db.Close()

	if code, blocked := checkRuneForceRequired(ctx, db, key, force, s, cmdName); blocked {
		return code
	}

	actor := ResolveActor(g.Actor)
	r, err := db.SetRuneEnabled(ctx, key, enabled, actor, force)
	if err != nil {
		s.Errorf("bdd: %s: %v\n", cmdName, err)
		return ExitCode(err)
	}
	return emitRune(s, cmdName, r)
}

// runRuneRemove implements `bdd rune remove <key> [--force]`.
func runRuneRemove(g GlobalFlags, args []string, s *Streams) int {
	var key string
	var force bool

	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--force" {
			force = true
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			s.Errorf("bdd: rune remove: unknown flag %q\n", arg)
			return ExitUsage
		}
		if key != "" {
			s.Errorf("bdd: rune remove: unexpected argument %q\n", arg)
			return ExitUsage
		}
		key = arg
		i++
	}
	if key == "" {
		s.Errorf("bdd: rune remove: key is required\n")
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "rune remove", s)
	if db == nil {
		return code
	}
	defer db.Close()

	if code, blocked := checkRuneForceRequired(ctx, db, key, force, s, "rune remove"); blocked {
		return code
	}

	actor := ResolveActor(g.Actor)
	if err := db.RemoveRune(ctx, key, actor, force); err != nil {
		s.Errorf("bdd: rune remove: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(KeyResult{Key: key}); err != nil {
			s.Errorf("bdd: rune remove: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	fmt.Fprintln(s.Stdout, key)
	return ExitSuccess
}

// runRuneExport implements `bdd rune export <key> [--format markdown|json]`.
func runRuneExport(g GlobalFlags, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: rune export: key is required\n")
		return ExitUsage
	}
	key := args[0]
	format := "markdown"

	i := 1
	for i < len(args) {
		arg := args[i]
		name, inline, hasInline := cutFlagValue(arg)
		if name != "--format" {
			s.Errorf("bdd: rune export: unknown flag %q\n", arg)
			return ExitUsage
		}
		val, consumed, err := flagValue(name, inline, hasInline, args, i)
		if err != nil {
			s.Errorf("bdd: rune export: %v\n", err)
			return ExitUsage
		}
		format = val
		i += consumed
	}
	if format != "markdown" && format != "json" {
		s.Errorf("bdd: rune export: --format must be markdown or json, got %q\n", format)
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "rune export", s)
	if db == nil {
		return code
	}
	defer db.Close()

	data, err := db.ExportRune(ctx, key, format)
	if err != nil {
		s.Errorf("bdd: rune export: %v\n", err)
		return ExitCode(err)
	}

	s.Stdout.Write(data)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		fmt.Fprintln(s.Stdout)
	}
	return ExitSuccess
}

// checkRuneForceRequired pre-flights a rune mutation targeting an existing
// protected rune: it reports ExitConflict (per the CLI contract, protected
// mutations without --force exit 4) rather than letting the library's
// generic ErrInvalidArgument mapping (ExitUsage) apply. A key that doesn't
// exist yet is not blocked here: `rune put` may still create it, and the
// read/enable/disable/remove callers will surface their own ErrNotFound.
func checkRuneForceRequired(ctx context.Context, db *bdd.DB, key string, force bool, s *Streams, cmdName string) (code int, blocked bool) {
	existing, err := db.GetRune(ctx, key)
	if err != nil {
		if errors.Is(err, bdd.ErrNotFound) {
			return ExitSuccess, false
		}
		s.Errorf("bdd: %s: %v\n", cmdName, err)
		return ExitCode(err), true
	}
	if existing.Protected && !force {
		s.Errorf("bdd: %s: %s is protected; use --force\n", cmdName, key)
		return ExitConflict, true
	}
	return ExitSuccess, false
}

func emitRune(s *Streams, cmdName string, r *bdd.Rune) int {
	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(toRuneResult(r)); err != nil {
			s.Errorf("bdd: %s: %v\n", cmdName, err)
			return ExitOther
		}
		return ExitSuccess
	}
	if s.Silent {
		fmt.Fprintln(s.Stdout, r.Key)
		return ExitSuccess
	}

	fmt.Fprintf(s.Stdout, "key:       %s\n", r.Key)
	fmt.Fprintf(s.Stdout, "kind:      %s\n", r.Kind)
	fmt.Fprintf(s.Stdout, "title:     %s\n", r.Title)
	fmt.Fprintf(s.Stdout, "enabled:   %t\n", r.Enabled)
	fmt.Fprintf(s.Stdout, "protected: %t\n", r.Protected)
	fmt.Fprintf(s.Stdout, "revision:  %d\n", r.Revision)
	if r.Metadata != "" && r.Metadata != "{}" {
		fmt.Fprintf(s.Stdout, "metadata:  %s\n", r.Metadata)
	}
	fmt.Fprintln(s.Stdout)
	fmt.Fprintln(s.Stdout, r.Body)
	return ExitSuccess
}

func emitRuneSummaries(s *Streams, cmdName string, summaries []bdd.RuneSummary) int {
	if s.JSON {
		arr := NewJSONArray(s.Stdout)
		for _, r := range summaries {
			if err := arr.WriteItem(toRuneSummaryResult(r)); err != nil {
				s.Errorf("bdd: %s: %v\n", cmdName, err)
				return ExitOther
			}
		}
		if err := arr.Close(); err != nil {
			s.Errorf("bdd: %s: %v\n", cmdName, err)
			return ExitOther
		}
		return ExitSuccess
	}

	for _, r := range summaries {
		fmt.Fprintf(s.Stdout, "%s\t%s\t%s\tenabled=%t protected=%t rev=%d\n",
			r.Key, r.Kind, r.Title, r.Enabled, r.Protected, r.Revision)
	}
	return ExitSuccess
}
