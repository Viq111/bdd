package bdd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func mustCreateChore(t *testing.T, db *DB, title string, labels ...string) *Card {
	t.Helper()
	c, err := db.CreateCard(context.Background(), CreateCard{Title: title, Type: CardTypeChore, Labels: labels, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard(%q) error = %v", title, err)
	}
	return c
}

func cardIDs(cards []Card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.ID
	}
	return out
}

func containsID(cards []Card, id string) bool {
	for _, c := range cards {
		if c.ID == id {
			return true
		}
	}
	return false
}

func TestListCardsDefaultExcludesDoneStatuses(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	open := mustCreateChore(t, db, "stays open")
	closedCard := mustCreateChore(t, db, "gets closed")
	closed := StatusClosed
	if _, err := db.UpdateCard(ctx, closedCard.ID, UpdateCard{Status: &closed, Actor: "alice"}); err != nil {
		t.Fatalf("UpdateCard(status=closed) error = %v", err)
	}

	got, err := db.ListCards(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if !containsID(got, open.ID) {
		t.Fatalf("ListCards() = %v, want to contain open card %s", cardIDs(got), open.ID)
	}
	if containsID(got, closedCard.ID) {
		t.Fatalf("ListCards() = %v, want to exclude closed card %s", cardIDs(got), closedCard.ID)
	}
}

func TestListCardsFilterByStatuses(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	open := mustCreateChore(t, db, "open")
	inProgressCard := mustCreateChore(t, db, "in progress")
	ip := StatusInProgress
	if _, err := db.UpdateCard(ctx, inProgressCard.ID, UpdateCard{Status: &ip, Actor: "alice"}); err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}

	got, err := db.ListCards(ctx, ListOptions{Statuses: []Status{StatusInProgress}})
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != inProgressCard.ID {
		t.Fatalf("ListCards(Statuses=[in_progress]) = %v, want [%s]", cardIDs(got), inProgressCard.ID)
	}
	if containsID(got, open.ID) {
		t.Fatalf("ListCards(Statuses=[in_progress]) unexpectedly contains open card")
	}
}

func TestListCardsFilterByStatusCategories(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	wip := mustCreateChore(t, db, "wip")
	ip := StatusInProgress
	if _, err := db.UpdateCard(ctx, wip.ID, UpdateCard{Status: &ip, Actor: "alice"}); err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	mustCreateChore(t, db, "still open")

	got, err := db.ListCards(ctx, ListOptions{StatusCategories: []StatusCategory{StatusCategoryWIP}})
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != wip.ID {
		t.Fatalf("ListCards(StatusCategories=[wip]) = %v, want [%s]", cardIDs(got), wip.ID)
	}
}

