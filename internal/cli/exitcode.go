package cli

import (
	"errors"

	"github.com/viq111/bdd"
)

// Exit codes shared by every bdd command. These are the only exit codes a
// command may return: success, or one of the four failure buckets below.
const (
	// ExitSuccess indicates the command completed as requested.
	ExitSuccess = 0
	// ExitOther indicates a failure that does not fit the more specific
	// buckets below (I/O errors, an unexpected schema state, and the like).
	ExitOther = 1
	// ExitUsage indicates a usage or validation error: bad flags, missing
	// required fields, malformed arguments.
	ExitUsage = 2
	// ExitNotFound indicates the requested card, note, memory, rune, or
	// database does not exist.
	ExitNotFound = 3
	// ExitConflict indicates a conflicting or invalid lifecycle operation:
	// an illegal state transition, a claim on an already-claimed card, a
	// cycle, or a create-only operation that already exists.
	ExitConflict = 4
)

// ExitCode maps an error returned by the bdd library to the exit code a
// command should return, by checking it against every sentinel error in
// one place so every command stays consistent. A nil error maps to
// ExitSuccess.
func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}

	switch {
	case errors.Is(err, bdd.ErrNotFound):
		return ExitNotFound
	case errors.Is(err, bdd.ErrInvalidTransition),
		errors.Is(err, bdd.ErrClaimed),
		errors.Is(err, bdd.ErrCycle),
		errors.Is(err, bdd.ErrAlreadyExists):
		return ExitConflict
	case errors.Is(err, bdd.ErrInvalidArgument):
		return ExitUsage
	default:
		// Includes ErrBusy, ErrSchemaTooOld, and ErrSchemaTooNew: none of
		// these are the caller's fault in a way "usage" captures, so they
		// fall through to the generic failure bucket.
		return ExitOther
	}
}
