// bdd-migration imports supported Beads data into bdd.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/viq111/bdd/tools/migrate/internal/bddsink"
	"github.com/viq111/bdd/tools/migrate/internal/mapping"
	"github.com/viq111/bdd/tools/migrate/internal/model"
	"github.com/viq111/bdd/tools/migrate/internal/sourcebd"
	"github.com/viq111/bdd/tools/migrate/internal/warnings"
)

const usage = "Usage: bdd-migration [--workspace <dir>] [--bd <path>] [--destination <path>] [--allow-unsupported-bd-version] [--version]\n"

type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }

type options struct {
	workspace, binary, destination string
	help, showVersion              bool
	allowUnsupportedBDVersion      bool
}

func main() { os.Exit(runMain(context.Background(), os.Args[1:], os.Stdout, os.Stderr)) }

// runMain keeps command-line exit policy separate from the import workflow.
func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args, stdout)
	if err != nil {
		if errors.As(err, new(usageError)) {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if opts.help {
		_, _ = io.WriteString(stdout, usage)
		return 0
	}
	if opts.showVersion {
		_, _ = fmt.Fprintf(stdout, "bdd-migration version %s (%s)\n", version, commit)
		return 0
	}
	warningsFound, err := run(ctx, opts, stderr)
	if rendered := warnings.Render(warningsFound); rendered != "" {
		fmt.Fprintln(stderr, rendered)
	}
	if err != nil {
		if errors.As(err, new(usageError)) {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote to %s\n", opts.destination)
	return 0
}

func parseArgs(args []string, stdout io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("bdd-migration", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.workspace, "workspace", "", "repository root")
	fs.StringVar(&o.binary, "bd", "bd", "beads executable")
	fs.StringVar(&o.destination, "destination", "", "destination database")
	fs.BoolVar(&o.help, "help", false, "print usage")
	fs.BoolVar(&o.help, "h", false, "print usage")
	fs.BoolVar(&o.showVersion, "version", false, "print version")
	fs.BoolVar(&o.allowUnsupportedBDVersion, "allow-unsupported-bd-version", false, "proceed even if the bd version's series is not in the supported allowlist")
	if err := fs.Parse(args); err != nil {
		return o, usageError{err}
	}
	if fs.NArg() != 0 {
		return o, usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	if o.help || o.showVersion {
		return o, nil
	}
	var err error
	if o.workspace == "" {
		o.workspace, err = os.Getwd()
	}
	if err != nil {
		return o, fmt.Errorf("resolve workspace: %w", err)
	}
	if o.workspace, err = canonicalPath(o.workspace); err != nil {
		return o, usageError{fmt.Errorf("resolve workspace: %w", err)}
	}
	if o.destination == "" {
		o.destination = filepath.Join(o.workspace, ".bdd", "bdd.sqlite")
	} else if !filepath.IsAbs(o.destination) {
		o.destination = filepath.Join(o.workspace, o.destination)
	}
	if o.destination, err = canonicalPath(o.destination); err != nil {
		return o, fmt.Errorf("resolve destination: %w", err)
	}
	return o, nil
}

// canonicalPath resolves symlinks in the existing prefix, so it also works for
// a destination that has not been created yet.
func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for missing := []string{}; ; {
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent, base := filepath.Dir(abs), filepath.Base(abs)
		if parent == abs {
			return "", err
		}
		missing, abs = append(missing, base), parent
	}
}

func run(ctx context.Context, o options, stderr io.Writer) ([]model.Warning, error) {
	if info, err := os.Stat(filepath.Join(o.workspace, ".beads")); err != nil || !info.IsDir() {
		if errors.Is(err, os.ErrNotExist) {
			return nil, usageError{fmt.Errorf("source workspace %s has no .beads directory", o.workspace)}
		}
		if err != nil {
			return nil, fmt.Errorf("inspect .beads: %w", err)
		}
		return nil, usageError{fmt.Errorf("source workspace %s has no .beads directory", o.workspace)}
	}
	runner := sourcebd.Runner{Binary: o.binary, Workspace: o.workspace}
	if _, err := runner.Version(ctx); err != nil {
		var unsupported *sourcebd.UnsupportedVersionError
		if errors.As(err, &unsupported) {
			if !o.allowUnsupportedBDVersion {
				return nil, usageError{err}
			}
			fmt.Fprintf(stderr, "warning: proceeding with unsupported bd version %q (supported: %s)\n", unsupported.Version, sourcebd.FormatSupportedSeries(unsupported.Supported))
		} else {
			return nil, fmt.Errorf("read bd version: %w", err)
		}
	}
	statuses, err := runner.Statuses(ctx)
	if err != nil {
		return nil, fmt.Errorf("read statuses: %w", err)
	}
	types, err := runner.Types(ctx)
	if err != nil {
		return nil, fmt.Errorf("read types: %w", err)
	}
	statusCustom, err := runner.Config(ctx, "status.custom")
	if err != nil {
		return nil, fmt.Errorf("read status.custom: %w", err)
	}
	typesCustom, err := runner.Config(ctx, "types.custom")
	if err != nil {
		return nil, fmt.Errorf("read types.custom: %w", err)
	}
	prefix, err := runner.Config(ctx, "issue-prefix")
	if err != nil {
		return nil, fmt.Errorf("read issue-prefix: %w", err)
	}
	export, err := runner.Export(ctx)
	if err != nil {
		return nil, fmt.Errorf("export source: %w", err)
	}
	records, err := sourcebd.ParseJSONL(bytes.NewReader(export.Stdout))
	if err != nil {
		return nil, usageError{err}
	}
	cfg, err := sourceConfig(statuses.Stdout, types.Stdout, statusCustom.Stdout, typesCustom.Stdout, prefix.Stdout)
	if err != nil {
		return nil, usageError{err}
	}
	plan, err := mapping.Map(records, cfg)
	if err != nil {
		return nil, usageError{err}
	}
	if plan.Workspace.IssuePrefix == "" {
		plan.Workspace.IssuePrefix, err = inferPrefix(records)
		if err != nil {
			return plan.Warnings, usageError{err}
		}
	}
	sinkWarnings, err := bddsink.ApplyWithWarnings(ctx, o.destination, plan.Workspace.IssuePrefix, plan)
	return append(plan.Warnings, sinkWarnings...), err
}

func sourceConfig(statusJSON, typesJSON, statusCustom, typesCustom, prefix []byte) (mapping.Config, error) {
	cfg := mapping.Config{StatusCategories: map[string]string{}, CustomTypes: map[string]bool{}, LegacyStatusCategories: map[string]string{}, IssuePrefix: configuredPrefix(prefix)}
	type status struct {
		Name     string `json:"name"`
		Category string `json:"category"`
	}
	var statusEnvelope struct {
		BuiltInStatuses []status `json:"built_in_statuses"`
		CustomStatuses  []status `json:"custom_statuses"`
	}
	statuses := []status{}
	if bytes.HasPrefix(bytes.TrimSpace(statusJSON), []byte("{")) {
		if err := json.Unmarshal(statusJSON, &statusEnvelope); err != nil {
			return cfg, fmt.Errorf("parse statuses: %w", err)
		}
		statuses = append(statusEnvelope.BuiltInStatuses, statusEnvelope.CustomStatuses...)
	} else if err := json.Unmarshal(statusJSON, &statuses); err != nil {
		return cfg, fmt.Errorf("parse statuses: %w", err)
	}
	for _, v := range statuses {
		if v.Name != "" && v.Category != "" {
			cfg.StatusCategories[v.Name] = v.Category
		}
	}
	type typ struct {
		Name    string `json:"name"`
		BuiltIn bool   `json:"built_in"`
	}
	var typeEnvelope struct {
		CoreTypes   []typ    `json:"core_types"`
		CustomTypes []string `json:"custom_types"`
	}
	types := []typ{}
	if bytes.HasPrefix(bytes.TrimSpace(typesJSON), []byte("{")) {
		if err := json.Unmarshal(typesJSON, &typeEnvelope); err != nil {
			return cfg, fmt.Errorf("parse types: %w", err)
		}
		for _, v := range typeEnvelope.CoreTypes {
			v.BuiltIn = true
			types = append(types, v)
		}
		for _, name := range typeEnvelope.CustomTypes {
			types = append(types, typ{Name: name})
		}
	} else if err := json.Unmarshal(typesJSON, &types); err != nil {
		return cfg, fmt.Errorf("parse types: %w", err)
	}
	for _, v := range types {
		if v.Name != "" && !v.BuiltIn {
			cfg.CustomTypes[v.Name] = true
		}
	}
	for _, entry := range strings.Split(strings.TrimSpace(string(statusCustom)), ",") {
		if entry == "" {
			continue
		}
		name, category, ok := strings.Cut(entry, ":")
		if !ok {
			name, category = entry, cfg.StatusCategories[entry]
		}
		if name == "" || category == "" {
			return cfg, fmt.Errorf("parse status.custom: invalid entry %q", entry)
		}
		cfg.LegacyStatusCategories[name] = category
	}
	for _, name := range strings.Split(strings.TrimSpace(string(typesCustom)), ",") {
		if name != "" {
			cfg.CustomTypes[name] = true
		}
	}
	return cfg, nil
}

func configuredPrefix(output []byte) string {
	v := strings.TrimSpace(string(output))
	if v == "" || strings.HasSuffix(v, "(not set)") {
		return ""
	}
	return v
}

// inferPrefix handles bd's supported default, where `config get issue-prefix`
// reports "(not set)" while issue IDs still carry their workspace prefix.
func inferPrefix(records []sourcebd.Record) (string, error) {
	for _, record := range records {
		issue, ok := record.(sourcebd.Issue)
		if !ok {
			continue
		}
		prefix, _, ok := strings.Cut(issue.ID, "-")
		if ok && validPrefix(prefix) {
			return prefix, nil
		}
	}
	return "", fmt.Errorf("read issue-prefix: not configured and no valid issue ID is available to infer it")
}

func validPrefix(prefix string) bool {
	if len(prefix) == 0 || len(prefix) > 32 {
		return false
	}
	for i, r := range prefix {
		if !(unicode.IsLower(r) || unicode.IsDigit(r) || r == '-') || (i == 0 && !unicode.IsLower(r)) {
			return false
		}
	}
	return true
}
