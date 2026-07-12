package bdd

import (
	"context"
	"strings"
	"testing"
)

func setDispatchable(t *testing.T, db *DB, id string, dispatchable bool) {
	t.Helper()
	v := 0
	if dispatchable {
		v = 1
	}
	if _, err := db.sql.Exec(`UPDATE cards SET dispatchable = ? WHERE id = ?`, v, id); err != nil {
		t.Fatalf("setDispatchable(%s) error = %v", id, err)
	}
}

func TestReadyCardsExcludesNonActiveCategory(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	active := mustCreate(t, db, "active")
	wip := mustCreate(t, db, "wip")
	if _, err := db.ClaimCard(ctx, wip.ID, "alice"); err != nil {
		t.Fatalf("ClaimCard() error = %v", err)
	}
	closedCard := mustCreate(t, db, "closed")
	if _, err := db.CloseCard(ctx, closedCard.ID, CloseCard{Actor: "alice"}); err != nil {
		t.Fatalf("CloseCard() error = %v", err)
	}
	frozen := mustCreate(t, db, "frozen")
	if _, err := db.DeferCard(ctx, frozen.ID, "alice", nil); err != nil {
		t.Fatalf("DeferCard() error = %v", err)
	}

	got, err := db.ReadyCards(ctx, ReadyOptions{})
	if err != nil {
		t.Fatalf("ReadyCards() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != active.ID {
		t.Fatalf("ReadyCards() = %v, want [%s]", cardIDs(got), active.ID)
	}
}

func TestReadyCardsExcludesNonDispatchable(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	ready := mustCreate(t, db, "ready")
	blocked := mustCreate(t, db, "not dispatchable")
	setDispatchable(t, db, blocked.ID, false)

	got, err := db.ReadyCards(ctx, ReadyOptions{})
	if err != nil {
		t.Fatalf("ReadyCards() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != ready.ID {
		t.Fatalf("ReadyCards() = %v, want [%s]", cardIDs(got), ready.ID)
	}
}

func TestReadyCardsExcludesAssigned(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	ready := mustCreate(t, db, "ready")
	assigned := mustCreate(t, db, "assigned but still open")
	if _, err := db.sql.Exec(`UPDATE cards SET assignee = ? WHERE id = ?`, "alice", assigned.ID); err != nil {
		t.Fatalf("set assignee error = %v", err)
	}

	got, err := db.ReadyCards(ctx, ReadyOptions{})
	if err != nil {
		t.Fatalf("ReadyCards() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != ready.ID {
		t.Fatalf("ReadyCards() = %v, want [%s]", cardIDs(got), ready.ID)
	}
}

func TestReadyCardsExcludesHumanLabel(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	ready := mustCreate(t, db, "ready")
	human := mustCreate(t, db, "needs human")
	if _, err := db.HumanCard(ctx, human.ID, "alice", "unsure"); err != nil {
		t.Fatalf("HumanCard() error = %v", err)
	}

	got, err := db.ReadyCards(ctx, ReadyOptions{})
	if err != nil {
		t.Fatalf("ReadyCards() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != ready.ID {
		t.Fatalf("ReadyCards() = %v, want [%s]", cardIDs(got), ready.ID)
	}
}

func TestReadyCardsExcludesUnfinishedParent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	parent := mustCreate(t, db, "parent")
	child := mustCreate(t, db, "child")
	if err := db.AddParent(ctx, child.ID, parent.ID, "alice"); err != nil {
		t.Fatalf("AddParent() error = %v", err)
	}

	got, err := db.ReadyCards(ctx, ReadyOptions{})
	if err != nil {
		t.Fatalf("ReadyCards() error = %v", err)
	}
	if !containsID(got, parent.ID) || containsID(got, child.ID) {
		t.Fatalf("ReadyCards() = %v, want [%s] only", cardIDs(got), parent.ID)
	}

	if _, err := db.CloseCard(ctx, parent.ID, CloseCard{Actor: "alice"}); err != nil {
		t.Fatalf("CloseCard() error = %v", err)
	}
	got, err = db.ReadyCards(ctx, ReadyOptions{})
	if err != nil {
		t.Fatalf("ReadyCards() error = %v", err)
	}
	if !containsID(got, child.ID) {
		t.Fatalf("ReadyCards() after parent closed = %v, want to contain %s", cardIDs(got), child.ID)
	}
}

func TestReadyCardsLabelFilterIsAND(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	both := mustCreateChore(t, db, "both", "a", "b")
	onlyA := mustCreateChore(t, db, "only a", "a")

	got, err := db.ReadyCards(ctx, ReadyOptions{Labels: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("ReadyCards() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != both.ID {
		t.Fatalf("ReadyCards(Labels=[a,b]) = %v, want [%s]", cardIDs(got), both.ID)
	}
	_ = onlyA
}

func TestReadyCardsSortOrder(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	low := mustLowPriority(t, db, "low priority", 5)
	high := mustLowPriority(t, db, "high priority", 0)
	mid := mustLowPriority(t, db, "mid priority", 2)

	got, err := db.ReadyCards(ctx, ReadyOptions{})
	if err != nil {
		t.Fatalf("ReadyCards() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ReadyCards() len = %d, want 3", len(got))
	}
	want := []string{high.ID, mid.ID, low.ID}
	got2 := cardIDs(got)
	for i, id := range want {
		if got2[i] != id {
			t.Fatalf("ReadyCards() order = %v, want %v", got2, want)
		}
	}
}

func mustLowPriority(t *testing.T, db *DB, title string, priority int32) *Card {
	t.Helper()
	c, err := db.CreateCard(context.Background(), CreateCard{Title: title, Type: CardTypeChore, Priority: &priority, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard(%q) error = %v", title, err)
	}
	return c
}

func TestReadyCardsLimitZeroIsUnlimited(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	mustCreate(t, db, "a")
	mustCreate(t, db, "b")
	mustCreate(t, db, "c")

	got, err := db.ReadyCards(ctx, ReadyOptions{Limit: 0})
	if err != nil {
		t.Fatalf("ReadyCards() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ReadyCards(Limit=0) len = %d, want 3", len(got))
	}

	got, err = db.ReadyCards(ctx, ReadyOptions{Limit: 2})
	if err != nil {
		t.Fatalf("ReadyCards() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ReadyCards(Limit=2) len = %d, want 2", len(got))
	}
}

func TestReadyCardsReadsCustomStatusCategories(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.ConfigSet(ctx, ConfigKeyStatusCustom, "triaged:active", "alice"); err != nil {
		t.Fatalf("ConfigSet() error = %v", err)
	}
	card := mustCreate(t, db, "custom active status")
	triaged := Status("triaged")
	if _, err := db.UpdateCard(ctx, card.ID, UpdateCard{Status: &triaged, Actor: "alice"}); err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}

	got, err := db.ReadyCards(ctx, ReadyOptions{})
	if err != nil {
		t.Fatalf("ReadyCards() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != card.ID {
		t.Fatalf("ReadyCards() = %v, want [%s]", cardIDs(got), card.ID)
	}
}

func TestExplainReadyReturnsEmptyForReadyCard(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "ready")

	reasons, err := db.ExplainReady(ctx, card.ID)
	if err != nil {
		t.Fatalf("ExplainReady() error = %v", err)
	}
	if len(reasons) != 0 {
		t.Fatalf("ExplainReady() = %v, want empty", reasons)
	}
}

func TestExplainReadyReportsEveryExclusionReason(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	parent := mustCreate(t, db, "parent")
	child := mustCreate(t, db, "child")
	if err := db.AddParent(ctx, child.ID, parent.ID, "alice"); err != nil {
		t.Fatalf("AddParent() error = %v", err)
	}
	// ClaimCard doesn't check parents, so child (still active-category) can
	// be claimed even with an unfinished parent; that gives us an assignee
	// exclusion reason alongside status, dispatchable, and the human label.
	if _, err := db.ClaimCard(ctx, child.ID, "alice"); err != nil {
		t.Fatalf("ClaimCard() error = %v", err)
	}
	if _, err := db.HumanCard(ctx, child.ID, "alice", "needs review"); err != nil {
		t.Fatalf("HumanCard() error = %v", err)
	}
	setDispatchable(t, db, child.ID, false)

	reasons, err := db.ExplainReady(ctx, child.ID)
	if err != nil {
		t.Fatalf("ExplainReady() error = %v", err)
	}
	if len(reasons) < 4 {
		t.Fatalf("ExplainReady() = %v, want at least 4 reasons (status, dispatchable, assignee, human label, unfinished parent)", reasons)
	}
}

func TestExplainReadyUnfinishedParentIncludesID(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	parent := mustCreate(t, db, "parent")
	child := mustCreate(t, db, "child")
	if err := db.AddParent(ctx, child.ID, parent.ID, "alice"); err != nil {
		t.Fatalf("AddParent() error = %v", err)
	}

	reasons, err := db.ExplainReady(ctx, child.ID)
	if err != nil {
		t.Fatalf("ExplainReady() error = %v", err)
	}
	found := false
	for _, r := range reasons {
		if strings.Contains(r, parent.ID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("ExplainReady() = %v, want a reason mentioning parent %s", reasons, parent.ID)
	}
}

func TestExplainReadyNotFound(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.ExplainReady(ctx, "bdd-missing"); err == nil {
		t.Fatalf("ExplainReady() on missing card error = nil, want ErrNotFound")
	}
}
