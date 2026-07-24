package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// DeleteResult is the JSON/human result of `bdd delete`.
type DeleteResult struct {
	ID              string   `json:"id"`
	RemovedParents  []string `json:"removed_parents"`
	RemovedChildren []string `json:"removed_children"`
}

// runCardDelete implements `bdd delete <id> --force`.
func runCardDelete(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: delete: card id is required\n")
		return ExitUsage
	}
	id := args[0]
	if len(args) > 1 {
		s.Errorf("bdd: delete: unexpected argument %q\n", args[1])
		return ExitUsage
	}
	force := flagBool(cmd.Flags(), "force")
	if !force {
		s.Errorf("bdd: delete: refusing to delete %s without --force\n", id)
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "delete", s)
	if db == nil {
		return code
	}
	defer db.Close()

	actor := ResolveActor(g.Actor)
	result, err := db.DeleteCard(ctx, id, actor, force)
	if err != nil {
		s.Errorf("bdd: delete: %v\n", err)
		return ExitCode(err)
	}

	out := DeleteResult{
		ID:              id,
		RemovedParents:  nonNilLabels(result.RemovedParents),
		RemovedChildren: nonNilLabels(result.RemovedChildren),
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(out); err != nil {
			s.Errorf("bdd: delete: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	if s.Silent {
		fmt.Fprintln(s.Stdout, id)
		return ExitSuccess
	}
	fmt.Fprintf(s.Stdout, "deleted %s\n", id)
	if len(out.RemovedParents) > 0 {
		fmt.Fprintf(s.Stdout, "removed parent edges: %s\n", strings.Join(out.RemovedParents, ", "))
	}
	if len(out.RemovedChildren) > 0 {
		fmt.Fprintf(s.Stdout, "removed child edges: %s\n", strings.Join(out.RemovedChildren, ", "))
	}
	return ExitSuccess
}
