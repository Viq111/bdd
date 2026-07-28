// Black-box coverage for .bdd/hooks.yaml: a real hooks.yaml on disk, the
// real compiled bdd binary, and a real child process. Unit tests in
// internal/cli (hookdispatch_test.go) and the root package (hooks_test.go)
// cover parsing, matching, and in-process dispatch; this file covers what
// only a subprocess boundary can catch — that the hook binary actually
// spawns, that stdin JSON matches the documented contract, that it cannot
// recurse through a second real bdd invocation, and that concurrent writers
// don't cause a double fire.
package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- hooks.yaml plumbing ---

// hooksYAMLPath returns the hooks.yaml path alongside db, mirroring
// (*bdd.DB).HooksPath: same .bdd directory as bdd.sqlite.
func hooksYAMLPath(db string) string {
	return filepath.Join(filepath.Dir(db), "hooks.yaml")
}

func writeHooksYAML(t *testing.T, db, content string) {
	t.Helper()
	if err := os.WriteFile(hooksYAMLPath(db), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// enableHooks writes hooks.yaml then flips hooks.enabled on for db's
// workspace.
func enableHooks(t *testing.T, db, content string) {
	t.Helper()
	writeHooksYAML(t, db, content)
	if r := run(t, db, "config", "set", "hooks.enabled", "true"); r.code != 0 {
		t.Fatalf("config set hooks.enabled: code=%d stderr=%s", r.code, r.stderr)
	}
}

// mustCommandJSON renders cmd as a YAML/JSON flow-sequence literal safe to
// splice directly into a hooks.yaml "command:" value.
func mustCommandJSON(t *testing.T, cmd []string) string {
	t.Helper()
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// recordingCommand is a hooks.yaml "command" list that appends the hook's
// stdin JSON as one line to outPath via /bin/sh, with no shell
// interpolation of untrusted input (outPath is test-controlled).
func recordingCommand(outPath string) []string {
	return []string{"/bin/sh", "-c", `cat >> "$1"; echo >> "$1"`, "--", outPath}
}

func readHookLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var lines []map[string]any
	for _, chunk := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(chunk), &m); err != nil {
			t.Fatalf("unmarshal hook payload %q: %v", chunk, err)
		}
		lines = append(lines, m)
	}
	return lines
}

// createCard creates a task card in db's workspace and returns its ID.
func createCard(t *testing.T, db string, extraArgs ...string) string {
	t.Helper()
	args := append([]string{"create", "hook target", "--type", "task", "--acceptance", "x", "--silent"}, extraArgs...)
	r := run(t, db, args...)
	if r.code != 0 {
		t.Fatalf("create: code=%d stderr=%s", r.code, r.stderr)
	}
	return strings.TrimSpace(r.stdout)
}

// --- Every mutating command fires its hook. ---

