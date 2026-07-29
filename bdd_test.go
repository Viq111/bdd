package bdd

import (
	"context"
	"errors"
	"testing"
)

// TestOpenWithoutWorkspaceReturnsNotFound checks that Open reports
// ErrNotFound when discoverDatabase can't locate a workspace, without
// panicking.
func TestOpenWithoutWorkspaceReturnsNotFound(t *testing.T) {
	t.Chdir(t.TempDir())

	db, err := Open(context.Background(), OpenOptions{})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open error = %v, want ErrNotFound", err)
	}
	if db != nil {
		t.Fatal("Open returned a non-nil *DB alongside an error")
	}
}
