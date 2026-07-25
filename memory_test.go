package bdd

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCreateMemoryCreatesWithExplicitKey(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	m, err := db.CreateMemory(ctx, Remember{Key: "greeting", Body: "hello there", Actor: "alice"})
	if err != nil {
		t.Fatalf("CreateMemory() error = %v", err)
	}
	if m.Key != "greeting" {
		t.Fatalf("Key = %q, want %q", m.Key, "greeting")
	}
	if m.Body != "hello there" {
		t.Fatalf("Body = %q, want %q", m.Body, "hello there")
	}
	if m.CreatedBy != "alice" || m.UpdatedBy != "alice" {
		t.Fatalf("CreatedBy/UpdatedBy = %q/%q, want alice/alice", m.CreatedBy, m.UpdatedBy)
	}
	if m.Revision != 1 {
		t.Fatalf("Revision = %d, want 1", m.Revision)
	}
	if m.CreatedAt.IsZero() || m.UpdatedAt.IsZero() {
		t.Fatal("CreatedAt/UpdatedAt must be set")
	}
	if m.Prime != MemoryPrimeOptional {
		t.Fatalf("Prime = %q, want %q by default", m.Prime, MemoryPrimeOptional)
	}
}

func TestCreateMemoryFailsOnExistingKey(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.CreateMemory(ctx, Remember{Key: "greeting", Body: "hello", Actor: "alice"}); err != nil {
		t.Fatalf("first CreateMemory() error = %v", err)
	}

	if _, err := db.CreateMemory(ctx, Remember{Key: "greeting", Body: "hi there", Actor: "bob"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("CreateMemory() on existing key error = %v, want ErrAlreadyExists", err)
	}

	// The original memory must be untouched.
	m, err := db.Recall(ctx, "greeting")
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if m.Body != "hello" || m.UpdatedBy != "alice" || m.Revision != 1 {
		t.Fatalf("Recall() after failed create = %+v, want the original record unchanged", m)
	}
}

func TestUpdateMemoryUpdatesExistingKeyAndIncrementsRevision(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.CreateMemory(ctx, Remember{Key: "greeting", Body: "hello", Actor: "alice"}); err != nil {
		t.Fatalf("CreateMemory() error = %v", err)
	}

	m, err := db.UpdateMemory(ctx, Remember{Key: "greeting", Body: "hi there", Actor: "bob"})
	if err != nil {
		t.Fatalf("UpdateMemory() error = %v", err)
	}
	if m.Body != "hi there" {
		t.Fatalf("Body = %q, want %q", m.Body, "hi there")
	}
	if m.CreatedBy != "alice" {
		t.Fatalf("CreatedBy = %q, want %q (must not change on update)", m.CreatedBy, "alice")
	}
	if m.UpdatedBy != "bob" {
		t.Fatalf("UpdatedBy = %q, want %q", m.UpdatedBy, "bob")
	}
	if m.Revision != 2 {
		t.Fatalf("Revision = %d, want 2", m.Revision)
	}
}

func TestUpdateMemoryFailsOnMissingKey(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.UpdateMemory(ctx, Remember{Key: "missing", Body: "x", Actor: "alice"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateMemory() on missing key error = %v, want ErrNotFound", err)
	}
}

func TestMemoryPrimeRoundTripsPreservesAndValidates(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	required := MemoryPrimeRequired
	m, err := db.CreateMemory(ctx, Remember{Key: "role", Body: "mandatory instruction", Prime: &required, Actor: "alice"})
	if err != nil {
		t.Fatalf("CreateMemory() error = %v", err)
	}
	if m.Prime != MemoryPrimeRequired {
		t.Fatalf("Prime = %q, want %q", m.Prime, MemoryPrimeRequired)
	}

	updated, err := db.UpdateMemory(ctx, Remember{Key: "role", Body: "mandatory instruction v2", Actor: "alice"})
	if err != nil {
		t.Fatalf("UpdateMemory() error = %v", err)
	}
	if updated.Prime != MemoryPrimeRequired {
		t.Fatalf("Prime = %q after unrelated update, want preserved %q", updated.Prime, MemoryPrimeRequired)
	}

	bogus := "sometimes"
	if _, err := db.UpdateMemory(ctx, Remember{Key: "role", Body: "x", Prime: &bogus, Actor: "alice"}); err == nil {
		t.Fatal("UpdateMemory() with invalid prime value: want error")
	} else {
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("UpdateMemory() with invalid prime error = %v, want *ValidationError", err)
		}
		if strings.Contains(err.Error(), "missing required field") {
			t.Fatalf("UpdateMemory() with invalid (but present) prime value error = %q, must not claim the field is missing", err.Error())
		}
	}
}

func TestCreateMemoryRequiresKey(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.CreateMemory(ctx, Remember{Body: "no key here", Actor: "alice"}); err == nil {
		t.Fatal("CreateMemory() with empty key: want error")
	} else {
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("CreateMemory() with empty key error = %v, want *ValidationError", err)
		}
	}
}

func TestUpdateMemoryRequiresKey(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.UpdateMemory(ctx, Remember{Body: "no key here", Actor: "alice"}); err == nil {
		t.Fatal("UpdateMemory() with empty key: want error")
	} else {
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("UpdateMemory() with empty key error = %v, want *ValidationError", err)
		}
	}
}

func TestRecallReturnsErrNotFound(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.Recall(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Recall() error = %v, want ErrNotFound", err)
	}
}

func TestRecallReturnsFullRecord(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	created, err := db.CreateMemory(ctx, Remember{Key: "k1", Body: "body one", Actor: "alice"})
	if err != nil {
		t.Fatalf("CreateMemory() error = %v", err)
	}

	got, err := db.Recall(ctx, "k1")
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if got.Key != created.Key || got.Body != created.Body || got.Revision != created.Revision {
		t.Fatalf("Recall() = %+v, want %+v", got, created)
	}
}

