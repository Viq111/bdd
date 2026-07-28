package cli

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/viq111/bdd"
)

// runCardUpdate implements `bdd update <id> [--claim] [--title <t>]
// [--type <t>] [--status <s>] [--priority <n|Pn>] [--description <t>]
// [--reproduce <t>] [--design <t>] [--acceptance <t>] [--external-ref <t>]
// [--worktree <path>|--clear-worktree] [--add-label <l>]...
// [--remove-label <l>]... [--add-parent <id>]... [--remove-parent <id>]...
// [--add-child <id>]... [--remove-child <id>]...`.
func runCardUpdate(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: update: card id is required\n")
		return ExitUsage
	}
	id := args[0]
	if len(args) > 1 {
		s.Errorf("bdd: update: unknown flag %q\n", args[1])
		return ExitUsage
	}

	fs := cmd.Flags()
	claim := flagBool(fs, "claim")
	title, haveTitle := flagString(fs, "title")
	typ, haveType := flagString(fs, "type")
	status, haveStatus := flagString(fs, "status")
	priorityRaw, havePriority := flagString(fs, "priority")
	description, haveDescription := flagString(fs, "description")
	reproduce, haveReproduce := flagString(fs, "reproduce")
	design, haveDesign := flagString(fs, "design")
	acceptance, haveAcceptance := flagString(fs, "acceptance")
	externalRef, haveExternalRef := flagString(fs, "external-ref")
	worktree, haveWorktree := flagString(fs, "worktree")
	clearWorktree := flagBool(fs, "clear-worktree")
	addLabels := flagStringSlice(fs, "add-label")
	removeLabels := flagStringSlice(fs, "remove-label")
	addParents := flagStringSlice(fs, "add-parent")
	removeParents := flagStringSlice(fs, "remove-parent")
	addChildren := flagStringSlice(fs, "add-child")
	removeChildren := flagStringSlice(fs, "remove-child")

	anyFieldChange := haveTitle || haveType || haveStatus || havePriority || haveDescription || haveReproduce ||
		haveDesign || haveAcceptance || haveExternalRef || haveWorktree || clearWorktree ||
		len(addLabels) > 0 || len(removeLabels) > 0 || len(addParents) > 0 || len(removeParents) > 0 ||
		len(addChildren) > 0 || len(removeChildren) > 0

	if !claim && !anyFieldChange {
		s.Errorf("bdd: update: at least one field to change is required\n")
		return ExitUsage
	}

	actor := ResolveActor(g.Actor)

	// Build (and fully validate) the UpdateCard input before touching the
	// database, so a malformed flag (e.g. --priority) is rejected before
	// anything is committed: a single `update` invocation, including one
	// that combines --claim with field changes, must not leave a partial,
	// uncommitted-looking side effect behind.
	in := bdd.UpdateCard{
		Claim:          claim,
		AddLabels:      addLabels,
		RemoveLabels:   removeLabels,
		AddParents:     addParents,
		RemoveParents:  removeParents,
		AddChildren:    addChildren,
		RemoveChildren: removeChildren,
		ClearWorktree:  clearWorktree,
		Actor:          actor,
	}
	if haveTitle {
		in.Title = &title
	}
	if haveType {
		t := bdd.CardType(typ)
		in.Type = &t
	}
	if haveStatus {
		st := bdd.Status(status)
		in.Status = &st
	}
	if havePriority {
		p, err := parsePriority(priorityRaw)
		if err != nil {
			s.Errorf("bdd: update: %v\n", err)
			return ExitUsage
		}
		in.Priority = &p
	}
	if haveDescription {
		in.Description = &description
	}
	if haveReproduce {
		in.Reproduction = &reproduce
	}
	if haveDesign {
		in.Design = &design
	}
	if haveAcceptance {
		in.Acceptance = &acceptance
	}
	if haveExternalRef {
		in.ExternalRef = &externalRef
	}
	if haveWorktree {
		in.Worktree = &worktree
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "update", s)
	if db == nil {
		return code
	}
	defer db.Close()

	var candidates []bdd.HookEvent
	if claim || haveStatus {
		candidates = append(candidates, bdd.HookEventStatusChange)
	}
	if len(addLabels) > 0 || len(removeLabels) > 0 {
		candidates = append(candidates, bdd.HookEventLabelChange)
	}
	var hs *hookSource
	if len(candidates) > 0 {
		hs = loadHookSource(ctx, db, g, s, actor, candidates...)
	}

	var card, pre *bdd.Card
	var err error
	if hs != nil {
		card, pre, err = db.UpdateCardWithPre(ctx, id, in)
	} else {
		card, err = db.UpdateCard(ctx, id, in)
	}
	if err != nil {
		s.Errorf("bdd: update: %v\n", err)
		return ExitCode(err)
	}

	if hs != nil && pre != nil {
		if pre.Status != card.Status {
			hs.fireStatusChange(ctx, s, card, string(pre.Status), string(card.Status))
		}
		if added, removed := labelDiff(pre.Labels, card.Labels); len(added) > 0 || len(removed) > 0 {
			hs.fireLabelChange(ctx, s, card, added, removed)
		}
	}

	return emitCard(s, "update", toCardResult(card))
}
