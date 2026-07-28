package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/viq111/bdd"
)

// runCardParents implements `bdd parents <id>`.
func runCardParents(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	return runCardEdgeList(g, args, s, "parents", func(ctx context.Context, db *bdd.DB, id string) ([]bdd.CardRef, error) {
		return db.Parents(ctx, id)
	})
}

// runCardChildren implements `bdd children <id>`.
func runCardChildren(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	return runCardEdgeList(g, args, s, "children", func(ctx context.Context, db *bdd.DB, id string) ([]bdd.CardRef, error) {
		return db.Children(ctx, id)
	})
}

func runCardEdgeList(g GlobalFlags, args []string, s *Streams, cmdName string, fetch func(context.Context, *bdd.DB, string) ([]bdd.CardRef, error)) int {
	if len(args) != 1 {
		s.Errorf("bdd: %s: expected exactly one card id argument\n", cmdName)
		return ExitUsage
	}
	id := args[0]

	ctx := context.Background()
	db, code := openDB(ctx, g, cmdName, s)
	if db == nil {
		return code
	}
	defer db.Close()

	refs, err := fetch(ctx, db, id)
	if err != nil {
		s.Errorf("bdd: %s: %v\n", cmdName, err)
		return ExitCode(err)
	}

	if s.JSON {
		arr := NewJSONArray(s.Stdout)
		for _, r := range refs {
			if err := arr.WriteItem(toCardRefResult(r)); err != nil {
				s.Errorf("bdd: %s: %v\n", cmdName, err)
				return ExitOther
			}
		}
		if err := arr.Close(); err != nil {
			s.Errorf("bdd: %s: %v\n", cmdName, err)
			return ExitOther
		}
		return ExitSuccess
	}
	if s.Silent {
		for _, r := range refs {
			fmt.Fprintln(s.Stdout, r.ID)
		}
		return ExitSuccess
	}
	for _, r := range refs {
		fmt.Fprintf(s.Stdout, "%s\t%s\t%s\t%s\n", r.ID, r.Type, r.Status, r.Title)
	}
	return ExitSuccess
}

// runCardLabel implements the `bdd label` group's fallback dispatch for a
// missing or unknown subcommand; matched subcommands (add, remove, list)
// are routed directly to their leaf by cobra and never reach here.
func runCardLabel(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: label: missing subcommand (add, remove, list)\n")
		return ExitUsage
	}
	s.Errorf("bdd: label: unknown subcommand %q\n", args[0])
	return ExitUsage
}

func runCardLabelMutate(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams, add bool) int {
	cmdName := "label remove"
	if add {
		cmdName = "label add"
	}
	if len(args) != 2 {
		s.Errorf("bdd: %s: expected a card id and a label argument\n", cmdName)
		return ExitUsage
	}
	id, label := args[0], args[1]

	ctx := context.Background()
	db, code := openDB(ctx, g, cmdName, s)
	if db == nil {
		return code
	}
	defer db.Close()

	actor := ResolveActor(g.Actor)
	in := bdd.UpdateCard{Actor: actor}
	if add {
		in.AddLabels = []string{label}
	} else {
		in.RemoveLabels = []string{label}
	}

	hs := loadHookSource(ctx, db, g, s, actor, bdd.HookEventLabelChange)

	var card, pre *bdd.Card
	var err error
	if hs != nil {
		card, pre, err = db.UpdateCardWithPre(ctx, id, in)
	} else {
		card, err = db.UpdateCard(ctx, id, in)
	}
	if err != nil {
		s.Errorf("bdd: %s: %v\n", cmdName, err)
		return ExitCode(err)
	}

	if hs != nil && pre != nil {
		if added, removed := labelDiff(pre.Labels, card.Labels); len(added) > 0 || len(removed) > 0 {
			hs.fireLabelChange(ctx, s, card, added, removed)
		}
	}

	return emitCard(s, cmdName, toCardResult(card))
}

func runCardLabelList(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) != 1 {
		s.Errorf("bdd: label list: expected exactly one card id argument\n")
		return ExitUsage
	}
	id := args[0]

	ctx := context.Background()
	db, code := openDB(ctx, g, "label list", s)
	if db == nil {
		return code
	}
	defer db.Close()

	card, err := db.GetCard(ctx, id)
	if err != nil {
		s.Errorf("bdd: label list: %v\n", err)
		return ExitCode(err)
	}
	labels := nonNilLabels(card.Labels)

	if s.JSON {
		arr := NewJSONArray(s.Stdout)
		for _, l := range labels {
			if err := arr.WriteItem(l); err != nil {
				s.Errorf("bdd: label list: %v\n", err)
				return ExitOther
			}
		}
		if err := arr.Close(); err != nil {
			s.Errorf("bdd: label list: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	for _, l := range labels {
		fmt.Fprintln(s.Stdout, l)
	}
	return ExitSuccess
}
