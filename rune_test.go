package bdd

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func newRuneTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := Init(context.Background(), InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func ptr[T any](v T) *T { return &v }

func TestPutRuneCreatesWithDefaults(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()

	r, err := db.PutRune(ctx, PutRune{
		Key:  "role/programmer",
		Kind: "role",
		Mutation: RuneMutation{
			Title: ptr("Programmer"),
			Body:  ptr("You implement things."),
		},
		Actor: "alice",
	})
	if err != nil {
		t.Fatalf("PutRune() error = %v", err)
	}
	if r.Key != "role/programmer" || r.Kind != "role" {
		t.Fatalf("PutRune() key/kind = %q/%q, want role/programmer", r.Key, r.Kind)
	}
	if !r.Enabled {
		t.Fatal("PutRune() Enabled = false, want true by default")
	}
	if r.Protected {
		t.Fatal("PutRune() Protected = true, want false by default")
	}
	if r.Revision != 1 {
		t.Fatalf("PutRune() Revision = %d, want 1", r.Revision)
	}
	if r.Metadata != "{}" {
		t.Fatalf("PutRune() Metadata = %q, want {}", r.Metadata)
	}
	if r.Prime != RunePrimeOptional {
		t.Fatalf("PutRune() Prime = %q, want %q by default", r.Prime, RunePrimeOptional)
	}

	got, err := db.GetRune(ctx, "role/programmer")
	if err != nil {
		t.Fatalf("GetRune() error = %v", err)
	}
	if got.Title != "Programmer" {
		t.Fatalf("GetRune().Title = %q, want Programmer", got.Title)
	}
}

func TestPutRunePrimeRoundTripsAndValidates(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()

	r, err := db.PutRune(ctx, PutRune{
		Key: "role/qa", Kind: "role",
		Mutation: RuneMutation{Title: ptr("QA"), Body: ptr("body"), Prime: ptr(RunePrimeRequired)},
		Actor:    "alice",
	})
	if err != nil {
		t.Fatalf("PutRune() error = %v", err)
	}
	if r.Prime != RunePrimeRequired {
		t.Fatalf("PutRune() Prime = %q, want %q", r.Prime, RunePrimeRequired)
	}

	updated, err := db.PutRune(ctx, PutRune{
		Key: "role/qa", Kind: "role",
		Mutation: RuneMutation{Prime: ptr(RunePrimeNever)},
		Actor:    "alice",
	})
	if err != nil {
		t.Fatalf("PutRune() update error = %v", err)
	}
	if updated.Prime != RunePrimeNever {
		t.Fatalf("PutRune() update Prime = %q, want %q", updated.Prime, RunePrimeNever)
	}

	_, err = db.PutRune(ctx, PutRune{
		Key: "role/qa", Kind: "role",
		Mutation: RuneMutation{Prime: ptr("sometimes")},
		Actor:    "alice",
	})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("PutRune() with invalid prime error = %v, want *ValidationError", err)
	}
}

func TestPutRuneRejectsBadKeyGrammar(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()

	for _, key := range []string{"role", "Role/Programmer", "role/", "/programmer", "role/Programmer"} {
		_, err := db.PutRune(ctx, PutRune{Key: key, Kind: "role", Actor: "alice"})
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("PutRune(key=%q) error = %v, want *ValidationError", key, err)
		}
	}
}

func TestPutRuneRejectsKindMismatch(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()

	_, err := db.PutRune(ctx, PutRune{Key: "role/programmer", Kind: "policy", Actor: "alice"})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("PutRune() error = %v, want *ValidationError", err)
	}
}

func TestPutRuneCreateOnlyRejectsExisting(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()

	in := PutRune{Key: "role/programmer", Kind: "role", Mutation: RuneMutation{Title: ptr("Programmer")}, Actor: "alice"}
	if _, err := db.PutRune(ctx, in); err != nil {
		t.Fatalf("first PutRune() error = %v", err)
	}

	in.CreateOnly = true
	_, err := db.PutRune(ctx, in)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("PutRune(CreateOnly) error = %v, want ErrAlreadyExists", err)
	}
}

