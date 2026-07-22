package cli

import (
	"context"
	"fmt"
	"path/filepath"
)

// StatusResult is the JSON/human result of `bdd status`.
type StatusResult struct {
	Workspace            string  `json:"workspace"`
	Database             string  `json:"database"`
	Prefix               *string `json:"prefix"`
	SchemaVersion        int     `json:"schema_version"`
	CurrentSchemaVersion int     `json:"current_schema_version"`
	UpToDate             bool    `json:"up_to_date"`
	Upgraded             bool    `json:"upgraded"`
}

// runStatus implements `bdd status [--upgrade]`.
func runStatus(g GlobalFlags, args []string, s *Streams) int {
	upgrade := false
	for _, arg := range args {
		switch arg {
		case "--upgrade":
			upgrade = true
		default:
			s.Errorf("bdd: status: unknown argument %q\n", arg)
			return ExitUsage
		}
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "status", s)
	if db == nil {
		return code
	}
	defer db.Close()

	upgraded := false
	if upgrade {
		onDisk, current, err := db.SchemaVersions(ctx)
		if err != nil {
			s.Errorf("bdd: status: %v\n", err)
			return ExitCode(err)
		}
		if onDisk < current {
			if err := db.Upgrade(ctx); err != nil {
				s.Errorf("bdd: status: upgrade: %v\n", err)
				return ExitCode(err)
			}
			upgraded = true
		}
	}

	onDisk, current, err := db.SchemaVersions(ctx)
	if err != nil {
		s.Errorf("bdd: status: %v\n", err)
		return ExitCode(err)
	}

	result := StatusResult{
		Workspace:            workspaceDir(db.Path()),
		Database:             db.Path(),
		SchemaVersion:        onDisk,
		CurrentSchemaVersion: current,
		UpToDate:             onDisk >= current,
		Upgraded:             upgraded,
	}
	if onDisk > 0 {
		if prefix, err := db.Prefix(ctx); err == nil {
			result.Prefix = &prefix
		}
	}

	return emitStatus(s, result)
}

func emitStatus(s *Streams, r StatusResult) int {
	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(r); err != nil {
			s.Errorf("bdd: status: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	if s.Silent {
		fmt.Fprintln(s.Stdout, r.Database)
		return ExitSuccess
	}

	fmt.Fprintf(s.Stdout, "workspace: %s\n", r.Workspace)
	fmt.Fprintf(s.Stdout, "database:  %s\n", r.Database)
	if r.Prefix != nil {
		fmt.Fprintf(s.Stdout, "prefix:    %s\n", *r.Prefix)
	}
	if r.UpToDate {
		fmt.Fprintf(s.Stdout, "schema:    %d (up to date)\n", r.SchemaVersion)
	} else {
		fmt.Fprintf(s.Stdout, "schema:    %d (current build expects %d; run `bdd status --upgrade`)\n", r.SchemaVersion, r.CurrentSchemaVersion)
	}
	if r.Upgraded {
		fmt.Fprintln(s.Stdout, "upgraded:  yes")
	}
	return ExitSuccess
}

// workspaceDir derives the workspace directory a resolved database path
// belongs to: the parent of the fixed .bdd/bdd.sqlite layout, or the
// database's own directory if it doesn't follow that layout.
func workspaceDir(dbPath string) string {
	dir := filepath.Dir(dbPath)
	if filepath.Base(dir) == ".bdd" {
		return filepath.Dir(dir)
	}
	return dir
}
