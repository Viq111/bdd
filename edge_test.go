package bdd

import (
	"context"
	"errors"
	"testing"
)

func mustCreate(t *testing.T, db *DB, title string) *Card {
	t.Helper()
	c, err := db.CreateCard(context.Background(), CreateCard{Title: title, Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	return c
}

func TestAddParentAllFourFormsProduceSameEdge(t *testing.T) {
	ctx := context.Background()

	forms := []struct {
		name string
		add  func(db *DB, parent, child string) error
		rm   func(db *DB, parent, child string) error
	}{
		{"AddParent/RemoveParent", func(db *DB, parent, child string) error { return db.AddParent(ctx, child, parent, "a") }, func(db *DB, parent, child string) error { return db.RemoveParent(ctx, child, parent, "a") }},
		{"AddChild/RemoveChild", func(db *DB, parent, child string) error { return db.AddChild(ctx, parent, child, "a") }, func(db *DB, parent, child string) error { return db.RemoveChild(ctx, parent, child, "a") }},
	}

	for _, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			db := newTestDB(t)
			parent := mustCreate(t, db, "parent")
			child := mustCreate(t, db, "child")

			if err := f.add(db, parent.ID, child.ID); err != nil {
				t.Fatalf("add error = %v", err)
			}

			parents, err := db.Parents(ctx, child.ID)
			if err != nil {
				t.Fatalf("Parents() error = %v", err)
			}
			if len(parents) != 1 || parents[0].ID != parent.ID {
				t.Fatalf("Parents() = %v, want [%s]", parents, parent.ID)
			}
			children, err := db.Children(ctx, parent.ID)
			if err != nil {
				t.Fatalf("Children() error = %v", err)
			}
			if len(children) != 1 || children[0].ID != child.ID {
				t.Fatalf("Children() = %v, want [%s]", children, child.ID)
			}

			if err := f.rm(db, parent.ID, child.ID); err != nil {
				t.Fatalf("remove error = %v", err)
			}
			parents, err = db.Parents(ctx, child.ID)
			if err != nil {
				t.Fatalf("Parents() error = %v", err)
			}
			if len(parents) != 0 {
				t.Fatalf("Parents() = %v, want empty after removal", parents)
			}
		})
	}
}

func TestAddParentIdempotentAndRemoveAbsentIsNoOp(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	parent := mustCreate(t, db, "parent")
	child := mustCreate(t, db, "child")

	if err := db.AddParent(ctx, child.ID, parent.ID, "a"); err != nil {
		t.Fatalf("AddParent() error = %v", err)
	}
	if err := db.AddParent(ctx, child.ID, parent.ID, "a"); err != nil {
		t.Fatalf("AddParent() (repeat) error = %v", err)
	}
	parents, err := db.Parents(ctx, child.ID)
	if err != nil {
		t.Fatalf("Parents() error = %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("Parents() = %v, want exactly one edge after repeated add", parents)
	}

	other := mustCreate(t, db, "other")
	if err := db.RemoveParent(ctx, child.ID, other.ID, "a"); err != nil {
		t.Fatalf("RemoveParent() on absent edge error = %v, want nil (no-op)", err)
	}
}

func TestAddParentRejectsSelfEdge(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	c := mustCreate(t, db, "solo")

	err := db.AddParent(ctx, c.ID, c.ID, "a")
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("AddParent() error = %v, want ErrInvalidArgument", err)
	}
}

func TestAddParentRejectsCycle(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	a := mustCreate(t, db, "a")
	b := mustCreate(t, db, "b")
	c := mustCreate(t, db, "c")

	// a -> b -> c
	if err := db.AddParent(ctx, b.ID, a.ID, "actor"); err != nil {
		t.Fatalf("AddParent() error = %v", err)
	}
	if err := db.AddParent(ctx, c.ID, b.ID, "actor"); err != nil {
		t.Fatalf("AddParent() error = %v", err)
	}

	// c -> a would close the cycle.
	err := db.AddParent(ctx, a.ID, c.ID, "actor")
	if !errors.Is(err, ErrCycle) {
		t.Fatalf("AddParent() error = %v, want ErrCycle", err)
	}

	parents, err := db.Parents(ctx, a.ID)
	if err != nil {
		t.Fatalf("Parents() error = %v", err)
	}
	if len(parents) != 0 {
		t.Fatalf("Parents(a) = %v, want empty: rejected cycle must write nothing", parents)
	}
}

func TestAddParentValidatesCardExistence(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	c := mustCreate(t, db, "c")

	if err := db.AddParent(ctx, c.ID, "bdd-missing", "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddParent() error = %v, want ErrNotFound for missing parent", err)
	}
	if err := db.AddParent(ctx, "bdd-missing", c.ID, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddParent() error = %v, want ErrNotFound for missing child", err)
	}
}

