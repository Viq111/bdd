package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/viq111/bdd"
)

// parseLimit parses a --limit value as a non-negative integer.
func parseLimit(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("must be a non-negative integer, got %q", raw)
	}
	return n, nil
}

// runCardList implements `bdd list [--status <s>]... [--status-category
// <c>]... [--type <t>]... [--label <l>]... [--all] [--parent <id>]
// [--child <id>] [--description-like <text>] [--sort <field>] [--reverse]
// [--limit <n>]`.
func runCardList(g GlobalFlags, args []string, s *Streams) int {
	var statuses, categories, types, labels []string
	var parent, child, descLike, sortField string
	var reverse, all bool
	var limitRaw string
	var haveLimit bool

	i := 0
	for i < len(args) {
		arg := args[i]
		name, inline, hasInline := cutFlagValue(arg)

		switch name {
		case "--status":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: list: %v\n", err)
				return ExitUsage
			}
			statuses = append(statuses, val)
			i += consumed
			continue
		case "--status-category":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: list: %v\n", err)
				return ExitUsage
			}
			categories = append(categories, val)
			i += consumed
			continue
		case "--type":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: list: %v\n", err)
				return ExitUsage
			}
			types = append(types, val)
			i += consumed
			continue
		case "--label":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: list: %v\n", err)
				return ExitUsage
			}
			labels = append(labels, val)
			i += consumed
			continue
		case "--parent":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: list: %v\n", err)
				return ExitUsage
			}
			parent = val
			i += consumed
			continue
		case "--child":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: list: %v\n", err)
				return ExitUsage
			}
			child = val
			i += consumed
			continue
		case "--description-like":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: list: %v\n", err)
				return ExitUsage
			}
			descLike = val
			i += consumed
			continue
		case "--sort":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: list: %v\n", err)
				return ExitUsage
			}
			sortField = val
			i += consumed
			continue
		case "--reverse":
			reverse = true
			i++
			continue
		case "--all":
			all = true
			i++
			continue
		case "--limit":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: list: %v\n", err)
				return ExitUsage
			}
			limitRaw, haveLimit = val, true
			i += consumed
			continue
		default:
			return reportUnknownArg(s, "list", arg)
		}
	}

	limit := 0
	if haveLimit {
		n, err := parseLimit(limitRaw)
		if err != nil {
			s.Errorf("bdd: list: --limit %v\n", err)
			return ExitUsage
		}
		limit = n
	}

	opts := bdd.ListOptions{
		Statuses:         toStatuses(statuses),
		StatusCategories: toStatusCategories(categories),
		Types:            toCardTypes(types),
		Labels:           labels,
		All:              all,
		Parent:           parent,
		Child:            child,
		DescriptionLike:  descLike,
		Sort:             sortField,
		Reverse:          reverse,
		Limit:            limit,
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "list", s)
	if db == nil {
		return code
	}
	defer db.Close()

	cards, err := db.ListCards(ctx, opts)
	if err != nil {
		s.Errorf("bdd: list: %v\n", err)
		return ExitCode(err)
	}
	return emitCardSummaries(s, "list", cards)
}

// runCardSearch implements `bdd search <query> [--status <s>]... [--all]
// [--label <l>]... [--limit <n>]`.
func runCardSearch(g GlobalFlags, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: search: query is required\n")
		return ExitUsage
	}
	if strings.HasPrefix(args[0], "-") {
		return reportUnknownArg(s, "search", args[0])
	}
	query := args[0]
	rest := args[1:]

	var statuses, labels []string
	var all bool
	var limitRaw string
	var haveLimit bool

	i := 0
	for i < len(rest) {
		arg := rest[i]
		name, inline, hasInline := cutFlagValue(arg)

		switch name {
		case "--status":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: search: %v\n", err)
				return ExitUsage
			}
			statuses = append(statuses, val)
			i += consumed
			continue
		case "--label":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: search: %v\n", err)
				return ExitUsage
			}
			labels = append(labels, val)
			i += consumed
			continue
		case "--all":
			all = true
			i++
			continue
		case "--limit":
			val, consumed, err := flagValue(name, inline, hasInline, rest, i)
			if err != nil {
				s.Errorf("bdd: search: %v\n", err)
				return ExitUsage
			}
			limitRaw, haveLimit = val, true
			i += consumed
			continue
		default:
			return reportUnknownArg(s, "search", arg)
		}
	}

	limit := 0
	if haveLimit {
		n, err := parseLimit(limitRaw)
		if err != nil {
			s.Errorf("bdd: search: --limit %v\n", err)
			return ExitUsage
		}
		limit = n
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "search", s)
	if db == nil {
		return code
	}
	defer db.Close()

	cards, err := db.SearchCards(ctx, bdd.SearchOptions{
		Query:    query,
		Statuses: toStatuses(statuses),
		All:      all,
		Labels:   labels,
		Limit:    limit,
	})
	if err != nil {
		s.Errorf("bdd: search: %v\n", err)
		return ExitCode(err)
	}
	return emitCardSummaries(s, "search", cards)
}