func TestPutRuneUpdatePreservesUnsuppliedFields(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()

	created, err := db.PutRune(ctx, PutRune{
		Key:      "role/programmer",
		Kind:     "role",
		Mutation: RuneMutation{Title: ptr("Programmer"), Body: ptr("v1")},
		Actor:    "alice",
	})
	if err != nil {
		t.Fatalf("PutRune() error = %v", err)
	}

	updated, err := db.PutRune(ctx, PutRune{
		Key:      "role/programmer",
		Kind:     "role",
		Mutation: RuneMutation{Body: ptr("v2")},
		Actor:    "bob",
	})
	if err != nil {
		t.Fatalf("PutRune(update) error = %v", err)
	}
	if updated.Title != "Programmer" {
		t.Fatalf("updated.Title = %q, want Programmer (unchanged)", updated.Title)
	}
	if updated.Body != "v2" {
		t.Fatalf("updated.Body = %q, want v2", updated.Body)
	}
	if updated.Revision != created.Revision+1 {
		t.Fatalf("updated.Revision = %d, want %d", updated.Revision, created.Revision+1)
	}
	if !updated.Enabled {
		t.Fatal("updated.Enabled = false, want true (preserved)")
	}
}

func TestPutRuneExpectedRevisionStaleFails(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()

	if _, err := db.PutRune(ctx, PutRune{Key: "role/programmer", Kind: "role", Mutation: RuneMutation{Title: ptr("Programmer")}, Actor: "alice"}); err != nil {
		t.Fatalf("PutRune() error = %v", err)
	}

	stale := int64(999)
	_, err := db.PutRune(ctx, PutRune{
		Key:              "role/programmer",
		Kind:             "role",
		Mutation:         RuneMutation{Body: ptr("v2")},
		ExpectedRevision: &stale,
		Actor:            "alice",
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("PutRune(stale ExpectedRevision) error = %v, want ErrInvalidArgument", err)
	}
	if strings.Contains(err.Error(), "put rune") {
		t.Fatalf("PutRune(stale ExpectedRevision) error = %v, must not mention retired %q wording", err, "put rune")
	}

	got, err := db.GetRune(ctx, "role/programmer")
	if err != nil {
		t.Fatalf("GetRune() error = %v", err)
	}
	if got.Body != "" {
		t.Fatalf("GetRune().Body = %q, want unchanged empty body after failed write", got.Body)
	}
}

func TestPutRuneProtectedRequiresForce(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()

	if _, err := db.PutRune(ctx, PutRune{
		Key:      "role/programmer",
		Kind:     "role",
		Mutation: RuneMutation{Title: ptr("Programmer"), Protected: ptr(true)},
		Actor:    "alice",
	}); err != nil {
		t.Fatalf("PutRune() error = %v", err)
	}

	_, err := db.PutRune(ctx, PutRune{Key: "role/programmer", Kind: "role", Mutation: RuneMutation{Body: ptr("v2")}, Actor: "alice"})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("PutRune(protected, no Force) error = %v, want ErrInvalidArgument", err)
	}
	if strings.Contains(err.Error(), "put rune") {
		t.Fatalf("PutRune(protected, no Force) error = %v, must not mention retired %q wording", err, "put rune")
	}

	updated, err := db.PutRune(ctx, PutRune{Key: "role/programmer", Kind: "role", Mutation: RuneMutation{Body: ptr("v2")}, Actor: "alice", Force: true})
	if err != nil {
		t.Fatalf("PutRune(protected, Force) error = %v", err)
	}
	if updated.Body != "v2" {
		t.Fatalf("updated.Body = %q, want v2", updated.Body)
	}
}

func TestGetRuneNotFound(t *testing.T) {
	db := newRuneTestDB(t)
	_, err := db.GetRune(context.Background(), "role/missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRune() error = %v, want ErrNotFound", err)
	}
}

func TestListRunesEnabledOnlyByDefault(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()

	mustPutRune(t, db, "role/programmer", "role", "Programmer")
	mustPutRune(t, db, "role/qa", "role", "QA")
	if _, err := db.SetRuneEnabled(ctx, "role/qa", false, "alice", false); err != nil {
		t.Fatalf("SetRuneEnabled() error = %v", err)
	}

	list, err := db.ListRunes(ctx, RuneQuery{})
	if err != nil {
		t.Fatalf("ListRunes() error = %v", err)
	}
	if len(list) != 1 || list[0].Key != "role/programmer" {
		t.Fatalf("ListRunes() = %+v, want only role/programmer", list)
	}

	all, err := db.ListRunes(ctx, RuneQuery{All: true})
	if err != nil {
		t.Fatalf("ListRunes(All) error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListRunes(All) = %+v, want 2 entries", all)
	}
}

func TestListRunesFiltersByKind(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()
	mustPutRune(t, db, "role/programmer", "role", "Programmer")
	mustPutRune(t, db, "policy/review", "policy", "Review policy")

	list, err := db.ListRunes(ctx, RuneQuery{Kind: "policy"})
	if err != nil {
		t.Fatalf("ListRunes() error = %v", err)
	}
	if len(list) != 1 || list[0].Key != "policy/review" {
		t.Fatalf("ListRunes(Kind=policy) = %+v", list)
	}
}