func TestParentsAndChildrenDeterministicOrderAndEmpty(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	child := mustCreate(t, db, "child")
	empty, err := db.Parents(ctx, child.ID)
	if err != nil {
		t.Fatalf("Parents() error = %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("Parents() = %v, want non-nil empty slice", empty)
	}

	var parentIDs []string
	for i := 0; i < 3; i++ {
		p := mustCreate(t, db, "p")
		parentIDs = append(parentIDs, p.ID)
	}
	for _, pid := range parentIDs {
		if err := db.AddParent(ctx, child.ID, pid, "a"); err != nil {
			t.Fatalf("AddParent() error = %v", err)
		}
	}

	got, err := db.Parents(ctx, child.ID)
	if err != nil {
		t.Fatalf("Parents() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Parents() = %v, want 3 entries", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].ID >= got[i].ID {
			t.Fatalf("Parents() not sorted ascending by ID: %v", got)
		}
	}
}

func TestParentsChildrenNotFound(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.Parents(ctx, "bdd-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parents() error = %v, want ErrNotFound", err)
	}
	if _, err := db.Children(ctx, "bdd-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Children() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteCardRequiresForce(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	c := mustCreate(t, db, "c")

	if _, err := db.DeleteCard(ctx, c.ID, "a", false); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("DeleteCard() error = %v, want ErrInvalidArgument", err)
	}
	if _, err := db.GetCard(ctx, c.ID); err != nil {
		t.Fatalf("GetCard() error = %v, want card to still exist", err)
	}
}

func TestDeleteCardMissingReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.DeleteCard(ctx, "bdd-missing", "a", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteCard() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteCardRemovesRecordsAndReportsEdges(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	parent := mustCreate(t, db, "parent")
	target := mustCreate(t, db, "target")
	child := mustCreate(t, db, "child")
	unrelated := mustCreate(t, db, "unrelated")

	if err := db.AddParent(ctx, target.ID, parent.ID, "a"); err != nil {
		t.Fatalf("AddParent() error = %v", err)
	}
	if err := db.AddParent(ctx, child.ID, target.ID, "a"); err != nil {
		t.Fatalf("AddParent() error = %v", err)
	}
	if _, err := db.UpdateCard(ctx, target.ID, UpdateCard{AddLabels: []string{"x"}}); err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	if _, err := db.AddNote(ctx, AddNote{CardID: target.ID, Body: "note", Author: "a"}); err != nil {
		t.Fatalf("AddNote() error = %v", err)
	}

	result, err := db.DeleteCard(ctx, target.ID, "a", true)
	if err != nil {
		t.Fatalf("DeleteCard() error = %v", err)
	}
	if len(result.RemovedParents) != 1 || result.RemovedParents[0] != parent.ID {
		t.Fatalf("RemovedParents = %v, want [%s]", result.RemovedParents, parent.ID)
	}
	if len(result.RemovedChildren) != 1 || result.RemovedChildren[0] != child.ID {
		t.Fatalf("RemovedChildren = %v, want [%s]", result.RemovedChildren, child.ID)
	}

	if _, err := db.GetCard(ctx, target.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCard() after delete error = %v, want ErrNotFound", err)
	}

	childParents, err := db.Parents(ctx, child.ID)
	if err != nil {
		t.Fatalf("Parents(child) error = %v", err)
	}
	if len(childParents) != 0 {
		t.Fatalf("Parents(child) = %v, want empty after deleting its only parent", childParents)
	}

	parentChildren, err := db.Children(ctx, parent.ID)
	if err != nil {
		t.Fatalf("Children(parent) error = %v", err)
	}
	if len(parentChildren) != 0 {
		t.Fatalf("Children(parent) = %v, want empty after deleting its only child", parentChildren)
	}

	// Unrelated cards must be untouched.
	if _, err := db.GetCard(ctx, unrelated.ID); err != nil {
		t.Fatalf("GetCard(unrelated) error = %v, want unaffected card", err)
	}
	if _, err := db.GetCard(ctx, parent.ID); err != nil {
		t.Fatalf("GetCard(parent) error = %v, want parent to survive deletion of its child", err)
	}
	if _, err := db.GetCard(ctx, child.ID); err != nil {
		t.Fatalf("GetCard(child) error = %v, want child to survive deletion of its parent", err)
	}

	var noteCount int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes WHERE card_id = ?`, target.ID).Scan(&noteCount); err != nil {
		t.Fatalf("querying notes: %v", err)
	}
	if noteCount != 0 {
		t.Fatalf("notes for deleted card = %d, want 0", noteCount)
	}

	var edgeCount int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM card_edges WHERE parent_id = ? OR child_id = ?`, target.ID, target.ID).Scan(&edgeCount); err != nil {
		t.Fatalf("querying edges: %v", err)
	}
	if edgeCount != 0 {
		t.Fatalf("edges referencing deleted card = %d, want 0", edgeCount)
	}

	var tombstoneCount int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE subject_kind = 'card' AND subject_key = ? AND action = 'delete'`, target.ID).Scan(&tombstoneCount); err != nil {
		t.Fatalf("querying tombstone event: %v", err)
	}
	if tombstoneCount != 1 {
		t.Fatalf("tombstone events for deleted card = %d, want 1", tombstoneCount)
	}

	var priorEventCount int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE subject_kind = 'card' AND subject_key = ?`, target.ID).Scan(&priorEventCount); err != nil {
		t.Fatalf("querying prior events: %v", err)
	}
	if priorEventCount < 4 {
		t.Fatalf("prior audit events for deleted card = %d, want history retained (create, add_parent, add_child(x2), update, note, delete)", priorEventCount)
	}
}
