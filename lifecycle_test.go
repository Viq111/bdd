package bdd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestClaimCardMovesActiveToInProgress(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "claim me")

	got, err := db.ClaimCard(ctx, card.ID, "alice")
	if err != nil {
		t.Fatalf("ClaimCard() error = %v", err)
	}
	if got.Status != StatusInProgress {
		t.Fatalf("Status = %q, want %q", got.Status, StatusInProgress)
	}
	if got.Assignee != "alice" {
		t.Fatalf("Assignee = %q, want alice", got.Assignee)
	}
	if got.StartedAt == nil {
		t.Fatalf("StartedAt = nil, want set")
	}
}

func TestClaimCardIdempotentForSameActor(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "claim me")
	if _, err := db.UpdateCard(ctx, card.ID, UpdateCard{AddLabels: []string{"x"}}); err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}

	first, err := db.ClaimCard(ctx, card.ID, "alice")
	if err != nil {
		t.Fatalf("ClaimCard() error = %v", err)
	}
	second, err := db.ClaimCard(ctx, card.ID, "alice")
	if err != nil {
		t.Fatalf("ClaimCard() second call error = %v", err)
	}
	if second.Revision != first.Revision {
		t.Fatalf("Revision = %d, want unchanged %d", second.Revision, first.Revision)
	}
	if !second.StartedAt.Equal(*first.StartedAt) {
		t.Fatalf("StartedAt changed on idempotent reclaim: %v -> %v", first.StartedAt, second.StartedAt)
	}
	// The idempotent no-op path returns loadCard's full expansion (labels,
	// parents), not just the lightweight status+assignee read ClaimCard's
	// hot path uses to decide whether a write is needed.
	if len(second.Labels) != 1 || second.Labels[0] != "x" {
		t.Fatalf("Labels = %v, want [x]", second.Labels)
	}
}

func TestClaimCardByDifferentActorReturnsErrClaimed(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "claim me")

	if _, err := db.ClaimCard(ctx, card.ID, "alice"); err != nil {
		t.Fatalf("ClaimCard() error = %v", err)
	}
	if _, err := db.ClaimCard(ctx, card.ID, "bob"); !errors.Is(err, ErrClaimed) {
		t.Fatalf("ClaimCard() by bob error = %v, want ErrClaimed", err)
	}
}

func TestClaimCardNonActiveReturnsErrInvalidTransition(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "closed already")

	if _, err := db.CloseCard(ctx, card.ID, CloseCard{Actor: "alice"}); err != nil {
		t.Fatalf("CloseCard() error = %v", err)
	}
	if _, err := db.ClaimCard(ctx, card.ID, "alice"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ClaimCard() on closed card error = %v, want ErrInvalidTransition", err)
	}
}

func TestClaimCardRequiresActor(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "claim me")

	if _, err := db.ClaimCard(ctx, card.ID, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ClaimCard() with empty actor error = %v, want ErrInvalidArgument", err)
	}
}

func TestUpdateCardClaimPlusFieldChangeIsAtomic(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "claim and update")

	updated, err := db.UpdateCard(ctx, card.ID, UpdateCard{
		Claim:    true,
		Priority: int32Ptr(5),
		Actor:    "alice",
	})
	if err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	if updated.Status != StatusInProgress || updated.Assignee != "alice" {
		t.Fatalf("card = %+v, want claimed by alice", updated)
	}
	if updated.Priority != 5 {
		t.Fatalf("Priority = %d, want 5", updated.Priority)
	}
	if updated.Revision != card.Revision+1 {
		t.Fatalf("Revision = %d, want exactly one bump (%d)", updated.Revision, card.Revision+1)
	}
}