func TestListCardsFilterByTypes(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	chore := mustCreateChore(t, db, "a chore")
	task, err := db.CreateCard(ctx, CreateCard{Title: "a task", Type: CardTypeTask, Acceptance: ptr("done"), CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard(task) error = %v", err)
	}

	got, err := db.ListCards(ctx, ListOptions{Types: []CardType{CardTypeTask}})
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != task.ID {
		t.Fatalf("ListCards(Types=[task]) = %v, want [%s]", cardIDs(got), task.ID)
	}
	if containsID(got, chore.ID) {
		t.Fatalf("ListCards(Types=[task]) unexpectedly contains chore")
	}
}

func TestListCardsFilterByLabelsAND(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	both := mustCreateChore(t, db, "both labels", "a", "b")
	onlyA := mustCreateChore(t, db, "only a", "a")

	got, err := db.ListCards(ctx, ListOptions{Labels: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != both.ID {
		t.Fatalf("ListCards(Labels=[a,b]) = %v, want [%s]", cardIDs(got), both.ID)
	}
	if containsID(got, onlyA.ID) {
		t.Fatalf("ListCards(Labels=[a,b]) unexpectedly contains card with only label a")
	}
}

func TestListCardsFilterByParentChild(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	parent := mustCreateChore(t, db, "parent")
	child := mustCreateChore(t, db, "child")
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO card_edges (parent_id, child_id, created_at, created_by) VALUES (?, ?, ?, ?)`,
		parent.ID, child.ID, formatTime(time.Now()), "alice"); err != nil {
		t.Fatalf("inserting card_edges row: %v", err)
	}

	children, err := db.ListCards(ctx, ListOptions{Parent: parent.ID})
	if err != nil {
		t.Fatalf("ListCards(Parent) error = %v", err)
	}
	if len(children) != 1 || children[0].ID != child.ID {
		t.Fatalf("ListCards(Parent=%s) = %v, want [%s]", parent.ID, cardIDs(children), child.ID)
	}

	parents, err := db.ListCards(ctx, ListOptions{Child: child.ID})
	if err != nil {
		t.Fatalf("ListCards(Child) error = %v", err)
	}
	if len(parents) != 1 || parents[0].ID != parent.ID {
		t.Fatalf("ListCards(Child=%s) = %v, want [%s]", child.ID, cardIDs(parents), parent.ID)
	}
}

func TestListCardsDescriptionLike(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	match, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore, Description: ptr("Cache Invalidation Bug"), CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	mustCreateChore(t, db, "unrelated")

	got, err := db.ListCards(ctx, ListOptions{DescriptionLike: "cache invalidation"})
	if err != nil {
		t.Fatalf("ListCards(DescriptionLike) error = %v", err)
	}
	if len(got) != 1 || got[0].ID != match.ID {
		t.Fatalf("ListCards(DescriptionLike) = %v, want [%s]", cardIDs(got), match.ID)
	}
}

func TestListCardsSortAndReverse(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	low, err := db.CreateCard(ctx, CreateCard{Title: "low priority number", Type: CardTypeChore, Priority: ptr(int32(0)), CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	high, err := db.CreateCard(ctx, CreateCard{Title: "high priority number", Type: CardTypeChore, Priority: ptr(int32(5)), CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	asc, err := db.ListCards(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(asc) != 2 || asc[0].ID != low.ID || asc[1].ID != high.ID {
		t.Fatalf("ListCards() default priority order = %v, want [%s %s]", cardIDs(asc), low.ID, high.ID)
	}

	desc, err := db.ListCards(ctx, ListOptions{Reverse: true})
	if err != nil {
		t.Fatalf("ListCards(Reverse) error = %v", err)
	}
	if len(desc) != 2 || desc[0].ID != high.ID || desc[1].ID != low.ID {
		t.Fatalf("ListCards(Reverse) = %v, want [%s %s]", cardIDs(desc), high.ID, low.ID)
	}
}

func TestListCardsUnknownStatusReturnsInvalidArgument(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.ListCards(ctx, ListOptions{Statuses: []Status{"opne"}})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ListCards(Statuses=[opne]) error = %v, want ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), `"opne"`) || !strings.Contains(err.Error(), "open") {
		t.Fatalf("ListCards(Statuses=[opne]) error = %q, want it to name the bad value and valid choices", err)
	}
}

func TestListCardsUnknownStatusCategoryReturnsInvalidArgument(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.ListCards(ctx, ListOptions{StatusCategories: []StatusCategory{"actve"}})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ListCards(StatusCategories=[actve]) error = %v, want ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), `"actve"`) || !strings.Contains(err.Error(), "active") {
		t.Fatalf("ListCards(StatusCategories=[actve]) error = %q, want it to name the bad value and valid choices", err)
	}
}

func TestListCardsUnknownTypeReturnsInvalidArgument(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.ListCards(ctx, ListOptions{Types: []CardType{"tesk"}})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ListCards(Types=[tesk]) error = %v, want ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), `"tesk"`) || !strings.Contains(err.Error(), "task") {
		t.Fatalf("ListCards(Types=[tesk]) error = %q, want it to name the bad value and valid choices", err)
	}
}

func TestListCardsAcceptsCustomStatusAndType(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.ConfigSet(ctx, ConfigKeyStatusCustom, "qa_testing:wip", "alice"); err != nil {
		t.Fatalf("ConfigSet(status.custom) error = %v", err)
	}
	if err := db.ConfigSet(ctx, ConfigKeyTypesCustom, "spike", "alice"); err != nil {
		t.Fatalf("ConfigSet(types.custom) error = %v", err)
	}

	if _, err := db.ListCards(ctx, ListOptions{Statuses: []Status{"qa_testing"}}); err != nil {
		t.Fatalf("ListCards(Statuses=[qa_testing]) error = %v, want nil", err)
	}
	if _, err := db.ListCards(ctx, ListOptions{Types: []CardType{"spike"}}); err != nil {
		t.Fatalf("ListCards(Types=[spike]) error = %v, want nil", err)
	}
}

func TestListCardsValidFilterMatchingNothingReturnsEmptyNotError(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	mustCreateChore(t, db, "a chore")

	got, err := db.ListCards(ctx, ListOptions{Types: []CardType{CardTypeTask}})
	if err != nil {
		t.Fatalf("ListCards(Types=[task]) error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListCards(Types=[task]) = %v, want empty", cardIDs(got))
	}
}

func TestListCardsAllIncludesDoneCards(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	open := mustCreateChore(t, db, "stays open")
	closedCard := mustCreateChore(t, db, "gets closed")
	closed := StatusClosed
	if _, err := db.UpdateCard(ctx, closedCard.ID, UpdateCard{Status: &closed, Actor: "alice"}); err != nil {
		t.Fatalf("UpdateCard(status=closed) error = %v", err)
	}

	got, err := db.ListCards(ctx, ListOptions{All: true})
	if err != nil {
		t.Fatalf("ListCards(All=true) error = %v", err)
	}
	if !containsID(got, open.ID) || !containsID(got, closedCard.ID) {
		t.Fatalf("ListCards(All=true) = %v, want to contain both %s and %s", cardIDs(got), open.ID, closedCard.ID)
	}

	withoutAll, err := db.ListCards(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if containsID(withoutAll, closedCard.ID) {
		t.Fatalf("ListCards() = %v, want to exclude closed card %s", cardIDs(withoutAll), closedCard.ID)
	}
}

func TestListCardsUnknownSortFieldReturnsInvalidArgument(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.ListCards(ctx, ListOptions{Sort: "bogus"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ListCards(Sort=bogus) error = %v, want ErrInvalidArgument", err)
	}
}

func TestListCardsInvalidLimitReturnsInvalidArgument(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.ListCards(ctx, ListOptions{Limit: -1}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ListCards(Limit=-1) error = %v, want ErrInvalidArgument", err)
	}
}

func TestListCardsLimitZeroMeansUnlimited(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		mustCreateChore(t, db, "card")
	}

	got, err := db.ListCards(ctx, ListOptions{Limit: 0})
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("ListCards(Limit=0) returned %d cards, want 5", len(got))
	}

	limited, err := db.ListCards(ctx, ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("ListCards(Limit=2) error = %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("ListCards(Limit=2) returned %d cards, want 2", len(limited))
	}
}

func TestSearchCardsMatchesAcrossFields(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	byTitle := mustCreateChore(t, db, "unique-title-marker")
	byDesc, err := db.CreateCard(ctx, CreateCard{Title: "x1", Type: CardTypeChore, Description: ptr("has unique-desc-marker inside"), CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	byWorktree, err := db.CreateCard(ctx, CreateCard{Title: "x2", Type: CardTypeChore, Worktree: ptr(".worktrees/unique-wt-marker"), CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	byNote := mustCreateChore(t, db, "x3")
	if _, err := db.AddNote(ctx, AddNote{CardID: byNote.ID, Body: "progress: unique-note-marker seen", Author: "alice"}); err != nil {
		t.Fatalf("AddNote() error = %v", err)
	}

	for _, tt := range []struct {
		query string
		want  string
	}{
		{"UNIQUE-TITLE-MARKER", byTitle.ID},
		{"unique-desc-marker", byDesc.ID},
		{"unique-wt-marker", byWorktree.ID},
		{"unique-note-marker", byNote.ID},
	} {
		got, err := db.SearchCards(ctx, SearchOptions{Query: tt.query})
		if err != nil {
			t.Fatalf("SearchCards(%q) error = %v", tt.query, err)
		}
		if len(got) != 1 || got[0].ID != tt.want {
			t.Fatalf("SearchCards(%q) = %v, want [%s]", tt.query, cardIDs(got), tt.want)
		}
	}
}

func TestSearchCardsMatchesIDPrefixAndSubstring(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	card := mustCreateChore(t, db, "prefix search target")

	// A prefix of the ID (e.g. what a user would type) must match via the
	// indexed prefix branch.
	got, err := db.SearchCards(ctx, SearchOptions{Query: card.ID[:len(card.ID)-2]})
	if err != nil {
		t.Fatalf("SearchCards(id prefix) error = %v", err)
	}
	if !containsID(got, card.ID) {
		t.Fatalf("SearchCards(id prefix) = %v, want to include %s", cardIDs(got), card.ID)
	}

	// A substring in the middle of the ID must still match via the
	// substring fallback branch, not just a prefix.
	mid := card.ID[len(card.ID)/2 : len(card.ID)/2+2]
	got, err = db.SearchCards(ctx, SearchOptions{Query: mid})
	if err != nil {
		t.Fatalf("SearchCards(id substring) error = %v", err)
	}
	if !containsID(got, card.ID) {
		t.Fatalf("SearchCards(id substring %q) = %v, want to include %s", mid, cardIDs(got), card.ID)
	}
}

// TestSearchQueryUsesFTS5TrigramIndex is a query-plan regression test for
// bd bdd-cdm: SearchCards' Query text must be satisfied via the cards_fts/
// notes_fts trigram-tokenized virtual tables (migration
// 0004_fts5_search.sql), not a full table scan of cards, which was the
// dominant cost behind SearchCards missing its section 7 latency budget.
func TestSearchQueryUsesFTS5TrigramIndex(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// SQLite's query planner only prefers an index lookup over a full
	// table/vtable scan once the table is large enough for the cost
	// estimate to favor it, so seed enough rows to make that choice
	// observable.
	for i := 0; i < 2000; i++ {
		mustCreateChore(t, db, fmt.Sprintf("card %d", i))
	}

	cond, args := searchMatchCondition("marker")
	cardsSubquery := strings.TrimPrefix(strings.SplitN(cond, " UNION ", 2)[0], "id IN (")
	cardsArgs := args[:len(searchQueryTextColumns)]

	rows, err := db.sql.QueryContext(ctx, "EXPLAIN QUERY PLAN "+cardsSubquery, cardsArgs...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN error = %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("rows.Columns() error = %v", err)
	}
	var usedVirtualTable, scannedCardsTable bool
	for rows.Next() {
		scan := make([]any, len(cols))
		vals := make([]sql.NullString, len(cols))
		for i := range scan {
			scan[i] = &vals[i]
		}
		if err := rows.Scan(scan...); err != nil {
			t.Fatalf("rows.Scan() error = %v", err)
		}
		for _, v := range vals {
			if strings.Contains(v.String, "cards_fts") && strings.Contains(v.String, "VIRTUAL TABLE") {
				usedVirtualTable = true
			}
			if strings.Contains(v.String, "SCAN cards ") || strings.HasSuffix(v.String, "SCAN cards") {
				scannedCardsTable = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v", err)
	}
	if !usedVirtualTable {
		t.Fatalf("SearchCards query plan did not use the cards_fts virtual table")
	}
	if scannedCardsTable {
		t.Fatalf("SearchCards query plan fell back to a full scan of cards")
	}
}

// TestDeleteCardRemovesItAndItsNotesFromSearch is a regression test for bd
// bdd-cdm: an earlier version of migration 0004_fts5_search.sql added a
// redundant BEFORE DELETE ON cards trigger to clean up notes_fts, on the
// mistaken assumption that an FK ON DELETE CASCADE removing a card's notes
// does not itself fire notes_fts_ad. It does, so the extra trigger raced
// with the cascade-fired one and issued the fts5 'delete' command against
// the same row twice, corrupting the database (SQLITE_CORRUPT_VTAB) on the
// very next DeleteCard call that had a note attached.
func TestDeleteCardRemovesItAndItsNotesFromSearch(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	card := mustCreateChore(t, db, "deleteme-title-marker")
	if _, err := db.AddNote(ctx, AddNote{CardID: card.ID, Body: "deleteme-note-marker", Author: "alice"}); err != nil {
		t.Fatalf("AddNote() error = %v", err)
	}

	if got, err := db.SearchCards(ctx, SearchOptions{Query: "deleteme-note-marker"}); err != nil || !containsID(got, card.ID) {
		t.Fatalf("SearchCards(note) before delete = %v, %v, want to include %s", got, err, card.ID)
	}

	if _, err := db.DeleteCard(ctx, card.ID, "alice", true); err != nil {
		t.Fatalf("DeleteCard() error = %v", err)
	}

	got, err := db.SearchCards(ctx, SearchOptions{Query: "deleteme-note-marker", All: true})
	if err != nil {
		t.Fatalf("SearchCards(note) after delete error = %v", err)
	}
	if containsID(got, card.ID) {
		t.Fatalf("SearchCards(note) after delete = %v, want to exclude deleted card %s", cardIDs(got), card.ID)
	}

	got, err = db.SearchCards(ctx, SearchOptions{Query: "deleteme-title-marker", All: true})
	if err != nil {
		t.Fatalf("SearchCards(title) after delete error = %v", err)
	}
	if containsID(got, card.ID) {
		t.Fatalf("SearchCards(title) after delete = %v, want to exclude deleted card %s", cardIDs(got), card.ID)
	}

	// The database must still be usable after the delete: prior corruption
	// left it unable to serve further writes.
	if _, err := db.CreateCard(ctx, CreateCard{Title: "still alive", Type: CardTypeChore, CreatedBy: "alice"}); err != nil {
		t.Fatalf("CreateCard() after delete error = %v", err)
	}
}

func TestSearchCardsDefaultExcludesDoneStatusUnlessAll(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	card, err := db.CreateCard(ctx, CreateCard{Title: "findme-marker", Type: CardTypeChore, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	closed := StatusClosed
	if _, err := db.UpdateCard(ctx, card.ID, UpdateCard{Status: &closed, Actor: "alice"}); err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}

	got, err := db.SearchCards(ctx, SearchOptions{Query: "findme-marker"})
	if err != nil {
		t.Fatalf("SearchCards() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("SearchCards() without All = %v, want empty (closed card excluded)", cardIDs(got))
	}

	got, err = db.SearchCards(ctx, SearchOptions{Query: "findme-marker", All: true})
	if err != nil {
		t.Fatalf("SearchCards(All) error = %v", err)
	}
	if len(got) != 1 || got[0].ID != card.ID {
		t.Fatalf("SearchCards(All) = %v, want [%s]", cardIDs(got), card.ID)
	}
}

func TestSearchCardsStatusesOverridesAll(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	card, err := db.CreateCard(ctx, CreateCard{Title: "findme-marker", Type: CardTypeChore, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	got, err := db.SearchCards(ctx, SearchOptions{Query: "findme-marker", Statuses: []Status{StatusClosed}})
	if err != nil {
		t.Fatalf("SearchCards(Statuses=[closed]) error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("SearchCards(Statuses=[closed]) = %v, want empty (card is open)", cardIDs(got))
	}
	if containsID(got, card.ID) {
		t.Fatalf("unexpected match")
	}
}

func TestSearchCardsLabelsAND(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	both := mustCreateChore(t, db, "search-marker both", "a", "b")
	mustCreateChore(t, db, "search-marker only-a", "a")

	got, err := db.SearchCards(ctx, SearchOptions{Query: "search-marker", Labels: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("SearchCards(Labels=[a,b]) error = %v", err)
	}
	if len(got) != 1 || got[0].ID != both.ID {
		t.Fatalf("SearchCards(Labels=[a,b]) = %v, want [%s]", cardIDs(got), both.ID)
	}
}

func TestSearchCardsOrderingByUpdatedAtDescThenID(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	a := mustCreateChore(t, db, "order-marker a")
	time.Sleep(2 * time.Millisecond)
	b := mustCreateChore(t, db, "order-marker b")
	time.Sleep(2 * time.Millisecond)

	// Touch a again so it becomes the most recently updated.
	if _, err := db.AddNote(ctx, AddNote{CardID: a.ID, Body: "bump", Author: "alice"}); err != nil {
		t.Fatalf("AddNote() error = %v", err)
	}

	got, err := db.SearchCards(ctx, SearchOptions{Query: "order-marker"})
	if err != nil {
		t.Fatalf("SearchCards() error = %v", err)
	}
	if len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("SearchCards() order = %v, want [%s %s] (most recently updated first)", cardIDs(got), a.ID, b.ID)
	}
}

func TestSearchCardsLimit(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		mustCreateChore(t, db, "limit-marker")
	}

	got, err := db.SearchCards(ctx, SearchOptions{Query: "limit-marker", Limit: 2})
	if err != nil {
		t.Fatalf("SearchCards(Limit=2) error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("SearchCards(Limit=2) returned %d cards, want 2", len(got))
	}
}

func TestSearchCardsInvalidLimitReturnsInvalidArgument(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.SearchCards(ctx, SearchOptions{Limit: -1}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("SearchCards(Limit=-1) error = %v, want ErrInvalidArgument", err)
	}
}
