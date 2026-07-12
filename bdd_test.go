package bdd

import (
	"context"
	"testing"
)

// TestOpenNotImplemented locks the phase-0 contract: Open compiles and
// returns a non-nil error until the storage card lands, without panicking.
func TestOpenNotImplemented(t *testing.T) {
	db, err := Open(context.Background(), OpenOptions{})
	if err == nil {
		t.Fatal("Open returned nil error before storage is implemented")
	}
	if db != nil {
		t.Fatal("Open returned a non-nil *DB before storage is implemented")
	}
}
