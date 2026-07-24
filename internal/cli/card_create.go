package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/viq111/bdd"
)

// createTextField describes one of CreateCard's optional pointer-typed text
// fields and the CLI flag names that populate it: --<flag>, --<flag>-file,
// and (when unambiguous) --stdin.
type createTextField struct {
	field string // CreateCard struct field name (also the ValidationError field name)
	flag  string // CLI flag name
}

var createTextFields = []createTextField{
	{"description", "description"},
	{"reproduction", "reproduce"},
	{"design", "design"},
	{"acceptance", "acceptance"},
}

// runCardCreate implements `bdd create [title] [--title <title>] --type
// <type> [--priority <n|Pn>] [--description <text>|--description-file
// <path>] [--reproduce ...] [--design ...] [--acceptance ...] [--worktree
// <path>] [--label <label>]... [--parent <id>]... [--notes <text>]
// [--stdin]`.
func runCardCreate(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	fs := cmd.Flags()

	titleFlag, haveTitleFlag := flagString(fs, "title")
	typ, _ := flagString(fs, "type")
	priorityRaw, havePriority := flagString(fs, "priority")
	worktree, _ := flagString(fs, "worktree")
	notes, haveNotes := flagString(fs, "notes")
	labels := flagStringSlice(fs, "label")
	parents := flagStringSlice(fs, "parent")
	stdin := flagBool(fs, "stdin")

	var titlePositional string
	var haveTitlePositional bool
	if len(args) > 0 {
		titlePositional, haveTitlePositional = args[0], true
	}
	if len(args) > 1 {
		s.Errorf("bdd: create: unexpected argument %q\n", args[1])
		return ExitUsage
	}

	if haveTitleFlag && haveTitlePositional {
		s.Errorf("bdd: create: cannot combine a positional title and --title\n")
		return ExitUsage
	}
	title := titleFlag
	if haveTitlePositional {
		title = titlePositional
	}

	fieldText := map[string]string{}
	for _, tf := range createTextFields {
		value, haveValue := flagString(fs, tf.flag)
		file, haveFile := flagString(fs, tf.flag+"-file")
		if haveValue && haveFile {
			s.Errorf("bdd: create: cannot combine --%s and --%s-file\n", tf.flag, tf.flag)
			return ExitUsage
		}
		if haveFile {
			data, err := os.ReadFile(file)
			if err != nil {
				s.Errorf("bdd: create: reading %s: %v\n", file, err)
				return ExitOther
			}
			fieldText[tf.field] = string(data)
		} else if haveValue {
			fieldText[tf.field] = value
		}
	}

	if stdin {
		var unset []string
		for _, f := range requiredTextFieldsForType(typ) {
			if _, ok := fieldText[f]; !ok {
				unset = append(unset, f)
			}
		}
		if len(unset) != 1 {
			s.Errorf("bdd: create: --stdin is ambiguous for type %q: %d required text field(s) still unset; supply --description/--reproduce/--design/--acceptance directly\n", typ, len(unset))
			return ExitUsage
		}
		data, err := io.ReadAll(s.Stdin)
		if err != nil {
			s.Errorf("bdd: create: reading stdin: %v\n", err)
			return ExitOther
		}
		fieldText[unset[0]] = string(data)
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "create", s)
	if db == nil {
		return code
	}
	defer db.Close()

	in := bdd.CreateCard{
		Title:     title,
		Type:      bdd.CardType(typ),
		Labels:    labels,
		Parents:   parents,
		CreatedBy: ResolveActor(g.Actor),
	}
	if havePriority {
		p, err := parsePriority(priorityRaw)
		if err != nil {
			s.Errorf("bdd: create: %v\n", err)
			return ExitUsage
		}
		in.Priority = &p
	}
	if worktree != "" {
		in.Worktree = &worktree
	}
	if haveNotes {
		in.Notes = &notes
	}
	for field, text := range fieldText {
		text := text
		switch field {
		case "description":
			in.Description = &text
		case "reproduction":
			in.Reproduction = &text
		case "design":
			in.Design = &text
		case "acceptance":
			in.Acceptance = &text
		}
	}

	card, err := db.CreateCard(ctx, in)
	if err != nil {
		var verr *bdd.ValidationError
		if errors.As(err, &verr) {
			if verr.Detail != "" {
				// A supplied-but-invalid value: verr.Fields lists the field
				// name, but "missing required field(s)" would mislead the
				// caller into thinking they omitted a flag they did pass.
				s.Errorf("bdd: create: %s\n", verr.Detail)
				return ExitUsage
			}
			s.Errorf("bdd: create: missing required field(s): %s\n", strings.Join(verr.Fields, ", "))
			if hint := bypassHint(verr.Fields); hint != "" {
				s.Errorf("bdd: create: explicitly pass %s to acknowledge\n", hint)
			}
			return ExitUsage
		}
		s.Errorf("bdd: create: %v\n", err)
		return ExitCode(err)
	}

	return emitCard(s, "create", toCardResult(card))
}

// requiredTextFieldsForType mirrors CreateCard's required-field matrix
// (mutation.go) for the pointer-typed text fields only, so --stdin can tell
// whether exactly one of them is still missing for typ.
func requiredTextFieldsForType(typ string) []string {
	switch bdd.CardType(typ) {
	case bdd.CardTypeBug:
		return []string{"reproduction", "acceptance"}
	case bdd.CardTypeTask, bdd.CardTypeFeature, bdd.CardTypeEpic:
		return []string{"acceptance"}
	case bdd.CardTypeDecision:
		return []string{"description", "design"}
	default:
		return nil
	}
}

// bypassHint renders the "pass --reproduce \"\" to acknowledge" hint for
// every missing pointer-typed field in fields (title/type have no such
// bypass, since they cannot be satisfied by an explicitly empty value).
func bypassHint(fields []string) string {
	flagFor := map[string]string{
		"description":  "--description",
		"reproduction": "--reproduce",
		"design":       "--design",
		"acceptance":   "--acceptance",
	}
	var hints []string
	for _, f := range fields {
		if flag, ok := flagFor[f]; ok {
			hints = append(hints, fmt.Sprintf(`%s ""`, flag))
		}
	}
	return strings.Join(hints, " and ")
}
