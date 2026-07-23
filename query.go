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

	// All includes cards of every status, including done-category ones,
	// when Statuses and StatusCategories are both empty. Ignored otherwise,
	// matching SearchOptions.All.
	All bool

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

func validateListOptions(ctx context.Context, q execer, opts ListOptions) error {
	if !validateLabels(opts.Labels) {
		return fmt.Errorf("bdd: list cards: labels must be non-empty, valid UTF-8, and at most %d bytes: %w", MaxLabelBytes, ErrInvalidArgument)
	}
	if opts.Limit < 0 {
		return fmt.Errorf("bdd: list cards: limit must be >= 0: %w", ErrInvalidArgument)
	}
	if err := validateStatusCategories(opts.StatusCategories); err != nil {
		return err
	}
	if len(opts.Statuses) > 0 {
		valid, err := definitionNames(ctx, q, "status_definitions")
		if err != nil {
			return fmt.Errorf("bdd: list cards: %w", err)
		}
		for _, st := range opts.Statuses {
			if !contains(valid, string(st)) {
				return fmt.Errorf("bdd: list cards: unknown status %q (valid: %s): %w", st, strings.Join(valid, ", "), ErrInvalidArgument)
			}
		}
	}
	if len(opts.Types) > 0 {
		valid, err := definitionNames(ctx, q, "type_definitions")
		if err != nil {
			return fmt.Errorf("bdd: list cards: %w", err)
		}
		for _, t := range opts.Types {
			if !contains(valid, string(t)) {
				return fmt.Errorf("bdd: list cards: unknown type %q (valid: %s): %w", t, strings.Join(valid, ", "), ErrInvalidArgument)
			}
		}
	}
	return nil
}

// validStatusCategoryNames lists every StatusCategory value, in declaration
// order, for use in "unknown status category" error messages.
var validStatusCategoryNames = []string{
	string(StatusCategoryActive), string(StatusCategoryWIP), string(StatusCategoryDone), string(StatusCategoryFrozen),
}

func validateStatusCategories(categories []StatusCategory) error {
	for _, c := range categories {
		if !validStatusCategories[c] {
			return fmt.Errorf("bdd: list cards: unknown status category %q (valid: %s): %w", c, strings.Join(validStatusCategoryNames, ", "), ErrInvalidArgument)
		}
	}
	return nil
}

