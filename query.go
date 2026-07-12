package bdd

import (
	"context"
	"fmt"
	"strings"
)

// ListOptions filters and orders ListCards. An empty Statuses selection
// defaults to every non-done-category card. Label filters AND-combine.
type ListOptions struct {
	Statuses         []Status
	StatusCategories []StatusCategory
	Types            []CardType
	Labels           []string

	Parent string // card ID: only children of this card
	Child  string // card ID: only parents of this card

	DescriptionLike string

	Sort    string // field name to sort by
	Reverse bool
	Limit   int // 0 means unlimited
}

// SearchOptions configures SearchCards. Query is matched case-insensitively
// against ID, title, description, reproduction, design, acceptance,
// external reference, worktree, and note text. Results are ordered by
// updated_at descending, then ID ascending.
type SearchOptions struct {
	Query string

	// Statuses restricts results to the given statuses. All is ignored
	// when Statuses is non-empty.
	Statuses []Status
	// All includes cards of every status, including done-category ones,
	// when Statuses is empty.
	All bool

	Labels []string
	Limit  int // 0 means unlimited
}

// ReadyOptions configures ReadyCards. A card is ready exactly when:
//  1. its status category is active,
//  2. it is dispatchable,
//  3. its assignee is empty,
//  4. it lacks the "human" label,
//  5. every parent has a done-category status, and
//  6. it matches every requested label filter (AND).
//
// Results are ordered by priority ascending, then created_at ascending,
// then ID.
type ReadyOptions struct {
	Labels []string
	Limit  int // 0 means unlimited
}

// listSortColumns maps a ListOptions.Sort field name to its underlying SQL
// column. "id" is its own tie-breaker; every other field sorts by (column,
// id) so results stay deterministic.
var listSortColumns = map[string]string{
	"id":         "id",
	"title":      "title",
	"status":     "status",
	"priority":   "priority",
	"type":       "card_type",
	"created_at": "created_at",
	"updated_at": "updated_at",
}

// listOrderBy resolves a ListOptions.Sort field (defaulting to "priority")
// and Reverse flag to an ORDER BY clause.
func listOrderBy(sortField string, reverse bool) (string, error) {
	field := sortField
	if field == "" {
		field = "priority"
	}
	col, ok := listSortColumns[field]
	if !ok {
		return "", fmt.Errorf("bdd: list cards: unknown sort field %q: %w", sortField, ErrInvalidArgument)
	}

	dir := "ASC"
	if reverse {
		dir = "DESC"
	}
	if col == "id" {
		return "id " + dir, nil
	}
	return col + " " + dir + ", id " + dir, nil
}

func validateListOptions(opts ListOptions) error {
	if !validateLabels(opts.Labels) {
		return fmt.Errorf("bdd: list cards: labels must be non-empty, valid UTF-8, and at most %d bytes: %w", MaxLabelBytes, ErrInvalidArgument)
	}
	if opts.Limit < 0 {
		return fmt.Errorf("bdd: list cards: limit must be >= 0: %w", ErrInvalidArgument)
	}
	return nil
}

// ListCards returns cards matching opts. It returns the core Card fields
// plus labels, without expanding notes or edges; use GetCard for the
// full-fat read of a single card.
func (db *DB) ListCards(ctx context.Context, opts ListOptions) ([]Card, error) {
	if err := validateListOptions(opts); err != nil {
		return nil, err
	}
	if err := db.ready(); err != nil {
		return nil, err
	}

	var conds []string
	var args []any

	switch {
	case len(opts.Statuses) > 0:
		conds = append(conds, "status IN ("+placeholders(len(opts.Statuses))+")")
		args = append(args, toAnySlice(opts.Statuses)...)
		if len(opts.StatusCategories) > 0 {
			conds = append(conds, "status IN (SELECT name FROM status_definitions WHERE category IN ("+placeholders(len(opts.StatusCategories))+"))")
			args = append(args, toAnySlice(opts.StatusCategories)...)
		}
	case len(opts.StatusCategories) > 0:
		conds = append(conds, "status IN (SELECT name FROM status_definitions WHERE category IN ("+placeholders(len(opts.StatusCategories))+"))")
		args = append(args, toAnySlice(opts.StatusCategories)...)
	default:
		// Empty status selection defaults to every non-done-category card
		// (plan section 17).
		conds = append(conds, "status IN (SELECT name FROM status_definitions WHERE category <> '"+string(StatusCategoryDone)+"')")
	}

	if len(opts.Types) > 0 {
		conds = append(conds, "card_type IN ("+placeholders(len(opts.Types))+")")
		args = append(args, toAnySlice(opts.Types)...)
	}

	for _, l := range dedupe(opts.Labels) {
		conds = append(conds, "EXISTS (SELECT 1 FROM labels WHERE labels.card_id = cards.id AND labels.label = ?)")
		args = append(args, l)
	}

	if opts.Parent != "" {
		conds = append(conds, "id IN (SELECT child_id FROM card_edges WHERE parent_id = ?)")
		args = append(args, opts.Parent)
	}
	if opts.Child != "" {
		conds = append(conds, "id IN (SELECT parent_id FROM card_edges WHERE child_id = ?)")
		args = append(args, opts.Child)
	}

	if opts.DescriptionLike != "" {
		conds = append(conds, "description LIKE ? ESCAPE '\\' COLLATE NOCASE")
		args = append(args, likePattern(opts.DescriptionLike))
	}

	orderBy, err := listOrderBy(opts.Sort, opts.Reverse)
	if err != nil {
		return nil, err
	}

	query := "SELECT " + cardColumns + " FROM cards"
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY " + orderBy
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	return runCardQuery(ctx, db.sql, query, args)
}

