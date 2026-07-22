package cli

import (
	"context"
)

// runCardShow implements `bdd show <id>`.
func runCardShow(g GlobalFlags, args []string, s *Streams) int {
	if arg, found := firstFlagArg(args); found {
		return reportUnknownArg(s, "show", arg)
	}
	if len(args) != 1 {
		s.Errorf("bdd: show: expected exactly one card id argument\n")
		return ExitUsage
	}
	id := args[0]

	ctx := context.Background()
	db, code := openDB(ctx, g, "show", s)
	if db == nil {
		return code
	}
	defer db.Close()

	card, err := db.GetCard(ctx, id)
	if err != nil {
		s.Errorf("bdd: show: %v\n", err)
		return ExitCode(err)
	}

	notes, err := db.Notes(ctx, id)
	if err != nil {
		s.Errorf("bdd: show: %v\n", err)
		return ExitCode(err)
	}

	result := ShowResult{CardResult: toCardResult(card), Notes: toNoteResults(notes)}
	return emitShow(s, result)
}
