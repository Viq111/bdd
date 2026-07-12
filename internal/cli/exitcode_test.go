package cli

import (
	"errors"
	"testing"

	"github.com/viq111/bdd"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitSuccess},
		{"not found", bdd.ErrNotFound, ExitNotFound},
		{"wrapped not found", errors.Join(errors.New("context"), bdd.ErrNotFound), ExitNotFound},
		{"invalid argument", bdd.ErrInvalidArgument, ExitUsage},
		{"validation error", &bdd.ValidationError{Fields: []string{"acceptance"}}, ExitUsage},
		{"invalid transition", bdd.ErrInvalidTransition, ExitConflict},
		{"already exists", bdd.ErrAlreadyExists, ExitConflict},
		{"claimed", bdd.ErrClaimed, ExitConflict},
		{"cycle", bdd.ErrCycle, ExitConflict},
		{"busy", bdd.ErrBusy, ExitOther},
		{"schema too old", bdd.ErrSchemaTooOld, ExitOther},
		{"schema too new", bdd.ErrSchemaTooNew, ExitOther},
		{"unmapped", errors.New("boom"), ExitOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Fatalf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
