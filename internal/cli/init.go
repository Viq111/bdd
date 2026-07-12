package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/viq111/bdd"
)

// InitResult is the JSON/human result of `bdd init`.
type InitResult struct {
	Workspace     string `json:"workspace"`
	Database      string `json:"database"`
	Prefix        string `json:"prefix"`
	SchemaVersion int    `json:"schema_version"`
}

// runInit implements `bdd init [--prefix <prefix>] [path]`.
func runInit(g GlobalFlags, args []string, s *Streams) int {
	var prefix, path string

	i := 0
	for i < len(args) {
		arg := args[i]
		name, inline, hasInline := cutFlagValue(arg)

		if name == "--prefix" {
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: init: %v\n", err)
				return ExitUsage
			}
			prefix = val
			i += consumed
			continue
		}

		if strings.HasPrefix(arg, "-") {
			s.Errorf("bdd: init: unknown flag %q\n", arg)
			return ExitUsage
		}
		if path != "" {
			s.Errorf("bdd: init: unexpected argument %q\n", arg)
			return ExitUsage
		}
		path = arg
		i++
	}

	initOpts := bdd.InitOptions{Prefix: prefix}
	var derivedFrom string

	if g.DBPath != "" {
		absDB, err := filepath.Abs(g.DBPath)
		if err != nil {
			s.Errorf("bdd: init: %v\n", err)
			return ExitOther
		}
		initOpts.DBPath = absDB
		derivedFrom = workspaceDir(absDB)
	} else {
		workspace := g.Workspace
		if path != "" {
			workspace = path
		}
		if workspace == "" {
			wd, err := os.Getwd()
			if err != nil {
				s.Errorf("bdd: init: %v\n", err)
				return ExitOther
			}
			workspace = wd
		}

		absWorkspace, err := filepath.Abs(workspace)
		if err != nil {
			s.Errorf("bdd: init: %v\n", err)
			return ExitOther
		}
		initOpts.Workspace = absWorkspace
		derivedFrom = absWorkspace
	}

	if prefix == "" {
		prefix = derivePrefix(derivedFrom)
		initOpts.Prefix = prefix
	}

	ctx := context.Background()
	db, err := bdd.Init(ctx, initOpts)
	if err != nil {
		s.Errorf("bdd: init: %v\n", err)
		return ExitCode(err)
	}
	defer db.Close()

	onDisk, _, err := db.SchemaVersions(ctx)
	if err != nil {
		s.Errorf("bdd: init: %v\n", err)
		return ExitCode(err)
	}

	result := InitResult{
		Workspace:     workspaceDir(db.Path()),
		Database:      db.Path(),
		Prefix:        prefix,
		SchemaVersion: onDisk,
	}
	return emitInit(s, result)
}

func emitInit(s *Streams, r InitResult) int {
	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(r); err != nil {
			s.Errorf("bdd: init: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	if s.Silent {
		fmt.Fprintln(s.Stdout, r.Database)
		return ExitSuccess
	}
	fmt.Fprintf(s.Stdout, "Initialized bdd workspace at %s (prefix: %s)\n", r.Database, r.Prefix)
	return ExitSuccess
}

// derivePrefix derives a workspace ID prefix from a workspace directory
// name when --prefix is omitted: the lowercased base name, with any
// character outside [a-z0-9-] collapsed to '-', leading digits/hyphens and
// trailing hyphens trimmed, and truncated to bdd's 32-byte prefix limit.
// It falls back to "bdd" if that leaves nothing usable.
func derivePrefix(dir string) string {
	base := strings.ToLower(filepath.Base(dir))

	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}

	s := strings.Trim(b.String(), "-")
	for len(s) > 0 && s[0] >= '0' && s[0] <= '9' {
		s = s[1:]
	}
	s = strings.Trim(s, "-")

	const maxPrefixLen = 32
	if len(s) > maxPrefixLen {
		s = strings.TrimRight(s[:maxPrefixLen], "-")
	}

	if s == "" {
		return "bdd"
	}
	return s
}
