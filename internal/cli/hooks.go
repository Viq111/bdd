package cli

import (
	"context"
	"os"

	"github.com/viq111/bdd"
)

// HooksResult is the JSON/human hooks section of `bdd status`.
type HooksResult struct {
	// Present reports whether a hooks.yaml file exists in the workspace.
	Present bool `json:"present"`

	// Enabled reports the hooks.enabled config key, independent of any
	// per-invocation --no-hooks / BDD_NO_HOOKS override.
	Enabled bool `json:"enabled"`

	// Active reports whether hooks would actually fire for this
	// invocation: Present, valid, Enabled, and not force-disabled.
	Active bool `json:"active"`

	// HookCount is the number of hooks parsed from hooks.yaml. Only
	// meaningful when Present and Error is empty.
	HookCount int `json:"hook_count,omitempty"`

	// Error is the parse/validation error message when Present is true but
	// hooks.yaml failed to load.
	Error string `json:"error,omitempty"`
}

// HooksDisabled reports whether hooks are force-disabled for this
// invocation via --no-hooks or BDD_NO_HOOKS=1, independent of the
// hooks.enabled config gate.
func HooksDisabled(g GlobalFlags) bool {
	if g.NoHooks {
		return true
	}
	return os.Getenv("BDD_NO_HOOKS") == "1"
}

// hooksStatus computes the HooksResult for db under global flags g, for
// `bdd status` to report.
func hooksStatus(ctx context.Context, db *bdd.DB, g GlobalFlags) HooksResult {
	path := db.HooksPath()
	if _, err := os.Stat(path); err != nil {
		return HooksResult{}
	}

	r := HooksResult{Present: true}

	hf, err := bdd.LoadHooksFile(path)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.HookCount = len(hf.Hooks)

	enabled, err := db.HooksEnabled(ctx)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Enabled = enabled
	r.Active = enabled && !HooksDisabled(g)
	return r
}