func TestUpdateCardClaimAndStatusIsRejected(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "claim and status")

	st := StatusInProgress
	if _, err := db.UpdateCard(ctx, card.ID, UpdateCard{Claim: true, Status: &st, Actor: "alice"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("UpdateCard() error = %v, want ErrInvalidArgument", err)
	}

	// The rejection must happen before anything is written.
	got, err := db.GetCard(ctx, card.ID)
	if err != nil {
		t.Fatalf("GetCard() error = %v", err)
	}
	if got.Status != StatusOpen || got.Assignee != "" || got.Revision != card.Revision {
		t.Fatalf("card = %+v, want unchanged", got)
	}
}

func TestUpdateCardClaimPlusFailingFieldChangeCommitsNothing(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "claim then fail")

	var eventCountBefore int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE subject_kind = 'card' AND subject_key = ?`, card.ID).Scan(&eventCountBefore); err != nil {
		t.Fatalf("counting events: %v", err)
	}

	// Referencing a nonexistent parent card fails the field-change portion
	// (a FOREIGN KEY / not-found style error) after the claim would already
	// have been decided; the whole transaction must still roll back.
	_, err := db.UpdateCard(ctx, card.ID, UpdateCard{
		Claim:      true,
		AddParents: []string{"bdd-does-not-exist"},
		Actor:      "alice",
	})
	if err == nil {
		t.Fatalf("UpdateCard() error = nil, want failure from unknown parent")
	}

	got, err := db.GetCard(ctx, card.ID)
	if err != nil {
		t.Fatalf("GetCard() error = %v", err)
	}
	if got.Status != StatusOpen || got.Assignee != "" || got.StartedAt != nil {
		t.Fatalf("card = %+v, want unclaimed (transaction should not have partially applied)", got)
	}
	if got.Revision != card.Revision {
		t.Fatalf("Revision = %d, want unchanged %d", got.Revision, card.Revision)
	}
	if !got.UpdatedAt.Equal(card.UpdatedAt) {
		t.Fatalf("UpdatedAt = %v, want unchanged %v", got.UpdatedAt, card.UpdatedAt)
	}

	var eventCountAfter int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE subject_kind = 'card' AND subject_key = ?`, card.ID).Scan(&eventCountAfter); err != nil {
		t.Fatalf("counting events: %v", err)
	}
	if eventCountAfter != eventCountBefore {
		t.Fatalf("event count = %d, want unchanged %d", eventCountAfter, eventCountBefore)
	}
}

func TestUpdateCardClaimIdempotentNoOpForSameActor(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "claim me")

	first, err := db.UpdateCard(ctx, card.ID, UpdateCard{Claim: true, Actor: "alice"})
	if err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	second, err := db.UpdateCard(ctx, card.ID, UpdateCard{Claim: true, Actor: "alice"})
	if err != nil {
		t.Fatalf("UpdateCard() second call error = %v", err)
	}
	if second.Revision != first.Revision {
		t.Fatalf("Revision = %d, want unchanged %d", second.Revision, first.Revision)
	}
}

func TestCloseCardSetsClosedAtAndAppendsReasonNote(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "close me")

	got, err := db.CloseCard(ctx, card.ID, CloseCard{Reason: "done deal", Actor: "alice"})
	if err != nil {
		t.Fatalf("CloseCard() error = %v", err)
	}
	if got.Status != StatusClosed {
		t.Fatalf("Status = %q, want %q", got.Status, StatusClosed)
	}
	if got.ClosedAt == nil {
		t.Fatalf("ClosedAt = nil, want set")
	}

	notes, err := db.Notes(ctx, card.ID)
	if err != nil {
		t.Fatalf("Notes() error = %v", err)
	}
	if len(notes) != 1 || notes[0].Body != "done deal" {
		t.Fatalf("Notes() = %v, want one note body %q", notes, "done deal")
	}
}

