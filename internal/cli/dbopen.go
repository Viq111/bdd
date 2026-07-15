package cli

import (
	"context"
	"errors"

	"github.com/viq111/bdd"
)

// KeyResult is the JSON result of a command whose only output is the key it
// operated on (bdd forget, bdd rune remove, and similar deletions).
type KeyResult struct {
	Key string `json:"key"`
}

// openDB opens the workspace database resolved by g, writing a diagnostic
// and returning the mapped exit code on failure. Callers check for a nil db
// to detect failure:
//
//	db, code := openDB(ctx, g, "remember", s)
//	if db == nil {
//		return code
//	}
//	defer db.Close()
func openDB(ctx context.Context, g GlobalFlags, cmdName string, s *Streams) (*bdd.DB, int) {
	db, err := bdd.Open(ctx, bdd.OpenOptions{Path: g.DBPath, Workspace: g.Workspace})
	if err != nil {
		// Workspace discovery (no explicit --db path) found no database
		// anywhere up the directory tree. Replace the raw "walking up
		// from ...: not found" wording with an actionable message; an
		// explicit --db path that doesn't exist is a different error
		// (g.DBPath != "") and keeps its own message untouched.
		if g.DBPath == "" && errors.Is(err, bdd.ErrNotFound) {
			s.Errorf("bdd: %s: bdd: no .bdd/bdd.sqlite found, init database with bdd init\n", cmdName)
		} else {
			s.Errorf("bdd: %s: %v\n", cmdName, err)
		}
		return nil, ExitCode(err)
	}
	return db, ExitSuccess
}