func TestHookFiresOnEachMutatingCommand(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, db string) string
		action  func(t *testing.T, db, id string) result
		event   string
	}{
		{
			name:    "create",
			prepare: func(t *testing.T, db string) string { return "" },
			action: func(t *testing.T, db, id string) result {
				return run(t, db, "create", "new card", "--type", "task", "--acceptance", "x", "--silent")
			},
			event: "status-change",
		},
		{
			name:    "update --status",
			prepare: func(t *testing.T, db string) string { return createCard(t, db) },
			action: func(t *testing.T, db, id string) result {
				return run(t, db, "update", id, "--status", "in_progress")
			},
			event: "status-change",
		},
		{
			name:    "update --claim",
			prepare: func(t *testing.T, db string) string { return createCard(t, db) },
			action: func(t *testing.T, db, id string) result {
				return run(t, db, "update", id, "--claim")
			},
			event: "status-change",
		},
		{
			name:    "close",
			prepare: func(t *testing.T, db string) string { return createCard(t, db) },
			action: func(t *testing.T, db, id string) result {
				return run(t, db, "close", id)
			},
			event: "status-change",
		},
		{
			name: "reopen",
			prepare: func(t *testing.T, db string) string {
				id := createCard(t, db)
				if r := run(t, db, "close", id); r.code != 0 {
					t.Fatalf("close (setup): code=%d stderr=%s", r.code, r.stderr)
				}
				return id
			},
			action: func(t *testing.T, db, id string) result {
				return run(t, db, "reopen", id)
			},
			event: "status-change",
		},
		{
			name:    "defer",
			prepare: func(t *testing.T, db string) string { return createCard(t, db) },
			action: func(t *testing.T, db, id string) result {
				return run(t, db, "defer", id)
			},
			event: "status-change",
		},
		{
			name:    "label add",
			prepare: func(t *testing.T, db string) string { return createCard(t, db) },
			action: func(t *testing.T, db, id string) result {
				return run(t, db, "label", "add", id, "urgent")
			},
			event: "label-change",
		},
		{
			name: "label remove",
			prepare: func(t *testing.T, db string) string {
				return createCard(t, db, "--label", "urgent")
			},
			action: func(t *testing.T, db, id string) result {
				return run(t, db, "label", "remove", id, "urgent")
			},
			event: "label-change",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newWorkspace(t)
			out := filepath.Join(t.TempDir(), "hook-out.log")
			enableHooks(t, db, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
  - event: label-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

			id := tc.prepare(t, db)
			// prepare() may itself have fired hooks (e.g. the create+close
			// in the reopen case); isolate the action under test.
			os.Remove(out)

			r := tc.action(t, db, id)
			if r.code != 0 {
				t.Fatalf("action: code=%d stderr=%s", r.code, r.stderr)
			}

			lines := readHookLines(t, out)
			if len(lines) != 1 {
				t.Fatalf("hook fired %d times, want 1 (lines=%v)", len(lines), lines)
			}
			if lines[0]["event"] != tc.event {
				t.Fatalf("event = %v, want %s", lines[0]["event"], tc.event)
			}
		})
	}
}

// --- Payload correctness against the documented contract. ---

func TestHookPayloadContract(t *testing.T) {
	db := newWorkspace(t)
	out := filepath.Join(t.TempDir(), "hook-out.log")
	enableHooks(t, db, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

	r := run(t, db, "--actor", "alice", "create", "Contract card", "--type", "task", "--acceptance", "x", "--silent")
	if r.code != 0 {
		t.Fatalf("create: code=%d stderr=%s", r.code, r.stderr)
	}
	id := strings.TrimSpace(r.stdout)

	lines := readHookLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("hook fired %d times, want 1", len(lines))
	}
	p := lines[0]

	if v, ok := p["version"].(float64); !ok || v != 1 {
		t.Fatalf("version = %v, want 1", p["version"])
	}
	if p["event"] != "status-change" {
		t.Fatalf("event = %v, want status-change", p["event"])
	}
	wantWorkspace := dbWorkspace(db)
	if p["workspace"] != wantWorkspace {
		t.Fatalf("workspace = %v, want %s", p["workspace"], wantWorkspace)
	}
	if p["database"] != db {
		t.Fatalf("database = %v, want %s", p["database"], db)
	}
	if p["actor"] != "alice" {
		t.Fatalf("actor = %v, want alice", p["actor"])
	}
	ts, _ := p["timestamp"].(string)
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Fatalf("timestamp = %q, not RFC3339: %v", ts, err)
	}

	card, ok := p["card"].(map[string]any)
	if !ok {
		t.Fatalf("card missing or wrong type: %v", p["card"])
	}
	if card["id"] != id {
		t.Fatalf("card.id = %v, want %s", card["id"], id)
	}
	if card["status"] != "open" {
		t.Fatalf("card.status = %v, want open", card["status"])
	}
	if labels, ok := card["labels"].([]any); !ok || len(labels) != 0 {
		t.Fatalf("card.labels = %v, want []", card["labels"])
	}
	if _, ok := card["revision"]; !ok {
		t.Fatalf("card.revision missing: %v", card)
	}

	sc, ok := p["status_change"].(map[string]any)
	if !ok {
		t.Fatalf("status_change missing or wrong type: %v", p["status_change"])
	}
	if sc["from"] != "" {
		t.Fatalf("status_change.from = %v, want empty", sc["from"])
	}
	if sc["to"] != "open" {
		t.Fatalf("status_change.to = %v, want open", sc["to"])
	}
	if p["label_change"] != nil {
		t.Fatalf("label_change = %v, want nil", p["label_change"])
	}
}

// --- create with --label at creation time: one status-change, no
// label-change. ---