func TestCloseCardIdempotent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "close me")

	first, err := db.CloseCard(ctx, card.ID, CloseCard{Actor: "alice"})
	if err != nil {
		t.Fatalf("CloseCard() error = %v", err)
	}
	second, err := db.CloseCard(ctx, card.ID, CloseCard{Actor: "alice"})
	if err != nil {
		t.Fatalf("CloseCard() second call error = %v", err)
	}
	if second.Revision != first.Revision {
		t.Fatalf("Revision = %d, want unchanged %d", second.Revision, first.Revision)
	}
	if !second.ClosedAt.Equal(*first.ClosedAt) {
		t.Fatalf("ClosedAt changed on idempotent reclose: %v -> %v", first.ClosedAt, second.ClosedAt)
	}
}

func TestCloseCardRejectsInvalidUTF8Reason(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "close me")

	_, err := db.CloseCard(ctx, card.ID, CloseCard{Reason: "bad \xff\xfe", Actor: "alice"})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("CloseCard() error = %v, want ErrInvalidArgument for invalid UTF-8 reason", err)
	}
}

func TestCloseCardFromWIPAndFrozen(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	wip := mustCreate(t, db, "wip")
	if _, err := db.ClaimCard(ctx, wip.ID, "alice"); err != nil {
		t.Fatalf("ClaimCard() error = %v", err)
	}
	if got, err := db.CloseCard(ctx, wip.ID, CloseCard{Actor: "alice"}); err != nil || got.Status != StatusClosed {
		t.Fatalf("CloseCard() from wip = (%v, %v), want (closed, nil)", got, err)
	}

	frozen := mustCreate(t, db, "frozen")
	if _, err := db.DeferCard(ctx, frozen.ID, "alice", nil); err != nil {
		t.Fatalf("DeferCard() error = %v", err)
	}
	if got, err := db.CloseCard(ctx, frozen.ID, CloseCard{Actor: "alice"}); err != nil || got.Status != StatusClosed {
		t.Fatalf("CloseCard() from frozen = (%v, %v), want (closed, nil)", got, err)
	}
}

func TestReopenCardClearsFields(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "reopen me")

	if _, err := db.ClaimCard(ctx, card.ID, "alice"); err != nil {
		t.Fatalf("ClaimCard() error = %v", err)
	}
	if _, err := db.CloseCard(ctx, card.ID, CloseCard{Actor: "alice"}); err != nil {
		t.Fatalf("CloseCard() error = %v", err)
	}

	got, err := db.ReopenCard(ctx, card.ID, "bob")
	if err != nil {
		t.Fatalf("ReopenCard() error = %v", err)
	}
	if got.Status != StatusOpen {
		t.Fatalf("Status = %q, want %q", got.Status, StatusOpen)
	}
	if got.ClosedAt != nil || got.StartedAt != nil || got.Assignee != "" {
		t.Fatalf("ReopenCard() did not clear fields: ClosedAt=%v StartedAt=%v Assignee=%q", got.ClosedAt, got.StartedAt, got.Assignee)
	}
}

func TestReopenCardNonDoneReturnsErrInvalidTransition(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "still open")

	if _, err := db.ReopenCard(ctx, card.ID, "alice"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ReopenCard() on open card error = %v, want ErrInvalidTransition", err)
	}
}

func TestDeferCardSetsStatusAndUntil(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "defer me")

	until := time.Now().Add(48 * time.Hour)
	got, err := db.DeferCard(ctx, card.ID, "alice", &until)
	if err != nil {
		t.Fatalf("DeferCard() error = %v", err)
	}
	if got.Status != StatusDeferred {
		t.Fatalf("Status = %q, want %q", got.Status, StatusDeferred)
	}
	if got.DeferUntil == nil || got.DeferUntil.Unix() != until.Unix() {
		t.Fatalf("DeferUntil = %v, want ~%v", got.DeferUntil, until)
	}
}

func TestDeferCardWithoutUntilLeavesItUnset(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "defer me")

	got, err := db.DeferCard(ctx, card.ID, "alice", nil)
	if err != nil {
		t.Fatalf("DeferCard() error = %v", err)
	}
	if got.DeferUntil != nil {
		t.Fatalf("DeferUntil = %v, want nil", got.DeferUntil)
	}
}

