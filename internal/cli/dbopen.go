package cli

import (
	"context"

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
		s.Errorf("bdd: %s: %v\n", cmdName, err)
		return nil, ExitCode(err)
	}
	return db, ExitSuccess
}