func TestSearchRunesMatchesBody(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()

	if _, err := db.PutRune(ctx, PutRune{
		Key:      "role/programmer",
		Kind:     "role",
		Mutation: RuneMutation{Title: ptr("Programmer"), Body: ptr("Writes durable named instructions.")},
		Actor:    "alice",
	}); err != nil {
		t.Fatalf("PutRune() error = %v", err)
	}

	found, err := db.SearchRunes(ctx, RuneQuery{Text: "durable"})
	if err != nil {
		t.Fatalf("SearchRunes() error = %v", err)
	}
	if len(found) != 1 || found[0].Key != "role/programmer" {
		t.Fatalf("SearchRunes(durable) = %+v", found)
	}

	notFound, err := db.SearchRunes(ctx, RuneQuery{Text: "nonexistent"})
	if err != nil {
		t.Fatalf("SearchRunes() error = %v", err)
	}
	if len(notFound) != 0 {
		t.Fatalf("SearchRunes(nonexistent) = %+v, want none", notFound)
	}
}

func TestSetRuneEnabledReversibleAndProtected(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()

	mustPutRune(t, db, "role/programmer", "role", "Programmer")

	disabled, err := db.SetRuneEnabled(ctx, "role/programmer", false, "alice", false)
	if err != nil {
		t.Fatalf("SetRuneEnabled(false) error = %v", err)
	}
	if disabled.Enabled {
		t.Fatal("SetRuneEnabled(false) left Enabled = true")
	}

	// Still directly readable while disabled.
	got, err := db.GetRune(ctx, "role/programmer")
	if err != nil {
		t.Fatalf("GetRune() on disabled rune error = %v", err)
	}
	if got.Enabled {
		t.Fatal("GetRune() Enabled = true, want false")
	}

	enabled, err := db.SetRuneEnabled(ctx, "role/programmer", true, "alice", false)
	if err != nil {
		t.Fatalf("SetRuneEnabled(true) error = %v", err)
	}
	if !enabled.Enabled {
		t.Fatal("SetRuneEnabled(true) left Enabled = false")
	}

	if _, err := db.PutRune(ctx, PutRune{Key: "role/programmer", Kind: "role", Mutation: RuneMutation{Protected: ptr(true)}, Actor: "alice"}); err != nil {
		t.Fatalf("protecting rune: %v", err)
	}
	if _, err := db.SetRuneEnabled(ctx, "role/programmer", false, "alice", false); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("SetRuneEnabled(protected, no Force) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := db.SetRuneEnabled(ctx, "role/programmer", false, "alice", true); err != nil {
		t.Fatalf("SetRuneEnabled(protected, Force) error = %v", err)
	}
}

func TestRemoveRuneTombstonesAndProtects(t *testing.T) {
	db := newRuneTestDB(t)
	ctx := context.Background()

	mustPutRune(t, db, "role/programmer", "role", "Programmer")
	if err := db.RemoveRune(ctx, "role/programmer", "alice", false); err != nil {
		t.Fatalf("RemoveRune() error = %v", err)
	}
	if _, err := db.GetRune(ctx, "role/programmer"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRune() after remove error = %v, want ErrNotFound", err)
	}

	var eventCount int
	if err := db.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE subject_key = ? AND action = 'rune.remove'", "role/programmer").Scan(&eventCount); err != nil {
		t.Fatalf("querying tombstone event: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("tombstone event count = %d, want 1", eventCount)
	}

	mustPutRune(t, db, "role/qa", "role", "QA")
	if _, err := db.PutRune(ctx, PutRune{Key: "role/qa", Kind: "role", Mutation: RuneMutation{Protected: ptr(true)}, Actor: "alice"}); err != nil {
		t.Fatalf("protecting rune: %v", err)
	}
	if err := db.RemoveRune(ctx, "role/qa", "alice", false); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("RemoveRune(protected, no Force) error = %v, want ErrInvalidArgument", err)
	}
	if err := db.RemoveRune(ctx, "role/qa", "alice", true); err != nil {
		t.Fatalf("RemoveRune(protected, Force) error = %v", err)
	}
}

func mustPutRune(t *testing.T, db *DB, key, kind, title string) *Rune {
	t.Helper()
	r, err := db.PutRune(context.Background(), PutRune{
		Key:      key,
		Kind:     kind,
		Mutation: RuneMutation{Title: ptr(title)},
		Actor:    "alice",
	})
	if err != nil {
		t.Fatalf("PutRune(%q) error = %v", key, err)
	}
	return r
}