func TestDeferCardDoneReturnsErrInvalidTransition(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "closed")

	if _, err := db.CloseCard(ctx, card.ID, CloseCard{Actor: "alice"}); err != nil {
		t.Fatalf("CloseCard() error = %v", err)
	}
	if _, err := db.DeferCard(ctx, card.ID, "alice", nil); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("DeferCard() on closed card error = %v, want ErrInvalidTransition", err)
	}
}

func TestDeferCardNeverAppliesAutomatically(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "defer me")

	past := time.Now().Add(-time.Hour)
	got, err := db.DeferCard(ctx, card.ID, "alice", &past)
	if err != nil {
		t.Fatalf("DeferCard() error = %v", err)
	}
	if got.Status != StatusDeferred {
		t.Fatalf("Status = %q, want %q (an elapsed defer_until must not auto-revert)", got.Status, StatusDeferred)
	}

	reread, err := db.GetCard(ctx, card.ID)
	if err != nil {
		t.Fatalf("GetCard() error = %v", err)
	}
	if reread.Status != StatusDeferred {
		t.Fatalf("Status after re-read = %q, want %q", reread.Status, StatusDeferred)
	}
}

func TestHumanCardAddsLabelAndNote(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "needs a human")

	got, err := db.HumanCard(ctx, card.ID, "alice", "ambiguous acceptance criteria")
	if err != nil {
		t.Fatalf("HumanCard() error = %v", err)
	}
	if len(got.Labels) != 1 || got.Labels[0] != HumanLabel {
		t.Fatalf("Labels = %v, want [%s]", got.Labels, HumanLabel)
	}

	notes, err := db.Notes(ctx, card.ID)
	if err != nil {
		t.Fatalf("Notes() error = %v", err)
	}
	if len(notes) != 1 || notes[0].Body != "ambiguous acceptance criteria" {
		t.Fatalf("Notes() = %v, want one note", notes)
	}

	// Idempotent: a second call does not duplicate the label.
	got2, err := db.HumanCard(ctx, card.ID, "bob", "")
	if err != nil {
		t.Fatalf("HumanCard() second call error = %v", err)
	}
	if len(got2.Labels) != 1 {
		t.Fatalf("Labels after second HumanCard() = %v, want a single human label", got2.Labels)
	}
}

func TestHumanCardRejectsInvalidUTF8Reason(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "needs a human")

	_, err := db.HumanCard(ctx, card.ID, "alice", "bad \xff\xfe")
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("HumanCard() error = %v, want ErrInvalidArgument for invalid UTF-8 reason", err)
	}
}

func TestHumanCardNotFound(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.HumanCard(ctx, "bdd-missing", "alice", "why"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("HumanCard() on missing card error = %v, want ErrNotFound", err)
	}
}

func TestUpdateCardStatusLeavingDoneRequiresReopen(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "closed")

	if err := db.ConfigSet(ctx, ConfigKeyStatusCustom, "wontfix:done", "alice"); err != nil {
		t.Fatalf("ConfigSet(status.custom) error = %v", err)
	}

	if _, err := db.CloseCard(ctx, card.ID, CloseCard{Actor: "alice"}); err != nil {
		t.Fatalf("CloseCard() error = %v", err)
	}

	open := StatusOpen
	if _, err := db.UpdateCard(ctx, card.ID, UpdateCard{Status: &open, Actor: "alice"}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("UpdateCard(status=open) on closed card error = %v, want ErrInvalidTransition", err)
	}

	wontfix := Status("wontfix")
	if got, err := db.UpdateCard(ctx, card.ID, UpdateCard{Status: &wontfix, Actor: "alice"}); err != nil || got.Status != wontfix {
		t.Fatalf("UpdateCard(status=wontfix) on closed card = (%v, %v), want (wontfix, nil)", got, err)
	}
}