// SearchCards returns cards matching opts, ordered by relevance-neutral
// recency (updated_at descending, then ID ascending). Like ListCards, it
// does not expand notes or edges.
func (db *DB) SearchCards(ctx context.Context, opts SearchOptions) ([]Card, error) {
	if !validateLabels(opts.Labels) {
		return nil, fmt.Errorf("bdd: search cards: labels must be non-empty, valid UTF-8, and at most %d bytes: %w", MaxLabelBytes, ErrInvalidArgument)
	}
	if opts.Limit < 0 {
		return nil, fmt.Errorf("bdd: search cards: limit must be >= 0: %w", ErrInvalidArgument)
	}
	if err := db.ready(); err != nil {
		return nil, err
	}

	var conds []string
	var args []any

	if opts.Query != "" {
		pattern := likePattern(opts.Query)
		textColumns := []string{
			"id", "title", "description", "reproduction", "design",
			"acceptance", "external_ref", "worktree",
		}
		ors := make([]string, 0, len(textColumns)+1)
		for _, col := range textColumns {
			ors = append(ors, col+" LIKE ? ESCAPE '\\' COLLATE NOCASE")
			args = append(args, pattern)
		}
		ors = append(ors, "EXISTS (SELECT 1 FROM notes WHERE notes.card_id = cards.id AND notes.body LIKE ? ESCAPE '\\' COLLATE NOCASE)")
		args = append(args, pattern)
		conds = append(conds, "("+strings.Join(ors, " OR ")+")")
	}

	switch {
	case len(opts.Statuses) > 0:
		conds = append(conds, "status IN ("+placeholders(len(opts.Statuses))+")")
		args = append(args, toAnySlice(opts.Statuses)...)
	case !opts.All:
		// Empty status selection without All defaults to every
		// non-done-category card, matching ListCards (plan section 17).
		conds = append(conds, "status IN (SELECT name FROM status_definitions WHERE category <> '"+string(StatusCategoryDone)+"')")
	}

	for _, l := range dedupe(opts.Labels) {
		conds = append(conds, "EXISTS (SELECT 1 FROM labels WHERE labels.card_id = cards.id AND labels.label = ?)")
		args = append(args, l)
	}

	query := "SELECT " + cardColumns + " FROM cards"
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY updated_at DESC, id ASC"
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	return runCardQuery(ctx, db.sql, query, args)
}

// ReadyCards returns every card matching the readiness predicate described
// on ReadyOptions, in ready-dispatch order.
func (db *DB) ReadyCards(ctx context.Context, opts ReadyOptions) ([]Card, error) {
	return nil, errNotImplemented
}

// ExplainReady evaluates the same readiness predicate as ReadyCards for a
// single card and returns every reason it is excluded (including the IDs
// of any unfinished parents). A ready card returns an empty slice.
func (db *DB) ExplainReady(ctx context.Context, id string) ([]string, error) {
	return nil, errNotImplemented
}

// runCardQuery executes a SELECT built around cardColumns, scans every row
// with scanCard, then fetches labels for the whole result page in one
// bounded follow-up query (avoiding N+1 per plan section 7).
func runCardQuery(ctx context.Context, q execer, query string, args []any) ([]Card, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("bdd: %w", err)
	}
	defer rows.Close()

	var cards []*Card
	ids := make([]string, 0)
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, fmt.Errorf("bdd: %w", err)
		}
		cards = append(cards, c)
		ids = append(ids, c.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bdd: %w", err)
	}

	labelsByCard, err := loadLabelsBatch(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("bdd: %w", err)
	}

	out := make([]Card, len(cards))
	for i, c := range cards {
		c.Labels = labelsByCard[c.ID]
		out[i] = *c
	}
	return out, nil
}

// loadLabelsBatch fetches labels for every card in ids with a single query,
// keyed by card ID.
func loadLabelsBatch(ctx context.Context, q execer, ids []string) (map[string][]string, error) {
	out := make(map[string][]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	query := "SELECT card_id, label FROM labels WHERE card_id IN (" + placeholders(len(ids)) + ") ORDER BY card_id ASC, label ASC"

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var cardID, label string
		if err := rows.Scan(&cardID, &label); err != nil {
			return nil, err
		}
		out[cardID] = append(out[cardID], label)
	}
	return out, rows.Err()
}

// placeholders returns a comma-joined "?" placeholder list of length n.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// toAnySlice converts a slice of a string-based type to []any, for use as
// database/sql query arguments.
func toAnySlice[T ~string](items []T) []any {
	out := make([]any, len(items))
	for i, v := range items {
		out[i] = string(v)
	}
	return out
}

// likePattern wraps s as a "%...%" LIKE pattern, escaping LIKE metacharacters
// ('%', '_') and the escape character itself so substring search treats s
// literally.
func likePattern(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(s) + "%"
}
