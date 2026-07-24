package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// runCardHistory implements `bdd history <id>`: the public read path for a
// card's internal audit trail (bd bdd-as31).
func runCardHistory(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) != 1 {
		s.Errorf("bdd: history: expected exactly one card id argument\n")
		return ExitUsage
	}
	id := args[0]

	ctx := context.Background()
	db, code := openDB(ctx, g, "history", s)
	if db == nil {
		return code
	}
	defer db.Close()

	events, err := db.Events(ctx, id)
	if err != nil {
		s.Errorf("bdd: history: %v\n", err)
		return ExitCode(err)
	}

	results := toEventResults(events)

	if s.JSON {
		arr := NewJSONArray(s.Stdout)
		for _, r := range results {
			if err := arr.WriteItem(r); err != nil {
				s.Errorf("bdd: history: %v\n", err)
				return ExitOther
			}
		}
		if err := arr.Close(); err != nil {
			s.Errorf("bdd: history: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	if s.Silent {
		for _, r := range results {
			fmt.Fprintln(s.Stdout, r.ID)
		}
		return ExitSuccess
	}
	for _, r := range results {
		actor := r.Actor
		if actor == "" {
			actor = "-"
		}
		fmt.Fprintf(s.Stdout, "[%d] %s rev=%d %s by %s %s\n",
			r.ID, r.CreatedAt, r.Revision, r.Action, sanitizeForTerminal(actor), sanitizeForTerminal(r.Payload))
	}
	return ExitSuccess
}