// definitionNames returns every name column value from a *_definitions
// table (status_definitions or type_definitions), ordered by name ascending,
// for validating list filter vocabulary against the workspace's built-in and
// custom definitions.
func definitionNames(ctx context.Context, q execer, table string) ([]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT name FROM "+table+" ORDER BY name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// contains reports whether items contains s.
func contains(items []string, s string) bool {
	for _, it := range items {
		if it == s {
			return true
		}
	}
	return false
}

// ListCards returns cards matching opts. It returns the core Card fields
// plus labels, without expanding notes or edges; use GetCard for the
// full-fat read of a single card.
func (db *DB) ListCards(ctx context.Context, opts ListOptions) ([]Card, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}
	if err := validateListOptions(ctx, db.sql, opts); err != nil {
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
	case !opts.All:
		// Empty status selection without All defaults to every
		// non-done-category card (plan section 17).
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
		matchCond, matchArgs := searchMatchCondition(opts.Query)
		conds = append(conds, matchCond)
		args = append(args, matchArgs...)
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

// readyConds is the WHERE-clause fragment shared by ReadyCards and (in
// spirit; ExplainReady re-checks the same six conditions per-field for a
// single card) the readiness predicate: status category active,
// dispatchable, unassigned, lacking the human label, and every parent
// done-category (plan section 16).
var readyConds = []string{
	"status IN (SELECT name FROM status_definitions WHERE category = '" + string(StatusCategoryActive) + "')",
	"dispatchable <> 0",
	"assignee = ''",
	"NOT EXISTS (SELECT 1 FROM labels WHERE labels.card_id = cards.id AND labels.label = '" + HumanLabel + "')",
	"NOT EXISTS (" +
		"SELECT 1 FROM card_edges ce JOIN cards p ON p.id = ce.parent_id " +
		"WHERE ce.child_id = cards.id AND p.status NOT IN (SELECT name FROM status_definitions WHERE category = '" + string(StatusCategoryDone) + "')" +
		")",
}

// ReadyCards returns every card matching the readiness predicate described
// on ReadyOptions, in ready-dispatch order (priority ascending, then
// created_at ascending, then ID).
func (db *DB) ReadyCards(ctx context.Context, opts ReadyOptions) ([]Card, error) {
	if !validateLabels(opts.Labels) {
		return nil, fmt.Errorf("bdd: ready cards: labels must be non-empty, valid UTF-8, and at most %d bytes: %w", MaxLabelBytes, ErrInvalidArgument)
	}
	if opts.Limit < 0 {
		return nil, fmt.Errorf("bdd: ready cards: limit must be >= 0: %w", ErrInvalidArgument)
	}
	if err := db.ready(); err != nil {
		return nil, err
	}

	conds := append([]string{}, readyConds...)
	var args []any
	for _, l := range dedupe(opts.Labels) {
		conds = append(conds, "EXISTS (SELECT 1 FROM labels WHERE labels.card_id = cards.id AND labels.label = ?)")
		args = append(args, l)
	}

	query := "SELECT " + cardColumns + " FROM cards WHERE " + strings.Join(conds, " AND ") + " ORDER BY priority ASC, created_at ASC, id ASC"
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	return runCardQuery(ctx, db.sql, query, args)
}

// unfinishedParent is one parent card that is not yet in a done-category
// status, as reported by ExplainReady.
type unfinishedParent struct {
	ID     string
	Status Status
}

// unfinishedParents returns every parent of id that is not in a
// done-category status, ordered by parent ID ascending.
func unfinishedParents(ctx context.Context, q execer, id string) ([]unfinishedParent, error) {
	rows, err := q.QueryContext(ctx, `
SELECT p.id, p.status FROM card_edges ce JOIN cards p ON p.id = ce.parent_id
WHERE ce.child_id = ? AND p.status NOT IN (SELECT name FROM status_definitions WHERE category = ?)
ORDER BY p.id ASC`, id, string(StatusCategoryDone))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []unfinishedParent{}
	for rows.Next() {
		var up unfinishedParent
		var status string
		if err := rows.Scan(&up.ID, &status); err != nil {
			return nil, err
		}
		up.Status = Status(status)
		out = append(out, up)
	}
	return out, rows.Err()
}

// ExplainReady evaluates the same readiness predicate as ReadyCards for a
// single card and returns every reason it is excluded (including one entry
// per unfinished parent). A ready card returns an empty (non-nil) slice.
func (db *DB) ExplainReady(ctx context.Context, id string) ([]string, error) {
	if err := db.ready(); err != nil {
		return nil, err
	}

	cur, err := loadCard(ctx, db.sql, id)
	if err != nil {
		return nil, err
	}

	category, err := statusCategory(ctx, db.sql, cur.Status)
	if err != nil {
		return nil, err
	}

	reasons := []string{}
	if category != StatusCategoryActive {
		reasons = append(reasons, fmt.Sprintf("status %q is %s-category, not active", cur.Status, category))
	}
	if !cur.Dispatchable {
		reasons = append(reasons, "not dispatchable")
	}
	if cur.Assignee != "" {
		reasons = append(reasons, fmt.Sprintf("assigned to %s", cur.Assignee))
	}
	for _, l := range cur.Labels {
		if l == HumanLabel {
			reasons = append(reasons, "has the human label")
			break
		}
	}

	unfinished, err := unfinishedParents(ctx, db.sql, id)
	if err != nil {
		return nil, fmt.Errorf("bdd: explain ready: %w", err)
	}
	for _, p := range unfinished {
		reasons = append(reasons, fmt.Sprintf("parent %s is not done (status: %s)", p.ID, p.Status))
	}

	return reasons, nil
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
	return "%" + escapeLike(s) + "%"
}

// needsLikeEscape reports whether s contains a LIKE metacharacter or the
// escape character, i.e. whether a literal match against s requires an
// ESCAPE clause.
func needsLikeEscape(s string) bool {
	return strings.ContainsAny(s, `\%_`)
}

// searchQueryTextColumns lists every cards_fts column SearchCards matches
// Query against, case-insensitively.
var searchQueryTextColumns = []string{
	"id", "title", "description", "reproduction", "design",
	"acceptance", "external_ref", "worktree",
}

// searchMatchCondition builds the "id IN (...)" WHERE condition and its args
// for SearchCards' Query text, as a UNION of two subqueries against the
// cards_fts/notes_fts trigram-tokenized virtual tables (migration
// 0004_fts5_search.sql, plan section 7's anticipated fallback: "Add an FTS5
// table maintained transactionally if the latency benchmark exceeds budget
// at the target database size"):
//
//  1. a case-insensitive substring match ("%query%") OR'd across every
//     searched column of cards_fts;
//  2. the same substring match against notes_fts's body column, mapped back
//     to its card_id.
//
// Querying cards_fts/notes_fts instead of cards/notes directly lets
// SQLite's LIKE optimizer satisfy each OR'd branch with a trigram index
// lookup (SQLite's MULTI-INDEX OR) instead of a full table scan, while
// preserving the exact substring-match semantics SearchCards has always had.
// A query containing a LIKE metacharacter ('%', '_', '\') still gets correct
// (if unaccelerated) substring semantics: the ESCAPE clause on a bound
// parameter defeats the trigram optimizer, same as it would for a plain
// LIKE against a real table, so that case simply falls back to a virtual
// table scan rather than a scan of cards/notes directly.
func searchMatchCondition(query string) (string, []any) {
	var args []any

	substringPattern := likePattern(query)
	needsEscape := needsLikeEscape(query)

	cardOrs := make([]string, 0, len(searchQueryTextColumns))
	for _, col := range searchQueryTextColumns {
		if needsEscape {
			cardOrs = append(cardOrs, col+" LIKE ? ESCAPE '\\'")
		} else {
			cardOrs = append(cardOrs, col+" LIKE ?")
		}
		args = append(args, substringPattern)
	}
	cardsBranch := "SELECT id FROM cards_fts WHERE " + strings.Join(cardOrs, " OR ")

	notesBranch := "SELECT card_id FROM notes_fts WHERE body LIKE ?"
	if needsEscape {
		notesBranch += " ESCAPE '\\'"
	}
	args = append(args, substringPattern)

	cond := "id IN (" + cardsBranch + " UNION " + notesBranch + ")"
	return cond, args
}