// ReadyExplainResult is the JSON/human result of one card's evaluation
// under `bdd ready --explain`.
type ReadyExplainResult struct {
	ID      string   `json:"id"`
	Ready   bool     `json:"ready"`
	Reasons []string `json:"reasons"`
}

// runCardReady implements `bdd ready [--label <l>]... [--limit <n>]
// [--explain [<id>]]`.
func runCardReady(g GlobalFlags, args []string, s *Streams) int {
	var labels []string
	var limitRaw string
	var haveLimit bool
	var explain bool
	var positional []string

	i := 0
	for i < len(args) {
		arg := args[i]
		name, inline, hasInline := cutFlagValue(arg)

		switch name {
		case "--label":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: ready: %v\n", err)
				return ExitUsage
			}
			labels = append(labels, val)
			i += consumed
			continue
		case "--limit":
			val, consumed, err := flagValue(name, inline, hasInline, args, i)
			if err != nil {
				s.Errorf("bdd: ready: %v\n", err)
				return ExitUsage
			}
			limitRaw, haveLimit = val, true
			i += consumed
			continue
		case "--explain":
			explain = true
			i++
			continue
		}

		if strings.HasPrefix(arg, "-") {
			s.Errorf("bdd: ready: unknown flag %q\n", arg)
			return ExitUsage
		}
		positional = append(positional, arg)
		i++
	}

	if len(positional) > 1 {
		s.Errorf("bdd: ready: unexpected argument %q\n", positional[1])
		return ExitUsage
	}
	if len(positional) == 1 && !explain {
		s.Errorf("bdd: ready: unexpected argument %q (use --explain %s)\n", positional[0], positional[0])
		return ExitUsage
	}
	var explainID string
	if len(positional) == 1 {
		explainID = positional[0]
	}

	limit := 0
	if haveLimit {
		n, err := parseLimit(limitRaw)
		if err != nil {
			s.Errorf("bdd: ready: --limit %v\n", err)
			return ExitUsage
		}
		limit = n
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "ready", s)
	if db == nil {
		return code
	}
	defer db.Close()

	if explain {
		return runReadyExplain(ctx, db, s, explainID, labels, limit)
	}

	cards, err := db.ReadyCards(ctx, bdd.ReadyOptions{Labels: labels, Limit: limit})
	if err != nil {
		s.Errorf("bdd: ready: %v\n", err)
		return ExitCode(err)
	}
	return emitCardSummaries(s, "ready", cards)
}

// runReadyExplain evaluates the readiness predicate against id (if
// non-empty) or every card matching labels otherwise, printing every
// exclusion reason (plan section 16).
func runReadyExplain(ctx context.Context, db *bdd.DB, s *Streams, id string, labels []string, limit int) int {
	var targets []string
	if id != "" {
		targets = []string{id}
	} else {
		cards, err := db.ListCards(ctx, bdd.ListOptions{Labels: labels, Limit: limit})
		if err != nil {
			s.Errorf("bdd: ready: %v\n", err)
			return ExitCode(err)
		}
		for _, c := range cards {
			targets = append(targets, c.ID)
		}
	}

	results := make([]ReadyExplainResult, 0, len(targets))
	for _, tid := range targets {
		reasons, err := db.ExplainReady(ctx, tid)
		if err != nil {
			s.Errorf("bdd: ready: %v\n", err)
			return ExitCode(err)
		}
		results = append(results, ReadyExplainResult{ID: tid, Ready: len(reasons) == 0, Reasons: reasons})
	}

	if s.JSON {
		if id != "" {
			if err := NewJSONEncoder(s.Stdout).Object(results[0]); err != nil {
				s.Errorf("bdd: ready: %v\n", err)
				return ExitOther
			}
			return ExitSuccess
		}
		arr := NewJSONArray(s.Stdout)
		for _, r := range results {
			if err := arr.WriteItem(r); err != nil {
				s.Errorf("bdd: ready: %v\n", err)
				return ExitOther
			}
		}
		if err := arr.Close(); err != nil {
			s.Errorf("bdd: ready: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}

	for _, r := range results {
		if r.Ready {
			fmt.Fprintf(s.Stdout, "%s: ready\n", r.ID)
			continue
		}
		fmt.Fprintf(s.Stdout, "%s: %s\n", r.ID, sanitizeForTerminal(strings.Join(r.Reasons, "; ")))
	}
	return ExitSuccess
}

func toStatuses(ss []string) []bdd.Status {
	if len(ss) == 0 {
		return nil
	}
	out := make([]bdd.Status, len(ss))
	for i, v := range ss {
		out[i] = bdd.Status(v)
	}
	return out
}

func toStatusCategories(cs []string) []bdd.StatusCategory {
	if len(cs) == 0 {
		return nil
	}
	out := make([]bdd.StatusCategory, len(cs))
	for i, v := range cs {
		out[i] = bdd.StatusCategory(v)
	}
	return out
}

func toCardTypes(ts []string) []bdd.CardType {
	if len(ts) == 0 {
		return nil
	}
	out := make([]bdd.CardType, len(ts))
	for i, v := range ts {
		out[i] = bdd.CardType(v)
	}
	return out
}
