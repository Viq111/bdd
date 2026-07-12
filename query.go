package bdd

import "context"

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

// ListCards returns cards matching opts. It returns the core Card fields
// plus labels, without expanding notes or edges; use GetCard for the
// full-fat read of a single card.
func (db *DB) ListCards(ctx context.Context, opts ListOptions) ([]Card, error) {
	return nil, errNotImplemented
}

// SearchCards returns cards matching opts, ordered by relevance-neutral
// recency (updated_at descending, then ID ascending). Like ListCards, it
// does not expand notes or edges.
func (db *DB) SearchCards(ctx context.Context, opts SearchOptions) ([]Card, error) {
	return nil, errNotImplemented
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
