package cli

import (
	"context"
	"fmt"
	"strings"
)

// DeleteResult is the JSON/human result of `bdd delete`.
type DeleteResult struct {
	ID              string   `json:"id"`
	RemovedParents  []string `json:"removed_parents"`
	RemovedChildren []string `json:"removed_children"`
}

// runCardDelete implements `bdd delete <id> --force`.
func runCardDelete(g GlobalFlags, args []string, s *Streams) int {
	var id string
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
			s.Errorf("bdd: delete: unknown flag %q\n", arg)
			return ExitUsage
		}
		if id != "" {
			s.Errorf("bdd: delete: unexpected argument %q\n", arg)
			return ExitUsage
		}
		id = arg
		i++
	}
	if id == "" {
		s.Errorf("bdd: delete: card id is required\n")
		return ExitUsage
	}
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
