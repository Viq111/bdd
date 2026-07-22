package cli

import (
	"context"
	"errors"

	"github.com/viq111/bdd"
)

// KeyResult is the JSON result of a command whose only output is the key it
// operated on (bdd memory remove, bdd rune remove, and similar deletions).
type KeyResult struct {
	Key string `json:"key"`
}

// openDB opens the workspace database resolved by g, writing a diagnostic
// and returning the mapped exit code on failure. Callers check for a nil db
// to detect failure:
//
//	db, code := openDB(ctx, g, "memory set", s)
//	if db == nil {
//		return code
//	}
//	defer db.Close()
func openDB(ctx context.Context, g GlobalFlags, cmdName string, s *Streams) (*bdd.DB, int) {
	db, err := bdd.Open(ctx, bdd.OpenOptions{Workspace: g.Workspace})
	if err != nil {
		// Workspace discovery found no database anywhere up the
		// directory tree. Replace the raw "walking up from ...: not
		// found" wording with an actionable message.
		if errors.Is(err, bdd.ErrNotFound) {
			s.Errorf("bdd: %s: bdd: no .bdd/bdd.sqlite found, init database with bdd init\n", cmdName)
		} else {
			s.Errorf("bdd: %s: %v\n", cmdName, err)
		}
		return nil, ExitCode(err)
	}
	return db, ExitSuccess
}
