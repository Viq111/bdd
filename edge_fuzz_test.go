package bdd

import (
	"context"
	"errors"
	"testing"
)

// FuzzCycleDetection drives a random sequence of AddParent/RemoveParent
// calls over a small, fixed set of cards and checks, after every step, that
// the edge graph on disk contains no cycle. wouldCreateCycle (edge.go) is
// the only thing standing between an add-edge call and a cyclic blocking
// graph; if fuzzing ever finds a cycle here, that function has a bug, not
// the test.
func FuzzCycleDetection(f *testing.F) {
	f.Add([]byte{0, 1, 0, 1, 2, 0, 2, 0, 0})
	f.Add([]byte{0, 1, 0, 1, 0, 1, 2, 0, 2})
	f.Add([]byte{})

	const numCards = 5

	f.Fuzz(func(t *testing.T, ops []byte) {
		if len(ops) > 300 {
			t.Skip("too long to be a useful fuzz case")
		}

		db := newTestDB(t)
		ctx := context.Background()

		ids := make([]string, numCards)
		for i := range ids {
			card, err := db.CreateCard(ctx, CreateCard{
				Title: "card", Type: CardTypeChore, CreatedBy: "fuzz",
			})
			if err != nil {
				t.Fatalf("CreateCard() error = %v", err)
			}
			ids[i] = card.ID
		}

		// Each byte encodes one operation: low bit selects add vs remove,
		// the rest select parent and child indices into ids.
		for _, b := range ops {
			add := b&1 == 0
			parent := ids[(b>>1)%numCards]
			child := ids[(b>>4)%numCards]

			var err error
			if add {
				err = db.AddParent(ctx, child, parent, "fuzz")
			} else {
				err = db.RemoveParent(ctx, child, parent, "fuzz")
			}
			// Self-edges, not-found, and cycle errors are all expected,
			// well-typed outcomes, not fuzz failures; anything else (an
			// unwrapped/unexpected error, or a panic) is.
			if err != nil && !isExpectedEdgeError(err) {
				t.Fatalf("unexpected error mutating edge: %v", err)
			}
		}

		assertNoCycle(t, db, ids)
	})
}

func isExpectedEdgeError(err error) bool {
	return errors.Is(err, ErrInvalidArgument) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrCycle)
}

// assertNoCycle walks the blocking-edge graph restricted to ids by
// repeatedly calling (*DB).Children, and fails the test if it ever
// revisits a node already on the current DFS path.
func assertNoCycle(t *testing.T, db *DB, ids []string) {
	t.Helper()
	ctx := context.Background()

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(ids))

	var visit func(id string) []string
	visit = func(id string) []string {
		color[id] = gray
		children, err := db.Children(ctx, id)
		if err != nil {
			t.Fatalf("Children(%s) error = %v", id, err)
		}
		for _, c := range children {
			if !idSet[c.ID] {
				continue
			}
			switch color[c.ID] {
			case gray:
				return []string{id, c.ID}
			case white:
				if cyc := visit(c.ID); cyc != nil {
					return append([]string{id}, cyc...)
				}
			}
		}
		color[id] = black
		return nil
	}

	for _, id := range ids {
		if color[id] == white {
			if cyc := visit(id); cyc != nil {
				t.Fatalf("cycle detection failed: found cycle %v", cyc)
			}
		}
	}
}