func TestHookCreateWithLabelsNoLabelChange(t *testing.T) {
	db := newWorkspace(t)
	out := filepath.Join(t.TempDir(), "hook-out.log")
	enableHooks(t, db, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
  - event: label-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

	r := run(t, db, "create", "Labeled at birth", "--type", "task", "--acceptance", "x", "--label", "foo", "--silent")
	if r.code != 0 {
		t.Fatalf("create: code=%d stderr=%s", r.code, r.stderr)
	}

	lines := readHookLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("hook fired %d times, want exactly 1 (lines=%v)", len(lines), lines)
	}
	if lines[0]["event"] != "status-change" {
		t.Fatalf("event = %v, want status-change", lines[0]["event"])
	}
	if lines[0]["label_change"] != nil {
		t.Fatalf("label_change = %v, want nil even though --label was passed at create", lines[0]["label_change"])
	}
	card := lines[0]["card"].(map[string]any)
	labels, _ := card["labels"].([]any)
	if len(labels) != 1 || labels[0] != "foo" {
		t.Fatalf("card.labels = %v, want [foo]", card["labels"])
	}
}

// --- A single update combining --status and --add-label fires both
// events, status-change before label-change. ---

func TestHookCombinedStatusAndLabelOrder(t *testing.T) {
	db := newWorkspace(t)
	out := filepath.Join(t.TempDir(), "hook-out.log")
	id := createCard(t, db)

	enableHooks(t, db, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
  - event: label-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

	r := run(t, db, "update", id, "--status", "in_progress", "--add-label", "urgent")
	if r.code != 0 {
		t.Fatalf("update: code=%d stderr=%s", r.code, r.stderr)
	}

	lines := readHookLines(t, out)
	if len(lines) != 2 {
		t.Fatalf("hook fired %d times, want 2 (lines=%v)", len(lines), lines)
	}
	if lines[0]["event"] != "status-change" {
		t.Fatalf("first event = %v, want status-change", lines[0]["event"])
	}
	if lines[1]["event"] != "label-change" {
		t.Fatalf("second event = %v, want label-change", lines[1]["event"])
	}
}

// --- Filters: to_status, from_status, added, removed, and no-filter. ---

func TestHookFilters(t *testing.T) {
	t.Run("to_status", func(t *testing.T) {
		db := newWorkspace(t)
		out := filepath.Join(t.TempDir(), "hook-out.log")
		id := createCard(t, db)
		enableHooks(t, db, `
version: 1
hooks:
  - event: status-change
    to_status: [awaiting_review]
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

		if r := run(t, db, "update", id, "--status", "in_progress"); r.code != 0 {
			t.Fatalf("update to in_progress: code=%d stderr=%s", r.code, r.stderr)
		}
		if lines := readHookLines(t, out); len(lines) != 0 {
			t.Fatalf("to_status filter: transition to in_progress fired %d times, want 0 (non-matching to_status)", len(lines))
		}

		if r := run(t, db, "update", id, "--status", "awaiting_review"); r.code != 0 {
			t.Fatalf("update to awaiting_review: code=%d stderr=%s", r.code, r.stderr)
		}
		lines := readHookLines(t, out)
		if len(lines) != 1 {
			t.Fatalf("to_status filter: transition to awaiting_review fired %d times, want 1 (matching to_status)", len(lines))
		}
	})

	t.Run("from_status", func(t *testing.T) {
		db := newWorkspace(t)
		out := filepath.Join(t.TempDir(), "hook-out.log")
		id := createCard(t, db)
		enableHooks(t, db, `
version: 1
hooks:
  - event: status-change
    from_status: [in_progress]
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

		// open -> in_progress: from_status is "open", doesn't match filter.
		if r := run(t, db, "update", id, "--status", "in_progress"); r.code != 0 {
			t.Fatalf("update to in_progress: code=%d stderr=%s", r.code, r.stderr)
		}
		if lines := readHookLines(t, out); len(lines) != 0 {
			t.Fatalf("from_status filter: open->in_progress fired %d times, want 0 (non-matching from_status)", len(lines))
		}

		// in_progress -> closed: from_status is "in_progress", matches.
		if r := run(t, db, "close", id); r.code != 0 {
			t.Fatalf("close: code=%d stderr=%s", r.code, r.stderr)
		}
		lines := readHookLines(t, out)
		if len(lines) != 1 {
			t.Fatalf("from_status filter: in_progress->closed fired %d times, want 1 (matching from_status)", len(lines))
		}
	})

	t.Run("added", func(t *testing.T) {
		db := newWorkspace(t)
		out := filepath.Join(t.TempDir(), "hook-out.log")
		id := createCard(t, db)
		enableHooks(t, db, `
version: 1
hooks:
  - event: label-change
    added: [foo]
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

		if r := run(t, db, "label", "add", id, "bar"); r.code != 0 {
			t.Fatalf("label add bar: code=%d stderr=%s", r.code, r.stderr)
		}
		if lines := readHookLines(t, out); len(lines) != 0 {
			t.Fatalf("added filter: adding bar fired %d times, want 0 (only foo matches)", len(lines))
		}

		if r := run(t, db, "label", "add", id, "foo"); r.code != 0 {
			t.Fatalf("label add foo: code=%d stderr=%s", r.code, r.stderr)
		}
		lines := readHookLines(t, out)
		if len(lines) != 1 {
			t.Fatalf("added filter: adding foo fired %d times, want 1", len(lines))
		}
	})

	t.Run("removed", func(t *testing.T) {
		db := newWorkspace(t)
		out := filepath.Join(t.TempDir(), "hook-out.log")
		id := createCard(t, db, "--label", "foo", "--label", "bar")
		enableHooks(t, db, `
version: 1
hooks:
  - event: label-change
    removed: [foo]
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

		if r := run(t, db, "label", "remove", id, "bar"); r.code != 0 {
			t.Fatalf("label remove bar: code=%d stderr=%s", r.code, r.stderr)
		}
		if lines := readHookLines(t, out); len(lines) != 0 {
			t.Fatalf("removed filter: removing bar fired %d times, want 0 (only foo matches)", len(lines))
		}

		if r := run(t, db, "label", "remove", id, "foo"); r.code != 0 {
			t.Fatalf("label remove foo: code=%d stderr=%s", r.code, r.stderr)
		}
		lines := readHookLines(t, out)
		if len(lines) != 1 {
			t.Fatalf("removed filter: removing foo fired %d times, want 1", len(lines))
		}
	})

	t.Run("no filters matches everything", func(t *testing.T) {
		db := newWorkspace(t)
		out := filepath.Join(t.TempDir(), "hook-out.log")
		id := createCard(t, db)
		enableHooks(t, db, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

		for _, args := range [][]string{
			{"update", id, "--status", "in_progress"},
			{"close", id},
			{"reopen", id},
			{"defer", id},
		} {
			if r := run(t, db, args...); r.code != 0 {
				t.Fatalf("run(%v): code=%d stderr=%s", args, r.code, r.stderr)
			}
		}
		lines := readHookLines(t, out)
		if len(lines) != 4 {
			t.Fatalf("no-filter hook fired %d times, want 4 (one per transition)", len(lines))
		}
	})
}

// --- Gating: hooks.enabled, --no-hooks, BDD_NO_HOOKS. ---

func TestHookGatingRequiresEnabled(t *testing.T) {
	db := newWorkspace(t)
	out := filepath.Join(t.TempDir(), "hook-out.log")
	// hooks.yaml present but hooks.enabled left at its default (unset).
	writeHooksYAML(t, db, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

	r := run(t, db, "create", "Gated", "--type", "task", "--acceptance", "x", "--silent")
	if r.code != 0 {
		t.Fatalf("create: code=%d stderr=%s", r.code, r.stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("hook output exists at %s, want no hook run while hooks.enabled is unset", out)
	}

	if r := run(t, db, "config", "set", "hooks.enabled", "true"); r.code != 0 {
		t.Fatalf("config set hooks.enabled: code=%d stderr=%s", r.code, r.stderr)
	}
	r = run(t, db, "create", "Now enabled", "--type", "task", "--acceptance", "x", "--silent")
	if r.code != 0 {
		t.Fatalf("create: code=%d stderr=%s", r.code, r.stderr)
	}
	if lines := readHookLines(t, out); len(lines) != 1 {
		t.Fatalf("hook fired %d times after enabling, want 1", len(lines))
	}
}

func TestHookNoHooksFlagSuppresses(t *testing.T) {
	db := newWorkspace(t)
	out := filepath.Join(t.TempDir(), "hook-out.log")
	enableHooks(t, db, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

	r := run(t, db, "--no-hooks", "create", "Suppressed", "--type", "task", "--acceptance", "x", "--silent")
	if r.code != 0 {
		t.Fatalf("create: code=%d stderr=%s", r.code, r.stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("hook output exists at %s, want --no-hooks to suppress firing", out)
	}
}

func TestHookBDDNoHooksEnvSuppresses(t *testing.T) {
	db := newWorkspace(t)
	out := filepath.Join(t.TempDir(), "hook-out.log")
	enableHooks(t, db, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

	cmd := exec.Command(bddBinary, "--workspace", dbWorkspace(db), "create", "Env suppressed", "--type", "task", "--acceptance", "x", "--silent")
	cmd.Env = append(os.Environ(), "BDD_NO_HOOKS=1")
	if err := cmd.Run(); err != nil {
		t.Fatalf("create with BDD_NO_HOOKS=1: %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("hook output exists at %s, want BDD_NO_HOOKS=1 to suppress firing", out)
	}
}

// --- Re-entrancy: a hook that itself invokes the real bdd binary
// terminates instead of recursing, and the inner mutation still commits. ---

func TestHookReentrancyGuardAgainstRealBinary(t *testing.T) {
	db := newWorkspace(t)
	out := filepath.Join(t.TempDir(), "hook-out.log")
	inner := createCard(t, db)

	// The outer hook records its own invocation, then shells out to the
	// real bdd binary under test (not $PATH) to add a label to a second
	// card. If the re-entrancy guard failed, that inner "label add" would
	// itself be a label-change event and would append a second line to the
	// same log via the label-change hook below.
	script := `cat >> "$1"; echo >> "$1"; "$2" --workspace "$3" label add "$4" inner-fired`
	outerCmd := []string{"/bin/sh", "-c", script, "--", out, bddBinary, dbWorkspace(db), inner}

	enableHooks(t, db, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, outerCmd)+`
  - event: label-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

	r := run(t, db, "create", "Reentrancy trigger", "--type", "task", "--acceptance", "x", "--silent")
	if r.code != 0 {
		t.Fatalf("create: code=%d stderr=%s", r.code, r.stderr)
	}

	lines := readHookLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("outer hook effectively fired %d times (via log lines), want exactly 1 -- an inner label-change line means the inner bdd invocation recursed into hooks", len(lines))
	}
	if lines[0]["event"] != "status-change" {
		t.Fatalf("only line's event = %v, want status-change (the outer hook)", lines[0]["event"])
	}

	show := run(t, db, "show", inner, "--json")
	if show.code != 0 {
		t.Fatalf("show inner card: code=%d stderr=%s", show.code, show.stderr)
	}
	if !strings.Contains(show.stdout, "inner-fired") {
		t.Fatalf("inner card show = %q, want label inner-fired committed by the recursive bdd invocation", show.stdout)
	}
}

// --- Failure semantics: non-zero exit and timeout are advisory. ---

func TestHookNonZeroExitAdvisory(t *testing.T) {
	db := newWorkspace(t)
	enableHooks(t, db, `
version: 1
hooks:
  - event: status-change
    command: ["/bin/sh", "-c", "exit 3"]
`)

	r := run(t, db, "create", "Failing hook", "--type", "task", "--acceptance", "x", "--silent")
	if r.code != 0 {
		t.Fatalf("create: code=%d, want 0 despite hook failure (stderr=%s)", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "bdd: hook status-change") {
		t.Fatalf("stderr = %q, want a hook-failure diagnostic naming the event", r.stderr)
	}
	id := strings.TrimSpace(r.stdout)
	show := run(t, db, "show", id, "--json")
	if show.code != 0 {
		t.Fatalf("show: code=%d stderr=%s", show.code, show.stderr)
	}
	if !strings.Contains(show.stdout, `"status":"open"`) {
		t.Fatalf("show %s = %q, want the mutation committed despite hook failure", id, show.stdout)
	}
}

func TestHookTimeoutAdvisory(t *testing.T) {
	db := newWorkspace(t)
	enableHooks(t, db, `
version: 1
hooks:
  - event: status-change
    timeout: 100ms
    command: ["/bin/sh", "-c", "sleep 5"]
`)

	start := time.Now()
	r := run(t, db, "create", "Slow hook", "--type", "task", "--acceptance", "x", "--silent")
	elapsed := time.Since(start)
	if r.code != 0 {
		t.Fatalf("create: code=%d, want 0 despite hook timeout (stderr=%s)", r.code, r.stderr)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("create took %s, want it bounded by the hook's 100ms timeout rather than the full 5s sleep", elapsed)
	}
	if !strings.Contains(r.stderr, "bdd: hook status-change") {
		t.Fatalf("stderr = %q, want a hook-failure diagnostic naming the event", r.stderr)
	}
	id := strings.TrimSpace(r.stdout)
	show := run(t, db, "show", id, "--json")
	if show.code != 0 {
		t.Fatalf("show: code=%d stderr=%s", show.code, show.stderr)
	}
	if !strings.Contains(show.stdout, `"status":"open"`) {
		t.Fatalf("show %s = %q, want the mutation committed despite hook timeout", id, show.stdout)
	}
}

// --- No double-fire under a concurrent second writer: the sqlite.Retry
// regression guard. Two bdd processes mutate the same card concurrently;
// each successful mutation must fire its hook exactly once, never more even
// if one write had to retry past SQLITE_BUSY. ---

func TestHookNoDoubleFireUnderConcurrentWriters(t *testing.T) {
	db := newWorkspace(t)
	out := filepath.Join(t.TempDir(), "hook-out.log")
	id := createCard(t, db)
	enableHooks(t, db, `
version: 1
hooks:
  - event: label-change
    command: `+mustCommandJSON(t, recordingCommand(out))+`
`)

	labels := []string{"concurrent-a", "concurrent-b"}
	var wg sync.WaitGroup
	results := make([]result, len(labels))
	for i, label := range labels {
		wg.Add(1)
		go func(i int, label string) {
			defer wg.Done()
			// exec directly rather than via run(): t.Fatalf is unsafe to
			// call from a non-test goroutine, and run() may call it.
			// The bounded retry is a defensive margin against transient
			// SQLITE_BUSY on Open() under concurrent access; the specific
			// pragma-ordering gap that once made this common (busy_timeout
			// applied after synchronous) was fixed for bdd-hzlx, but the
			// retry costs nothing and keeps this test robust to any
			// remaining contention on Open() rather than coupling its
			// stability to that fix.
			var r result
			for attempt := 0; attempt < 20; attempt++ {
				cmd := exec.Command(bddBinary, "--workspace", dbWorkspace(db), "label", "add", id, label)
				var stdout, stderr strings.Builder
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				err := cmd.Run()
				code := 0
				if err != nil {
					if exitErr, ok := err.(*exec.ExitError); ok {
						code = exitErr.ExitCode()
					} else {
						code = -1
						stderr.WriteString(err.Error())
					}
				}
				r = result{stdout: stdout.String(), stderr: stderr.String(), code: code}
				if code == 0 || !strings.Contains(r.stderr, "database is locked") {
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			results[i] = r
		}(i, label)
	}
	wg.Wait()

	for i, r := range results {
		if r.code != 0 {
			t.Fatalf("concurrent label add %d: code=%d stderr=%s", i, r.code, r.stderr)
		}
	}

	// Exactly one hook invocation per successful write: this is the
	// double-fire guard. It intentionally does not assert that each
	// invocation's label_change.added names only its own process's label,
	// since the hookSource's pre-mutation read is a separate query from the
	// mutation itself and can race a concurrent writer's commit -- a real,
	// open defect (bdd-o0zu) distinct from double-firing.
	lines := readHookLines(t, out)
	if len(lines) != len(labels) {
		t.Fatalf("hook fired %d times for %d concurrent successful writes, want exactly %d (no double-fire)", len(lines), len(labels), len(labels))
	}

	seenLabels := map[string]bool{}
	for _, l := range lines {
		lc, ok := l["label_change"].(map[string]any)
		if !ok {
			t.Fatalf("line missing label_change: %v", l)
		}
		added, _ := lc["added"].([]any)
		if len(added) == 0 {
			t.Fatalf("label_change.added = %v, want at least one label", lc["added"])
		}
		for _, a := range added {
			seenLabels[a.(string)] = true
		}
	}
	for _, label := range labels {
		if !seenLabels[label] {
			t.Fatalf("label %s never appeared in any label_change.added across %d hook firings", label, len(lines))
		}
	}
}
