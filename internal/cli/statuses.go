package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// StatusDefResult is one entry of the JSON/human result of `bdd statuses`.
type StatusDefResult struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	BuiltIn  bool   `json:"built_in"`
}

// runStatuses implements `bdd statuses`.
func runStatuses(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) != 0 {
		return reportUnknownArg(s, "statuses", args[0])
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "statuses", s)
	if db == nil {
		return code
	}
	defer db.Close()

	defs, err := db.Statuses(ctx)
	if err != nil {
		s.Errorf("bdd: statuses: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		arr := NewJSONArray(s.Stdout)
		for _, d := range defs {
			item := StatusDefResult{Name: string(d.Name), Category: string(d.Category), BuiltIn: d.BuiltIn}
			if err := arr.WriteItem(item); err != nil {
				s.Errorf("bdd: statuses: %v\n", err)
				return ExitOther
			}
		}
		if err := arr.Close(); err != nil {
			s.Errorf("bdd: statuses: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}

	for _, d := range defs {
		origin := "custom"
		if d.BuiltIn {
			origin = "built-in"
		}
		fmt.Fprintf(s.Stdout, "%-20s %-10s %s\n", d.Name, d.Category, origin)
	}
	return ExitSuccess
}
