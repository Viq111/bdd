package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
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

// defaultListLimit is applied when --limit is not passed to list, search,
// or ready. An explicit --limit 0 remains the opt-in to unlimited results.
const defaultListLimit = 20

// resolveLimit applies parseLimit to raw when haveLimit is true, otherwise
// defaultListLimit, reporting a "bdd: <cmdName>: --limit ..." usage error on
// a malformed value.
func resolveLimit(s *Streams, cmdName string, raw string, haveLimit bool) (int, bool) {
	if !haveLimit {
		return defaultListLimit, true
	}
	n, err := parseLimit(raw)
	if err != nil {
		s.Errorf("bdd: %s: --limit %v\n", cmdName, err)
		return 0, false
	}
	return n, true
}

// runCardList implements `bdd list [--status <s>]... [--status-category
// <c>]... [--type <t>]... [--label <l>]... [--all] [--parent <id>]
// [--child <id>] [--description-like <text>] [--sort <field>] [--reverse]
// [--limit <n>]`.
func runCardList(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) > 0 {
		return reportUnknownArg(s, "list", args[0])
	}

	fs := cmd.Flags()
	statuses := flagStringSlice(fs, "status")
	categories := flagStringSlice(fs, "status-category")
	types := flagStringSlice(fs, "type")
	labels := flagStringSlice(fs, "label")
	all := flagBool(fs, "all")
	parent, _ := flagString(fs, "parent")
	child, _ := flagString(fs, "child")
	descLike, _ := flagString(fs, "description-like")
	sortField, _ := flagString(fs, "sort")
	reverse := flagBool(fs, "reverse")
	limitRaw, haveLimit := flagString(fs, "limit")

	limit, ok := resolveLimit(s, "list", limitRaw, haveLimit)
	if !ok {
		return ExitUsage
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
func runCardSearch(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: search: query is required\n")
		return ExitUsage
	}
	query := args[0]
	if len(args) > 1 {
		return reportUnknownArg(s, "search", args[1])
	}

	fs := cmd.Flags()
	statuses := flagStringSlice(fs, "status")
	all := flagBool(fs, "all")
	labels := flagStringSlice(fs, "label")
	limitRaw, haveLimit := flagString(fs, "limit")

	limit, ok := resolveLimit(s, "search", limitRaw, haveLimit)
	if !ok {
		return ExitUsage
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
func runCardReady(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	fs := cmd.Flags()
	labels := flagStringSlice(fs, "label")
	limitRaw, haveLimit := flagString(fs, "limit")
	explain := flagBool(fs, "explain")

	if len(args) > 1 {
		s.Errorf("bdd: ready: unexpected argument %q\n", args[1])
		return ExitUsage
	}
	if len(args) == 1 && !explain {
		s.Errorf("bdd: ready: unexpected argument %q (use --explain %s)\n", args[0], args[0])
		return ExitUsage
	}
	var explainID string
	if len(args) == 1 {
		explainID = args[0]
	}

	limit, ok := resolveLimit(s, "ready", limitRaw, haveLimit)
	if !ok {
		return ExitUsage
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
// exclusion reason.
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
