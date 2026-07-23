package bdd

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by DB methods. Callers should compare with
// errors.Is, since concrete errors returned by the library may wrap these.
var (
	// ErrNotFound indicates the requested card, note, memory, or rune does
	// not exist.
	ErrNotFound = errors.New("bdd: not found")

	// ErrAlreadyExists indicates a create-only operation targeted a key or
	// ID that already exists.
	ErrAlreadyExists = errors.New("bdd: already exists")

	// ErrInvalidArgument indicates a caller-supplied argument failed
	// validation (missing required fields, malformed keys, etc).
	ErrInvalidArgument = errors.New("bdd: invalid argument")

	// ErrInvalidTransition indicates a lifecycle mutation would move a card
	// through an illegal state transition.
	ErrInvalidTransition = errors.New("bdd: invalid transition")

	// ErrClaimed indicates a claim attempt on a card already claimed by a
	// different actor.
	ErrClaimed = errors.New("bdd: already claimed")

	// ErrCycle indicates a parent/child edge mutation would introduce a
	// cycle in the blocking graph.
	ErrCycle = errors.New("bdd: cycle detected")

	// ErrBusy indicates the underlying database could not be locked within
	// the configured retry budget.
	ErrBusy = errors.New("bdd: busy")

	// ErrSchemaTooNew indicates the database schema version is newer than
	// this build of bdd understands.
	ErrSchemaTooNew = errors.New("bdd: schema too new")

	// ErrSchemaTooOld indicates the database schema version predates this
	// build of bdd and requires Upgrade.
	ErrSchemaTooOld = errors.New("bdd: schema too old")
)

// ValidationError reports every field that failed validation in a single
// call, rather than surfacing only the first violation. It wraps
// ErrInvalidArgument, so errors.Is(err, ErrInvalidArgument) succeeds.
type ValidationError struct {
	// Fields lists the names of every field that failed validation, in a
	// stable, deterministic order.
	Fields []string

	// Detail, if non-empty, replaces the default "missing required
	// field(s)" wording. Use it when a field is present but invalid (e.g.
	// malformed grammar) rather than absent, so the message doesn't
	// mislead callers into thinking they omitted it.
	Detail string
}

func (e *ValidationError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("bdd: invalid argument: %s", e.Detail)
	}
	return fmt.Sprintf("bdd: invalid argument: missing required field(s): %s", strings.Join(e.Fields, ", "))
}

func (e *ValidationError) Unwrap() error {
	return ErrInvalidArgument
}
