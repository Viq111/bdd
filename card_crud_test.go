package bdd

import (
	"context"
	"errors"
	"testing"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := Init(context.Background(), InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateCardChoreNeedsOnlyTitle(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	card, err := db.CreateCard(ctx, CreateCard{Title: "Tidy up", Type: CardTypeChore, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	if card.ID == "" || !hasPrefix(card.ID, "bdd-") {
		t.Fatalf("card.ID = %q, want bdd-<suffix>", card.ID)
	}
	if card.Status != StatusOpen {
		t.Fatalf("card.Status = %q, want %q", card.Status, StatusOpen)
	}
	if card.Priority != 2 {
		t.Fatalf("card.Priority = %d, want 2 (default)", card.Priority)
	}
	if card.Revision != 1 {
		t.Fatalf("card.Revision = %d, want 1", card.Revision)
	}
	if card.CreatedBy != "alice" {
		t.Fatalf("card.CreatedBy = %q, want alice", card.CreatedBy)
	}
	if card.Owner != "" {
		t.Fatalf("card.Owner = %q, want empty by default", card.Owner)
	}
}

func TestCreateCardPreservesOwner(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "Preserve source owner", Type: CardTypeChore, CreatedBy: "creator", Owner: "source-owner"})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	if created.Owner != "source-owner" {
		t.Fatalf("created.Owner = %q, want source-owner", created.Owner)
	}

	got, err := db.GetCard(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetCard() error = %v", err)
	}
	if got.Owner != "source-owner" {
		t.Fatalf("GetCard().Owner = %q, want source-owner", got.Owner)
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func TestCreateCardRequiredFieldMatrix(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		in     CreateCard
		fields []string
	}{
		{"bug missing both", CreateCard{Title: "x", Type: CardTypeBug}, []string{"reproduction", "acceptance"}},
		{"bug missing acceptance", CreateCard{Title: "x", Type: CardTypeBug, Reproduction: ptr("steps")}, []string{"acceptance"}},
		{"task missing acceptance", CreateCard{Title: "x", Type: CardTypeTask}, []string{"acceptance"}},
		{"feature missing acceptance", CreateCard{Title: "x", Type: CardTypeFeature}, []string{"acceptance"}},
		{"epic missing acceptance", CreateCard{Title: "x", Type: CardTypeEpic}, []string{"acceptance"}},
		{"decision missing both", CreateCard{Title: "x", Type: CardTypeDecision}, []string{"description", "design"}},
		{"decision missing design", CreateCard{Title: "x", Type: CardTypeDecision, Description: ptr("d")}, []string{"design"}},
		{"missing title", CreateCard{Type: CardTypeChore}, []string{"title"}},
		{"missing type", CreateCard{Title: "x"}, []string{"type"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.CreateCard(ctx, tc.in)
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("CreateCard() error = %v, want *ValidationError", err)
			}
			if len(verr.Fields) != len(tc.fields) {
				t.Fatalf("Fields = %v, want %v", verr.Fields, tc.fields)
			}
			for i, f := range tc.fields {
				if verr.Fields[i] != f {
					t.Fatalf("Fields = %v, want %v", verr.Fields, tc.fields)
				}
			}
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("error does not satisfy errors.Is(err, ErrInvalidArgument)")
			}
		})
	}
}

func TestCreateCardExplicitEmptyStringSatisfiesRequirement(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	card, err := db.CreateCard(ctx, CreateCard{
		Title:        "Cache corruption",
		Type:         CardTypeBug,
		Reproduction: ptr(""),
		Acceptance:   ptr("N/A"),
	})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	if card.Reproduction != "" {
		t.Fatalf("card.Reproduction = %q, want empty", card.Reproduction)
	}
	if card.Acceptance != "N/A" {
		t.Fatalf("card.Acceptance = %q, want N/A", card.Acceptance)
	}
}

func TestCreateCardCustomTypeHasNoExtraRequiredFields(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Registering custom types is out of scope for this card (bdd-4y7n);
	// insert the type_definitions row directly to exercise the
	// no-extra-required-fields rule for an already-registered custom type.
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO type_definitions (name, built_in) VALUES ('incident', 0)`); err != nil {
		t.Fatalf("seeding custom type: %v", err)
	}

	card, err := db.CreateCard(ctx, CreateCard{Title: "Investigate incident", Type: CardType("incident")})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	if card.Type != CardType("incident") {
		t.Fatalf("card.Type = %q, want incident", card.Type)
	}
}

func TestCreateCardRejectsUnregisteredType(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardType("unregistered")})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("CreateCard() error = %v, want ErrInvalidArgument", err)
	}
}

func TestCreateCardValidationHappensBeforeAnyWrite(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeBug})
	if err == nil {
		t.Fatal("expected error")
	}

	rows, err := db.sql.QueryContext(ctx, "SELECT COUNT(*) FROM cards")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var n int
	for rows.Next() {
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
	}
	if n != 0 {
		t.Fatalf("cards table has %d rows after a rejected create, want 0", n)
	}
}

func TestCreateCardWithLabelsAndNote(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	card, err := db.CreateCard(ctx, CreateCard{
		Title:  "Implement cache",
		Type:   CardTypeChore,
		Labels: []string{"area:cli", "area:cli", "perf"},
		Notes:  ptr("initial context"),
	})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	if len(card.Labels) != 2 {
		t.Fatalf("card.Labels = %v, want 2 deduped labels", card.Labels)
	}

	notes, err := db.Notes(ctx, card.ID)
	if err != nil {
		t.Fatalf("Notes() error = %v", err)
	}
	if len(notes) != 1 || notes[0].Body != "initial context" {
		t.Fatalf("Notes() = %v, want one note with the initial body", notes)
	}
}

func TestCreateCardRejectsInvalidLabel(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore, Labels: []string{""}})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("CreateCard() error = %v, want ErrInvalidArgument", err)
	}
}

func TestCreateCardRejectsNegativePriority(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	p := int32(-1)
	_, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore, Priority: &p})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("CreateCard() error = %v, want ErrInvalidArgument", err)
	}
}

func TestCreateCardGeneratesUniqueIDs(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		card, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
		if err != nil {
			t.Fatalf("CreateCard() error = %v", err)
		}
		if seen[card.ID] {
			t.Fatalf("duplicate ID generated: %s", card.ID)
		}
		seen[card.ID] = true
	}
}

func TestGetCardReturnsFullRecord(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{
		Title:       "Implement cache",
		Type:        CardTypeTask,
		Acceptance:  ptr("done"),
		Worktree:    ptr(".worktrees/cache"),
		Priority:    int32Ptr(20),
		ExternalRef: ptr("EXT-1"),
		Labels:      []string{"area:cli"},
		CreatedBy:   "bob",
	})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	got, err := db.GetCard(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetCard() error = %v", err)
	}
	if got.Title != "Implement cache" || got.Worktree != ".worktrees/cache" || got.Priority != 20 ||
		got.ExternalRef != "EXT-1" || len(got.Labels) != 1 || got.Labels[0] != "area:cli" {
		t.Fatalf("GetCard() = %+v, unexpected", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("GetCard() left CreatedAt/UpdatedAt zero: %+v", got)
	}
	if got.StartedAt != nil || got.ClosedAt != nil || got.DeferUntil != nil {
		t.Fatalf("GetCard() = %+v, want nil nullable timestamps on a fresh card", got)
	}
}

func int32Ptr(v int32) *int32 { return &v }

func TestGetCardNotFound(t *testing.T) {
	db := newTestDB(t)
	_, err := db.GetCard(context.Background(), "bdd-missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCard() error = %v, want ErrNotFound", err)
	}
}

func TestUpdateCardFieldChangesAndRevision(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	updated, err := db.UpdateCard(ctx, created.ID, UpdateCard{
		Title:      ptr("y"),
		Priority:   int32Ptr(5),
		Acceptance: ptr("meets criteria"),
		Actor:      "carol",
	})
	if err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	if updated.Title != "y" || updated.Priority != 5 || updated.Acceptance != "meets criteria" {
		t.Fatalf("UpdateCard() = %+v, unexpected", updated)
	}
	if updated.Revision != created.Revision+1 {
		t.Fatalf("Revision = %d, want %d", updated.Revision, created.Revision+1)
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) && !updated.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("UpdatedAt did not advance")
	}
}

func TestUpdateCardExplicitEmptyClearsField(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{
		Title:        "bug",
		Type:         CardTypeBug,
		Reproduction: ptr("repro steps"),
		Acceptance:   ptr("fixed"),
	})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	updated, err := db.UpdateCard(ctx, created.ID, UpdateCard{Reproduction: ptr("")})
	if err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	if updated.Reproduction != "" {
		t.Fatalf("Reproduction = %q, want cleared", updated.Reproduction)
	}
}

func TestUpdateCardOmittedFieldUnchanged(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore, ExternalRef: ptr("EXT-1")})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	updated, err := db.UpdateCard(ctx, created.ID, UpdateCard{Title: ptr("y")})
	if err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	if updated.ExternalRef != "EXT-1" {
		t.Fatalf("ExternalRef = %q, want unchanged EXT-1", updated.ExternalRef)
	}
}

func TestUpdateCardCanChangeTypeWithoutRerunningCreationRules(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	bug := CardTypeBug
	updated, err := db.UpdateCard(ctx, created.ID, UpdateCard{Type: &bug})
	if err != nil {
		t.Fatalf("UpdateCard() error = %v, want success even though reproduction/acceptance are still unset", err)
	}
	if updated.Type != CardTypeBug {
		t.Fatalf("Type = %q, want bug", updated.Type)
	}
}

func TestUpdateCardWorktreeSetAndClear(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	updated, err := db.UpdateCard(ctx, created.ID, UpdateCard{Worktree: ptr(".worktrees/cache")})
	if err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	if updated.Worktree != ".worktrees/cache" {
		t.Fatalf("Worktree = %q, want .worktrees/cache", updated.Worktree)
	}

	cleared, err := db.UpdateCard(ctx, created.ID, UpdateCard{ClearWorktree: true})
	if err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	if cleared.Worktree != "" {
		t.Fatalf("Worktree = %q, want cleared", cleared.Worktree)
	}

	_, err = db.UpdateCard(ctx, created.ID, UpdateCard{Worktree: ptr("x"), ClearWorktree: true})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("UpdateCard() error = %v, want ErrInvalidArgument for conflicting worktree flags", err)
	}
}

func TestUpdateCardLabelsIdempotentAddRemove(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore, Labels: []string{"a"}})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	updated, err := db.UpdateCard(ctx, created.ID, UpdateCard{AddLabels: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	if len(updated.Labels) != 2 {
		t.Fatalf("Labels = %v, want [a b]", updated.Labels)
	}

	updated, err = db.UpdateCard(ctx, created.ID, UpdateCard{RemoveLabels: []string{"a", "missing"}})
	if err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	if len(updated.Labels) != 1 || updated.Labels[0] != "b" {
		t.Fatalf("Labels = %v, want [b]", updated.Labels)
	}
}

func TestUpdateCardRejectsInvalidRemoveLabel(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	_, err = db.UpdateCard(ctx, created.ID, UpdateCard{RemoveLabels: []string{""}})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("UpdateCard() error = %v, want ErrInvalidArgument for empty remove label", err)
	}

	_, err = db.UpdateCard(ctx, created.ID, UpdateCard{RemoveLabels: []string{"\xff\xfe"}})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("UpdateCard() error = %v, want ErrInvalidArgument for invalid UTF-8 remove label", err)
	}

	unchanged, err := db.GetCard(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetCard() error = %v", err)
	}
	if unchanged.Revision != created.Revision {
		t.Fatalf("Revision = %d, want unchanged %d after rejected UpdateCard", unchanged.Revision, created.Revision)
	}
}

func TestAddLabelAddsAndIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	updated, err := db.AddLabel(ctx, created.ID, "a", "alice")
	if err != nil {
		t.Fatalf("AddLabel() error = %v", err)
	}
	if len(updated.Labels) != 1 || updated.Labels[0] != "a" {
		t.Fatalf("Labels = %v, want [a]", updated.Labels)
	}
	if updated.Revision != created.Revision+1 {
		t.Fatalf("Revision = %d, want %d", updated.Revision, created.Revision+1)
	}

	again, err := db.AddLabel(ctx, created.ID, "a", "alice")
	if err != nil {
		t.Fatalf("AddLabel() (repeat) error = %v", err)
	}
	if len(again.Labels) != 1 || again.Labels[0] != "a" {
		t.Fatalf("Labels = %v, want [a] after idempotent add", again.Labels)
	}

	var eventActor string
	if err := db.sql.QueryRowContext(ctx, `SELECT actor FROM events WHERE subject_kind = 'card' AND subject_key = ? AND action = 'update' AND revision = ?`, created.ID, updated.Revision).Scan(&eventActor); err != nil {
		t.Fatalf("querying add-label event: %v", err)
	}
	if eventActor != "alice" {
		t.Fatalf("event actor = %q, want alice", eventActor)
	}
}

func TestAddLabelRejectsInvalidLabel(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	if _, err := db.AddLabel(ctx, created.ID, "", "alice"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("AddLabel() error = %v, want ErrInvalidArgument for empty label", err)
	}
}

func TestAddLabelMissingCard(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.AddLabel(ctx, "bdd-missing", "a", "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddLabel() error = %v, want ErrNotFound", err)
	}
}

func TestRemoveLabelRemovesAndIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore, Labels: []string{"a"}})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	updated, err := db.RemoveLabel(ctx, created.ID, "a", "bob")
	if err != nil {
		t.Fatalf("RemoveLabel() error = %v", err)
	}
	if len(updated.Labels) != 0 {
		t.Fatalf("Labels = %v, want empty", updated.Labels)
	}
	if updated.Revision != created.Revision+1 {
		t.Fatalf("Revision = %d, want %d", updated.Revision, created.Revision+1)
	}

	again, err := db.RemoveLabel(ctx, created.ID, "a", "bob")
	if err != nil {
		t.Fatalf("RemoveLabel() (repeat) error = %v", err)
	}
	if len(again.Labels) != 0 {
		t.Fatalf("Labels = %v, want still empty after idempotent remove", again.Labels)
	}

	var eventActor string
	if err := db.sql.QueryRowContext(ctx, `SELECT actor FROM events WHERE subject_kind = 'card' AND subject_key = ? AND action = 'update' AND revision = ?`, created.ID, updated.Revision).Scan(&eventActor); err != nil {
		t.Fatalf("querying remove-label event: %v", err)
	}
	if eventActor != "bob" {
		t.Fatalf("event actor = %q, want bob", eventActor)
	}
}

func TestRemoveLabelMissingCard(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.RemoveLabel(ctx, "bdd-missing", "a", "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RemoveLabel() error = %v, want ErrNotFound", err)
	}
}

func TestCreateCardRejectsInvalidUTF8(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	tests := []struct {
		name string
		in   CreateCard
	}{
		{"title", CreateCard{Title: "bad \xff\xfe", Type: CardTypeChore}},
		{"description", CreateCard{Title: "x", Type: CardTypeChore, Description: ptr("bad \xff\xfe")}},
		{"reproduction", CreateCard{Title: "x", Type: CardTypeChore, Reproduction: ptr("bad \xff\xfe")}},
		{"design", CreateCard{Title: "x", Type: CardTypeChore, Design: ptr("bad \xff\xfe")}},
		{"acceptance", CreateCard{Title: "x", Type: CardTypeChore, Acceptance: ptr("bad \xff\xfe")}},
		{"external_ref", CreateCard{Title: "x", Type: CardTypeChore, ExternalRef: ptr("bad \xff\xfe")}},
		{"worktree", CreateCard{Title: "x", Type: CardTypeChore, Worktree: ptr("bad \xff\xfe")}},
		{"notes", CreateCard{Title: "x", Type: CardTypeChore, Notes: ptr("bad \xff\xfe")}},
		{"owner", CreateCard{Title: "x", Type: CardTypeChore, Owner: "bad \xff\xfe"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.CreateCard(ctx, tc.in)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("CreateCard() error = %v, want ErrInvalidArgument for invalid UTF-8 %s", err, tc.name)
			}
		})
	}
}

func TestUpdateCardRejectsInvalidUTF8(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	tests := []struct {
		name string
		in   UpdateCard
	}{
		{"title", UpdateCard{Title: ptr("bad \xff\xfe")}},
		{"description", UpdateCard{Description: ptr("bad \xff\xfe")}},
		{"reproduction", UpdateCard{Reproduction: ptr("bad \xff\xfe")}},
		{"design", UpdateCard{Design: ptr("bad \xff\xfe")}},
		{"acceptance", UpdateCard{Acceptance: ptr("bad \xff\xfe")}},
		{"external_ref", UpdateCard{ExternalRef: ptr("bad \xff\xfe")}},
		{"worktree", UpdateCard{Worktree: ptr("bad \xff\xfe")}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.UpdateCard(ctx, created.ID, tc.in)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("UpdateCard() error = %v, want ErrInvalidArgument for invalid UTF-8 %s", err, tc.name)
			}

			unchanged, err := db.GetCard(ctx, created.ID)
			if err != nil {
				t.Fatalf("GetCard() error = %v", err)
			}
			if unchanged.Revision != created.Revision {
				t.Fatalf("Revision = %d, want unchanged %d after rejected UpdateCard", unchanged.Revision, created.Revision)
			}
		})
	}
}

func TestAddNoteRejectsInvalidUTF8(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	_, err = db.AddNote(ctx, AddNote{CardID: created.ID, Body: "bad \xff\xfe", Author: "alice"})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("AddNote() error = %v, want ErrInvalidArgument for invalid UTF-8 body", err)
	}
}

func TestUpdateCardNotFound(t *testing.T) {
	db := newTestDB(t)
	_, err := db.UpdateCard(context.Background(), "bdd-missing", UpdateCard{Title: ptr("x")})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateCard() error = %v, want ErrNotFound", err)
	}
}

func TestUpdateCardRejectsClearingTitle(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	_, err = db.UpdateCard(ctx, created.ID, UpdateCard{Title: ptr("")})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("UpdateCard() error = %v, want ErrInvalidArgument", err)
	}
}

func TestAddNoteAppendsChronologically(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	n1, err := db.AddNote(ctx, AddNote{CardID: created.ID, Body: "first", Author: "alice"})
	if err != nil {
		t.Fatalf("AddNote() error = %v", err)
	}
	n2, err := db.AddNote(ctx, AddNote{CardID: created.ID, Body: "second", Author: "bob"})
	if err != nil {
		t.Fatalf("AddNote() error = %v", err)
	}
	if n1.ID == n2.ID {
		t.Fatalf("expected distinct note IDs")
	}

	notes, err := db.Notes(ctx, created.ID)
	if err != nil {
		t.Fatalf("Notes() error = %v", err)
	}
	if len(notes) != 2 || notes[0].Body != "first" || notes[1].Body != "second" {
		t.Fatalf("Notes() = %v, want [first second] in order", notes)
	}
}

func TestAddNoteIncrementsCardRevision(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	if created.Revision != 1 {
		t.Fatalf("created.Revision = %d, want 1", created.Revision)
	}

	if _, err := db.AddNote(ctx, AddNote{CardID: created.ID, Body: "note", Author: "alice"}); err != nil {
		t.Fatalf("AddNote() error = %v", err)
	}

	got, err := db.GetCard(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetCard() error = %v", err)
	}
	if got.Revision != 2 {
		t.Fatalf("GetCard().Revision = %d, want 2", got.Revision)
	}

	var eventRevision int64
	if err := db.sql.QueryRowContext(ctx, `SELECT revision FROM events WHERE subject_kind = 'card' AND subject_key = ? AND action = 'note'`, created.ID).Scan(&eventRevision); err != nil {
		t.Fatalf("querying note event: %v", err)
	}
	if eventRevision != 2 {
		t.Fatalf("note event revision = %d, want 2", eventRevision)
	}
}

func TestAddNoteRequiresBodyAndExistingCard(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	if _, err := db.AddNote(ctx, AddNote{CardID: created.ID, Body: ""}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("AddNote() error = %v, want ErrInvalidArgument", err)
	}
	if _, err := db.AddNote(ctx, AddNote{CardID: "bdd-missing", Body: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddNote() error = %v, want ErrNotFound", err)
	}
}

func TestNotesOnMissingCardReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	_, err := db.Notes(context.Background(), "bdd-missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Notes() error = %v, want ErrNotFound", err)
	}
}

func TestCreateCardRejectsMissingParent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore, Parents: []string{"bdd-abc123"}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateCard() error = %v, want ErrNotFound", err)
	}

	rows, err := db.sql.QueryContext(ctx, "SELECT COUNT(*) FROM cards")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var n int
	for rows.Next() {
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
	}
	if n != 0 {
		t.Fatalf("cards table has %d rows after a rejected create with a missing parent, want 0", n)
	}
}

func TestCreateCardWithParentsAttachesEdges(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	p1, err := db.CreateCard(ctx, CreateCard{Title: "p1", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	p2, err := db.CreateCard(ctx, CreateCard{Title: "p2", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	child, err := db.CreateCard(ctx, CreateCard{Title: "child", Type: CardTypeChore, Parents: []string{p1.ID, p2.ID, p1.ID}})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	parents, err := db.Parents(ctx, child.ID)
	if err != nil {
		t.Fatalf("Parents() error = %v", err)
	}
	if len(parents) != 2 {
		t.Fatalf("Parents() = %v, want 2 deduped parents", parents)
	}
	got := map[string]bool{parents[0].ID: true, parents[1].ID: true}
	if !got[p1.ID] || !got[p2.ID] {
		t.Fatalf("Parents() = %v, want %s and %s", parents, p1.ID, p2.ID)
	}
	if parents[0].ID >= parents[1].ID {
		t.Fatalf("Parents() = %v, want ascending ID order", parents)
	}
}

// TestGetCardExposesParentLinks covers bdd-otzf's acceptance criterion "A
// card created with multiple parents shows them all in GetCard" (bdd-ej0):
// GetCard must expand parent edges itself rather than requiring a separate
// Parents() call.
func TestGetCardExposesParentLinks(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	p1, err := db.CreateCard(ctx, CreateCard{Title: "p1", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	p2, err := db.CreateCard(ctx, CreateCard{Title: "p2", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	child, err := db.CreateCard(ctx, CreateCard{Title: "child", Type: CardTypeChore, Parents: []string{p1.ID, p2.ID}})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	// CreateCard's own returned card should already reflect the attached
	// parents, since it loads the card the same way GetCard does.
	if len(child.Parents) != 2 {
		t.Fatalf("CreateCard() result Parents = %v, want 2 parents", child.Parents)
	}

	got, err := db.GetCard(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetCard() error = %v", err)
	}
	if len(got.Parents) != 2 {
		t.Fatalf("GetCard() Parents = %v, want 2 parents", got.Parents)
	}
	seen := map[string]bool{got.Parents[0].ID: true, got.Parents[1].ID: true}
	if !seen[p1.ID] || !seen[p2.ID] {
		t.Fatalf("GetCard() Parents = %v, want %s and %s", got.Parents, p1.ID, p2.ID)
	}
	if got.Parents[0].ID >= got.Parents[1].ID {
		t.Fatalf("GetCard() Parents = %v, want ascending ID order", got.Parents)
	}
	if got.Parents[0].Title == "" || got.Parents[0].Type == "" || got.Parents[0].Status == "" {
		t.Fatalf("GetCard() Parents = %+v, want title/type/status populated on each CardRef", got.Parents)
	}

	// A card with no parents gets an empty (not nil) slice.
	if got := p1; got.Parents == nil || len(got.Parents) != 0 {
		t.Fatalf("GetCard() Parents = %v, want empty non-nil slice for a parentless card", got.Parents)
	}
}

// TestGetCardExposesChildLinks guards against the regression fixed in
// bdd-ui9: GetCard must expand child edges itself, mirroring its parent-edge
// expansion, rather than requiring a separate Children() call.
func TestGetCardExposesChildLinks(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	parent, err := db.CreateCard(ctx, CreateCard{Title: "parent", Type: CardTypeChore})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	c1, err := db.CreateCard(ctx, CreateCard{Title: "c1", Type: CardTypeChore, Parents: []string{parent.ID}})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	c2, err := db.CreateCard(ctx, CreateCard{Title: "c2", Type: CardTypeChore, Parents: []string{parent.ID}})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}

	got, err := db.GetCard(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetCard() error = %v", err)
	}
	if len(got.Children) != 2 {
		t.Fatalf("GetCard() Children = %v, want 2 children", got.Children)
	}
	seen := map[string]bool{got.Children[0].ID: true, got.Children[1].ID: true}
	if !seen[c1.ID] || !seen[c2.ID] {
		t.Fatalf("GetCard() Children = %v, want %s and %s", got.Children, c1.ID, c2.ID)
	}
	if got.Children[0].ID >= got.Children[1].ID {
		t.Fatalf("GetCard() Children = %v, want ascending ID order", got.Children)
	}
	if got.Children[0].Title == "" || got.Children[0].Type == "" || got.Children[0].Status == "" {
		t.Fatalf("GetCard() Children = %+v, want title/type/status populated on each CardRef", got.Children)
	}

	// A card with no children gets an empty (not nil) slice.
	if got := c1; got.Children == nil || len(got.Children) != 0 {
		t.Fatalf("GetCard() Children = %v, want empty non-nil slice for a childless card", got.Children)
	}
}
