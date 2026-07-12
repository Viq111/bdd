package cli

import (
	"context"
	"fmt"
)

// TypeDefResult is one entry of the JSON/human result of `bdd types`.
type TypeDefResult struct {
	Name    string `json:"name"`
	BuiltIn bool   `json:"built_in"`
}

// runTypes implements `bdd types`.
func runTypes(g GlobalFlags, args []string, s *Streams) int {
	if len(args) != 0 {
		s.Errorf("bdd: types: unexpected argument %q\n", args[0])
		return ExitUsage
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "types", s)
	if db == nil {
		return code
	}
	defer db.Close()

	defs, err := db.Types(ctx)
	if err != nil {
		s.Errorf("bdd: types: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		arr := NewJSONArray(s.Stdout)
		for _, d := range defs {
			item := TypeDefResult{Name: string(d.Name), BuiltIn: d.BuiltIn}
			if err := arr.WriteItem(item); err != nil {
				s.Errorf("bdd: types: %v\n", err)
				return ExitOther
			}
		}
		if err := arr.Close(); err != nil {
			s.Errorf("bdd: types: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}

	for _, d := range defs {
		origin := "custom"
		if d.BuiltIn {
			origin = "built-in"
		}
		fmt.Fprintf(s.Stdout, "%-20s %s\n", d.Name, origin)
	}
	return ExitSuccess
}