func TestUpdateCardEdgesAtomicWithFields(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	child := mustCreate(t, db, "child")
	parent := mustCreate(t, db, "parent")
	other := mustCreate(t, db, "other")

	title := "renamed while linking"
	got, err := db.UpdateCard(ctx, child.ID, UpdateCard{
		Title:       &title,
		AddParents:  []string{parent.ID},
		AddChildren: []string{other.ID},
		Actor:       "alice",
	})
	if err != nil {
		t.Fatalf("UpdateCard() error = %v", err)
	}
	if got.Title != title {
		t.Fatalf("Title = %q, want %q", got.Title, title)
	}
	if len(got.Parents) != 1 || got.Parents[0].ID != parent.ID {
		t.Fatalf("Parents = %v, want [%s]", got.Parents, parent.ID)
	}

	children, err := db.Children(ctx, child.ID)
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}
	if len(children) != 1 || children[0].ID != other.ID {
		t.Fatalf("Children() = %v, want [%s]", children, other.ID)
	}

	got2, err := db.UpdateCard(ctx, child.ID, UpdateCard{RemoveParents: []string{parent.ID}, Actor: "alice"})
	if err != nil {
		t.Fatalf("UpdateCard() remove parent error = %v", err)
	}
	if len(got2.Parents) != 0 {
		t.Fatalf("Parents after remove = %v, want empty", got2.Parents)
	}
}

func TestUpdateCardAddParentsRejectsCycle(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	a := mustCreate(t, db, "a")
	b := mustCreate(t, db, "b")

	if err := db.AddParent(ctx, b.ID, a.ID, "alice"); err != nil {
		t.Fatalf("AddParent() error = %v", err)
	}

	if _, err := db.UpdateCard(ctx, a.ID, UpdateCard{AddParents: []string{b.ID}, Actor: "alice"}); !errors.Is(err, ErrCycle) {
		t.Fatalf("UpdateCard(AddParents cycle) error = %v, want ErrCycle", err)
	}
}

// TestCloseCardWithPreIsolatesConcurrentWriters ensures the pre-state
// returned by CloseCardWithPre reflects only this call's own transaction,
// never a concurrent writer's commit racing in between a separate pre-read
// and the mutation. Regression test for the same class of misattribution
// bug fixed for label/status hooks by UpdateCardWithPre (bd bdd-o0zu),
// applied here to the lifecycle-specific status-change mutations
// (CloseCard/ReopenCard/DeferCard) driving the `close`/`reopen`/`defer`
// commands' status-change hook.
//
// Every call passes a non-empty Reason, so CloseCard always bumps the
// revision (even when the status itself is already closed and the call is
// otherwise a no-op), giving every successful call a distinct, strictly
// increasing revision that totally orders the calls' commits. Once sorted
// by revision, each call's pre.Status must equal the previous call's
// post.Status: any break in that chain means a pre-read observed state
// other than what its own transaction actually wrote over.
func TestCloseCardWithPreIsolatesConcurrentWriters(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	card := mustCreate(t, db, "close me")

	const n = 12
	type result struct {
		revision   int64
		preStatus  Status
		postStatus Status
	}
	results := make([]result, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			post, pre, err := db.CloseCardWithPre(ctx, card.ID, CloseCard{Reason: fmt.Sprintf("note %d", i), Actor: "alice"})
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = result{revision: post.Revision, preStatus: pre.Status, postStatus: post.Status}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("CloseCardWithPre(%d) error = %v", i, err)
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].revision < results[j].revision })
	prevStatus := StatusOpen
	for _, r := range results {
		if r.preStatus != prevStatus {
			t.Fatalf("revision %d: pre.Status = %s, want %s (chain broken: pre-read raced a concurrent writer's commit)", r.revision, r.preStatus, prevStatus)
		}
		prevStatus = r.postStatus
	}
}
