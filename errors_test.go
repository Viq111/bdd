package bdd

import (
	"errors"
	"strings"
	"testing"
)

func TestValidationErrorWrapsErrInvalidArgument(t *testing.T) {
	err := &ValidationError{Fields: []string{"reproduction", "acceptance"}}

	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("ValidationError does not satisfy errors.Is(err, ErrInvalidArgument)")
	}
	if !strings.Contains(err.Error(), "reproduction") || !strings.Contains(err.Error(), "acceptance") {
		t.Fatalf("Error() = %q, want it to mention every missing field", err.Error())
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	sentinels := []error{
		ErrNotFound,
		ErrAlreadyExists,
		ErrInvalidArgument,
		ErrInvalidTransition,
		ErrClaimed,
		ErrCycle,
		ErrBusy,
		ErrSchemaTooNew,
		ErrSchemaTooOld,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Fatalf("sentinel %d (%v) unexpectedly satisfies errors.Is against sentinel %d (%v)", i, a, j, b)
			}
		}
	}
}