func TestMemoriesListsAllWhenQueryEmpty(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.CreateMemory(ctx, Remember{Key: "alpha", Body: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateMemory(ctx, Remember{Key: "beta", Body: "second"}); err != nil {
		t.Fatal(err)
	}

	got, err := db.Memories(ctx, MemoryQuery{})
	if err != nil {
		t.Fatalf("Memories() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Memories() returned %d records, want 2", len(got))
	}
}

func TestMemoriesSearchesKeyAndBodyCaseInsensitively(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.CreateMemory(ctx, Remember{Key: "sqlite-notes", Body: "WAL mode is enabled"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateMemory(ctx, Remember{Key: "unrelated", Body: "nothing to see here"}); err != nil {
		t.Fatal(err)
	}

	byKey, err := db.Memories(ctx, MemoryQuery{Query: "SQLITE"})
	if err != nil {
		t.Fatalf("Memories() error = %v", err)
	}
	if len(byKey) != 1 || byKey[0].Key != "sqlite-notes" {
		t.Fatalf("Memories(query=SQLITE) = %+v, want just sqlite-notes", byKey)
	}

	byBody, err := db.Memories(ctx, MemoryQuery{Query: "wal MODE"})
	if err != nil {
		t.Fatalf("Memories() error = %v", err)
	}
	if len(byBody) != 1 || byBody[0].Key != "sqlite-notes" {
		t.Fatalf("Memories(query=wal MODE) = %+v, want just sqlite-notes", byBody)
	}

	none, err := db.Memories(ctx, MemoryQuery{Query: "nonexistent"})
	if err != nil {
		t.Fatalf("Memories() error = %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("Memories(query=nonexistent) = %+v, want empty", none)
	}
}

func TestMemoriesQueryLikeMetacharactersAreLiteral(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.CreateMemory(ctx, Remember{Key: "percent", Body: "100% done"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateMemory(ctx, Remember{Key: "other", Body: "100x done"}); err != nil {
		t.Fatal(err)
	}

	got, err := db.Memories(ctx, MemoryQuery{Query: "100%"})
	if err != nil {
		t.Fatalf("Memories() error = %v", err)
	}
	if len(got) != 1 || got[0].Key != "percent" {
		t.Fatalf("Memories(query=100%%) = %+v, want just percent", got)
	}
}

func TestForgetDeletesAndIsNotFoundAfter(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.CreateMemory(ctx, Remember{Key: "k1", Body: "body", Actor: "alice"}); err != nil {
		t.Fatal(err)
	}

	if err := db.Forget(ctx, "k1", "alice"); err != nil {
		t.Fatalf("Forget() error = %v", err)
	}

	_, err := db.Recall(ctx, "k1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Recall() after Forget() error = %v, want ErrNotFound", err)
	}
}

func TestForgetMissingKeyReturnsErrNotFound(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	err := db.Forget(ctx, "missing", "alice")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Forget() error = %v, want ErrNotFound", err)
	}
}

func TestForgetWritesAuditEvent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.CreateMemory(ctx, Remember{Key: "k1", Body: "body", Actor: "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Forget(ctx, "k1", "alice"); err != nil {
		t.Fatalf("Forget() error = %v", err)
	}

	var count int
	row := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE subject_kind = 'memory' AND subject_key = ? AND action = 'memory.delete'`, "k1")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("querying events: %v", err)
	}
	if count != 1 {
		t.Fatalf("memory.delete events for k1 = %d, want 1", count)
	}
}

func TestMemoryWritesAuditEvents(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.CreateMemory(ctx, Remember{Key: "k1", Body: "v1", Actor: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateMemory(ctx, Remember{Key: "k1", Body: "v2", Actor: "alice"}); err != nil {
		t.Fatal(err)
	}

	var createCount, updateCount int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE subject_kind = 'memory' AND subject_key = 'k1' AND action = 'memory.create'`).Scan(&createCount); err != nil {
		t.Fatal(err)
	}
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE subject_kind = 'memory' AND subject_key = 'k1' AND action = 'memory.update'`).Scan(&updateCount); err != nil {
		t.Fatal(err)
	}
	if createCount != 1 {
		t.Fatalf("memory.create events = %d, want 1", createCount)
	}
	if updateCount != 1 {
		t.Fatalf("memory.update events = %d, want 1", updateCount)
	}
}

func TestMemoryMethodsOnReadOnlyDB(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	init, err := Init(ctx, InitOptions{Workspace: dir, Prefix: "bdd"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if _, err := init.CreateMemory(ctx, Remember{Key: "k1", Body: "body", Actor: "alice"}); err != nil {
		t.Fatalf("seeding CreateMemory() error = %v", err)
	}
	init.Close()

	dbPath := dir + "/.bdd/bdd.sqlite"
	db, err := Open(ctx, OpenOptions{Path: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	if _, err := db.CreateMemory(ctx, Remember{Key: "k2", Body: "body"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("CreateMemory() on read-only db error = %v, want ErrInvalidArgument", err)
	}
	if _, err := db.UpdateMemory(ctx, Remember{Key: "k1", Body: "body"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("UpdateMemory() on read-only db error = %v, want ErrInvalidArgument", err)
	}
	if err := db.Forget(ctx, "k1", "alice"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Forget() on read-only db error = %v, want ErrInvalidArgument", err)
	}

	// Reads still work.
	if _, err := db.Recall(ctx, "k1"); err != nil {
		t.Fatalf("Recall() on read-only db error = %v", err)
	}
	if _, err := db.Memories(ctx, MemoryQuery{}); err != nil {
		t.Fatalf("Memories() on read-only db error = %v", err)
	}
}
