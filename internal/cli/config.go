package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/viq111/bdd"
)

// ConfigEntryResult is the JSON/human result of `bdd config get` and one
// entry of `bdd config list`.
type ConfigEntryResult struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ConfigSetResult is the JSON/human result of `bdd config set`. Impact is
// only populated when key is status.custom and the write changed the
// category of a status still referenced by existing cards.
type ConfigSetResult struct {
	Key    string   `json:"key"`
	Value  string   `json:"value"`
	Impact []string `json:"impact,omitempty"`
}

// runConfig implements the `bdd config` group's fallback dispatch for a
// missing or unknown subcommand; matched subcommands (get, set, unset,
// list) are routed directly to their leaf by cobra and never reach here.
func runConfig(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) == 0 {
		s.Errorf("bdd: config: missing subcommand (get, set, unset, list)\n")
		return ExitUsage
	}
	s.Errorf("bdd: config: unknown subcommand %q\n", args[0])
	return ExitUsage
}

func runConfigGet(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) != 1 {
		s.Errorf("bdd: config get: expected exactly one key argument\n")
		return ExitUsage
	}
	key := args[0]

	ctx := context.Background()
	db, code := openDB(ctx, g, "config get", s)
	if db == nil {
		return code
	}
	defer db.Close()

	value, err := db.ConfigGet(ctx, key)
	if err != nil {
		s.Errorf("bdd: config get: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(ConfigEntryResult{Key: key, Value: value}); err != nil {
			s.Errorf("bdd: config get: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	fmt.Fprintln(s.Stdout, value)
	return ExitSuccess
}

func runConfigList(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) != 0 {
		return reportUnknownArg(s, "config list", args[0])
	}

	ctx := context.Background()
	db, code := openDB(ctx, g, "config list", s)
	if db == nil {
		return code
	}
	defer db.Close()

	entries, err := db.ConfigList(ctx)
	if err != nil {
		s.Errorf("bdd: config list: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		arr := NewJSONArray(s.Stdout)
		for _, e := range entries {
			if err := arr.WriteItem(ConfigEntryResult{Key: e.Key, Value: e.Value}); err != nil {
				s.Errorf("bdd: config list: %v\n", err)
				return ExitOther
			}
		}
		if err := arr.Close(); err != nil {
			s.Errorf("bdd: config list: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}

	for _, e := range entries {
		fmt.Fprintf(s.Stdout, "%s=%s\n", e.Key, e.Value)
	}
	return ExitSuccess
}

func runConfigSet(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) != 2 {
		s.Errorf("bdd: config set: expected a key and a value argument\n")
		return ExitUsage
	}
	key, value := args[0], args[1]

	ctx := context.Background()
	db, code := openDB(ctx, g, "config set", s)
	if db == nil {
		return code
	}
	defer db.Close()

	var oldValue string
	if key == bdd.ConfigKeyStatusCustom {
		oldValue, _ = db.ConfigGet(ctx, key) // best-effort; ErrNotFound leaves oldValue empty
	}

	actor := ResolveActor(g.Actor)
	if err := db.ConfigSet(ctx, key, value, actor); err != nil {
		s.Errorf("bdd: config set: %v\n", err)
		return ExitCode(err)
	}

	var impact []string
	if key == bdd.ConfigKeyStatusCustom {
		impact = statusCategoryImpact(ctx, db, oldValue, value)
	}

	if s.JSON {
		result := ConfigSetResult{Key: key, Value: value, Impact: impact}
		if err := NewJSONEncoder(s.Stdout).Object(result); err != nil {
			s.Errorf("bdd: config set: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	if s.Silent {
		fmt.Fprintln(s.Stdout, value)
		return ExitSuccess
	}
	fmt.Fprintf(s.Stdout, "%s=%s\n", key, value)
	for _, line := range impact {
		fmt.Fprintf(s.Stdout, "impact: %s\n", line)
	}
	return ExitSuccess
}

func runConfigUnset(g GlobalFlags, cmd *cobra.Command, args []string, s *Streams) int {
	if len(args) != 1 {
		s.Errorf("bdd: config unset: expected exactly one key argument\n")
		return ExitUsage
	}
	key := args[0]

	ctx := context.Background()
	db, code := openDB(ctx, g, "config unset", s)
	if db == nil {
		return code
	}
	defer db.Close()

	actor := ResolveActor(g.Actor)
	if err := db.ConfigUnset(ctx, key, actor); err != nil {
		s.Errorf("bdd: config unset: %v\n", err)
		return ExitCode(err)
	}

	if s.JSON {
		if err := NewJSONEncoder(s.Stdout).Object(KeyResult{Key: key}); err != nil {
			s.Errorf("bdd: config unset: %v\n", err)
			return ExitOther
		}
		return ExitSuccess
	}
	fmt.Fprintln(s.Stdout, key)
	return ExitSuccess
}

// statusCategoryImpact reports, for every status present in both the old and
// new status.custom value whose category changed, how many existing cards
// currently hold that status: a custom status silently switching to (or out
// of) the active category can unexpectedly change which cards dispatch.
// Parsing here is best-effort and only for display; by the time this runs,
// ConfigSet has already validated and applied the authoritative
// status.custom grammar.
func statusCategoryImpact(ctx context.Context, db *bdd.DB, oldValue, newValue string) []string {
	oldCats := parseNameCategoryLoose(oldValue)
	newCats := parseNameCategoryLoose(newValue)

	names := make([]string, 0, len(oldCats))
	for name := range oldCats {
		names = append(names, name)
	}
	sort.Strings(names)

	var impact []string
	for _, name := range names {
		newCat, stillDefined := newCats[name]
		if !stillDefined || newCat == oldCats[name] {
			continue
		}

		cards, err := db.ListCards(ctx, bdd.ListOptions{Statuses: []bdd.Status{bdd.Status(name)}})
		if err != nil || len(cards) == 0 {
			continue
		}
		impact = append(impact, fmt.Sprintf("status %q category changed from %s to %s: %d existing card(s) affected", name, oldCats[name], newCat, len(cards)))
	}
	return impact
}

// parseNameCategoryLoose parses the status.custom "name:category,..."
// grammar leniently, skipping malformed entries instead of failing: it only
// feeds a best-effort display preview, not validation.
func parseNameCategoryLoose(value string) map[string]string {
	out := map[string]string{}
	value = strings.TrimSpace(value)
	if value == "" {
		return out
	}
	for _, tok := range strings.Split(value, ",") {
		parts := strings.SplitN(strings.TrimSpace(tok), ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		category := strings.TrimSpace(parts[1])
		if name == "" {
			continue
		}
		out[name] = category
	}
	return out
}
