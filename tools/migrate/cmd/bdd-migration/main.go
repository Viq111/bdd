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

	"github.com/viq111/bdd/tools/migrate/internal/bddsink"
	"github.com/viq111/bdd/tools/migrate/internal/mapping"
	"github.com/viq111/bdd/tools/migrate/internal/model"
	"github.com/viq111/bdd/tools/migrate/internal/sourcebd"
	"github.com/viq111/bdd/tools/migrate/internal/warnings"
)

const usage = "Usage: bdd-migration [--workspace <dir>] [--bd <path>] [--destination <path>]\n"

type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }

type options struct {
	workspace, binary, destination string
	help                           bool
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
	warningsFound, err := run(ctx, opts)
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
	if err := fs.Parse(args); err != nil {
		return o, usageError{err}
	}
	if fs.NArg() != 0 {
		return o, usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	if o.help {
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
		return o, usageError{fmt.Errorf("resolve destination: %w", err)}
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

func run(ctx context.Context, o options) ([]model.Warning, error) {
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
		if strings.HasPrefix(err.Error(), "unsupported bd version") {
			return nil, usageError{err}
		}
		return nil, fmt.Errorf("read bd version: %w", err)
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
	sinkWarnings, err := bddsink.ApplyWithWarnings(ctx, o.destination, plan.Workspace.IssuePrefix, plan)
	return append(plan.Warnings, sinkWarnings...), err
}

func sourceConfig(statusJSON, typesJSON, statusCustom, typesCustom, prefix []byte) (mapping.Config, error) {
	cfg := mapping.Config{StatusCategories: map[string]string{}, CustomTypes: map[string]bool{}, LegacyStatusCategories: map[string]string{}, IssuePrefix: strings.TrimSpace(string(prefix))}
	type status struct {
		Name     string `json:"name"`
		Category string `json:"category"`
	}
	var statuses struct {
		BuiltIn []status `json:"built_in_statuses"`
		Custom  []status `json:"custom_statuses"`
	}
	if err := json.Unmarshal(statusJSON, &statuses); err != nil {
		return cfg, fmt.Errorf("parse statuses: %w", err)
	}
	for _, v := range append(statuses.BuiltIn, statuses.Custom...) {
		if v.Name != "" && v.Category != "" {
			cfg.StatusCategories[v.Name] = v.Category
		}
	}
	var types struct {
		Core []struct {
			Name string `json:"name"`
		} `json:"core_types"`
		Custom []string `json:"custom_types"`
	}
	if err := json.Unmarshal(typesJSON, &types); err != nil {
		return cfg, fmt.Errorf("parse types: %w", err)
	}
	for _, v := range types.Custom {
		if v != "" {
			cfg.CustomTypes[v] = true
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
	if cfg.IssuePrefix == "" {
		return cfg, fmt.Errorf("read issue-prefix: empty value")
	}
	return cfg, nil
}
