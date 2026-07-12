package cli

import (
	"context"

	"github.com/viq111/bdd"
)

// runCardUpdate implements `bdd update <id> [--claim] [--title <t>]
// [--type <t>] [--status <s>] [--priority <n|Pn>] [--description <t>]
// [--reproduce <t>] [--design <t>] [--acceptance <t>] [--external-ref <t>]
// [--worktree <path>|--clear-worktree] [--add-label <l>]...
// [--remove-label <l>]... [--add-parent <id>]... [--remove-parent <id>]...
// [--add-child <id>]... [--remove-child <id>]...`.
func runCardUpdate(g GlobalFlags, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: update: card id is required\n")
		return ExitUsage
	}
	id := args[0]
	rest := args[1:]

	var claim bool
	var title, typ, status, priorityRaw, description, reproduce, design, acceptance, externalRef, worktree string
	var haveTitle, haveType, haveStatus, havePriority, haveDescription, haveReproduce, haveDesign, haveAcceptance, haveExternalRef, haveWorktree bool
	var clearWorktree bool
	var addLabels, removeLabels, addParents, removeParents, addChildren, removeChildren []string

	i := 0
	for i < len(rest) {
		arg := rest[i]
		name, inline, hasInline := cutFlagValue(arg)

		switch name {
		case "--claim":
			claim = true
			i++
			continue
		case "--clear-worktree":
			clearWorktree = true
			i++
			continue
		case "--title":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			title, haveTitle = val, true
			i += consumed
			continue
		case "--type":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			typ, haveType = val, true
			i += consumed
			continue
		case "--status":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			status, haveStatus = val, true
			i += consumed
			continue
		case "--priority":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			priorityRaw, havePriority = val, true
			i += consumed
			continue
		case "--description":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			description, haveDescription = val, true
			i += consumed
			continue
		case "--reproduce":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			reproduce, haveReproduce = val, true
			i += consumed
			continue
		case "--design":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			design, haveDesign = val, true
			i += consumed
			continue
		case "--acceptance":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			acceptance, haveAcceptance = val, true
			i += consumed
			continue
		case "--external-ref":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			externalRef, haveExternalRef = val, true
			i += consumed
			continue
		case "--worktree":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			worktree, haveWorktree = val, true
			i += consumed
			continue
		case "--add-label":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			addLabels = append(addLabels, val)
			i += consumed
			continue
		case "--remove-label":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			removeLabels = append(removeLabels, val)
			i += consumed
			continue
		case "--add-parent":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			addParents = append(addParents, val)
			i += consumed
			continue
		case "--remove-parent":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			removeParents = append(removeParents, val)
			i += consumed
			continue
		case "--add-child":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			addChildren = append(addChildren, val)
			i += consumed
			continue
		case "--remove-child":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: update: %v\n", err)
				return ExitUsage
			}
			removeChildren = append(removeChildren, val)
			i += consumed
			continue
		}

		s.Errorf("bdd: update: unknown flag %q\n", arg)
		return ExitUsage
	}

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
	// --claim's mutation is committed: a single `update` invocation must not
	// leave a partial, uncommitted-looking side effect behind.
	var in bdd.UpdateCard
	if anyFieldChange {
		in = bdd.UpdateCard{
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
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "update", s)
	if db == nil {
		return code
	}
	defer db.Close()

	var card *bdd.Card
	if claim {
		claimed, err := db.ClaimCard(ctx, id, actor)
		if err != nil {
			s.Errorf("bdd: update: %v\n", err)
			return ExitCode(err)
		}
		card = claimed
	}

	if anyFieldChange {
		updated, err := db.UpdateCard(ctx, id, in)
		if err != nil {
			s.Errorf("bdd: update: %v\n", err)
			return ExitCode(err)
		}
		card = updated
	}

	return emitCard(s, "update", toCardResult(card))
}
