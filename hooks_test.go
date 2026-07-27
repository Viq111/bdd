package bdd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeHooksFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "hooks.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadHooksFileMissingIsNotError(t *testing.T) {
	dir := t.TempDir()
	hf, err := LoadHooksFile(filepath.Join(dir, "hooks.yaml"))
	if err != nil {
		t.Fatalf("LoadHooksFile() error = %v, want nil", err)
	}
	if len(hf.Hooks) != 0 {
		t.Fatalf("hf.Hooks = %v, want empty", hf.Hooks)
	}
}

func TestLoadHooksFileValid(t *testing.T) {
	dir := t.TempDir()
	path := writeHooksFile(t, dir, `
version: 1
hooks:
  - event: status-change
    to_status: [awaiting_review]
    command: ["orcha", "hook", "bdd-status"]
    timeout: 5s
  - event: label-change
    added: [review-approved, review-changes-needed]
    command: ["orcha", "hook", "bdd-label"]
`)

	hf, err := LoadHooksFile(path)
	if err != nil {
		t.Fatalf("LoadHooksFile() error = %v", err)
	}
	if len(hf.Hooks) != 2 {
		t.Fatalf("len(hf.Hooks) = %d, want 2", len(hf.Hooks))
	}

	h0 := hf.Hooks[0]
	if h0.Event != HookEventStatusChange {
		t.Errorf("h0.Event = %q, want %q", h0.Event, HookEventStatusChange)
	}
	if len(h0.ToStatus) != 1 || h0.ToStatus[0] != "awaiting_review" {
		t.Errorf("h0.ToStatus = %v, want [awaiting_review]", h0.ToStatus)
	}
	if h0.Timeout != 5*time.Second {
		t.Errorf("h0.Timeout = %v, want 5s", h0.Timeout)
	}

	h1 := hf.Hooks[1]
	if h1.Event != HookEventLabelChange {
		t.Errorf("h1.Event = %q, want %q", h1.Event, HookEventLabelChange)
	}
	if h1.Timeout != 10*time.Second {
		t.Errorf("h1.Timeout = %v, want default 10s", h1.Timeout)
	}
}

func TestLoadHooksFileRejections(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "unknown top-level key",
			content: `
version: 1
bogus: true
hooks: []
`,
			want: `unknown top-level key "bogus"`,
		},
		{
			name: "unknown version",
			content: `
version: 2
hooks: []
`,
			want: "unrecognized version 2",
		},
		{
			name: "unknown per-hook key",
			content: `
version: 1
hooks:
  - event: status-change
    command: ["x"]
    bogus: true
`,
			want: `hook 0: unknown key "bogus"`,
		},
		{
			name: "missing event",
			content: `
version: 1
hooks:
  - command: ["x"]
`,
			want: `hook 0: missing required key "event"`,
		},
		{
			name: "unknown event",
			content: `
version: 1
hooks:
  - event: bogus
    command: ["x"]
`,
			want: `hook 0: unknown event "bogus"`,
		},
		{
			name: "empty command",
			content: `
version: 1
hooks:
  - event: status-change
    command: []
`,
			want: "hook 0: command must be a non-empty list",
		},
		{
			name: "missing command",
			content: `
version: 1
hooks:
  - event: status-change
`,
			want: `hook 0: missing required key "command"`,
		},
		{
			name: "status filter on label-change hook",
			content: `
version: 1
hooks:
  - event: label-change
    to_status: [open]
    command: ["x"]
`,
			want: "hook 0: to_status is not valid for label-change hooks",
		},
		{
			name: "label filter on status-change hook",
			content: `
version: 1
hooks:
  - event: status-change
    added: [urgent]
    command: ["x"]
`,
			want: "hook 0: added is not valid for status-change hooks",
		},
		{
			name: "unparseable timeout",
			content: `
version: 1
hooks:
  - event: status-change
    command: ["x"]
    timeout: not-a-duration
`,
			want: "hook 0: timeout",
		},
		{
			name: "second hook index reported",
			content: `
version: 1
hooks:
  - event: status-change
    command: ["x"]
  - event: status-change
    bogus: true
    command: ["x"]
`,
			want: `hook 1: unknown key "bogus"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeHooksFile(t, dir, tc.content)
			_, err := LoadHooksFile(path)
			if err == nil {
				t.Fatalf("LoadHooksFile() error = nil, want error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadHooksFile() error = %q, want it to contain %q", err.Error(), tc.want)
			}
			if !strings.Contains(err.Error(), path) {
				t.Fatalf("LoadHooksFile() error = %q, want it to name the file %q", err.Error(), path)
			}
		})
	}
}

func TestHooksEnabledDefaultsFalse(t *testing.T) {
	dir := t.TempDir()
	db, err := Init(context.Background(), InitOptions{Workspace: dir, Prefix: "test"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	enabled, err := db.HooksEnabled(ctx)
	if err != nil {
		t.Fatalf("HooksEnabled() error = %v", err)
	}
	if enabled {
		t.Fatal("HooksEnabled() = true, want false before opt-in")
	}

	if err := db.ConfigSet(ctx, ConfigKeyHooksEnabled, "true", "tester"); err != nil {
		t.Fatalf("ConfigSet() error = %v", err)
	}
	enabled, err = db.HooksEnabled(ctx)
	if err != nil {
		t.Fatalf("HooksEnabled() error = %v", err)
	}
	if !enabled {
		t.Fatal("HooksEnabled() = false, want true after opt-in")
	}
}

func TestHooksPathAlongsideDatabase(t *testing.T) {
	dir := t.TempDir()
	db, err := Init(context.Background(), InitOptions{Workspace: dir, Prefix: "test"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer db.Close()

	want := filepath.Join(dir, ".bdd", "hooks.yaml")
	if got := db.HooksPath(); got != want {
		t.Fatalf("HooksPath() = %q, want %q", got, want)
	}
}

func TestMatchHooksNoFiltersMatchesEverything(t *testing.T) {
	hooks := []Hook{{Event: HookEventStatusChange, Command: []string{"x"}}}
	ev := MatchEvent{Event: HookEventStatusChange, FromStatus: "open", ToStatus: "in_progress"}
	got := MatchHooks(hooks, ev)
	if len(got) != 1 {
		t.Fatalf("MatchHooks() = %v, want 1 match", got)
	}
}

func TestMatchHooksToStatusFilter(t *testing.T) {
	hooks := []Hook{{Event: HookEventStatusChange, ToStatus: []string{"awaiting_review", "in_progress"}, Command: []string{"x"}}}

	match := MatchHooks(hooks, MatchEvent{Event: HookEventStatusChange, ToStatus: "awaiting_review"})
	if len(match) != 1 {
		t.Fatalf("MatchHooks(to_status=awaiting_review) = %v, want 1 match", match)
	}

	noMatch := MatchHooks(hooks, MatchEvent{Event: HookEventStatusChange, ToStatus: "closed"})
	if len(noMatch) != 0 {
		t.Fatalf("MatchHooks(to_status=closed) = %v, want no match", noMatch)
	}
}

func TestMatchHooksFromAndToStatusRequireBoth(t *testing.T) {
	hooks := []Hook{{
		Event:      HookEventStatusChange,
		FromStatus: []string{"in_progress"},
		ToStatus:   []string{"awaiting_review"},
		Command:    []string{"x"},
	}}

	match := MatchHooks(hooks, MatchEvent{Event: HookEventStatusChange, FromStatus: "in_progress", ToStatus: "awaiting_review"})
	if len(match) != 1 {
		t.Fatalf("MatchHooks(both match) = %v, want 1 match", match)
	}

	noMatch := MatchHooks(hooks, MatchEvent{Event: HookEventStatusChange, FromStatus: "open", ToStatus: "awaiting_review"})
	if len(noMatch) != 0 {
		t.Fatalf("MatchHooks(from mismatched) = %v, want no match", noMatch)
	}
}

func TestMatchHooksLabelDeltas(t *testing.T) {
	hooks := []Hook{{
		Event:   HookEventLabelChange,
		Added:   []string{"review-approved", "review-changes-needed"},
		Command: []string{"x"},
	}}

	match := MatchHooks(hooks, MatchEvent{Event: HookEventLabelChange, Added: []string{"review-approved"}})
	if len(match) != 1 {
		t.Fatalf("MatchHooks(added overlap) = %v, want 1 match", match)
	}

	noMatch := MatchHooks(hooks, MatchEvent{Event: HookEventLabelChange, Added: []string{"unrelated"}})
	if len(noMatch) != 0 {
		t.Fatalf("MatchHooks(added no overlap) = %v, want no match", noMatch)
	}

	removedHook := []Hook{{Event: HookEventLabelChange, Removed: []string{"blocked"}, Command: []string{"x"}}}
	match = MatchHooks(removedHook, MatchEvent{Event: HookEventLabelChange, Removed: []string{"blocked"}})
	if len(match) != 1 {
		t.Fatalf("MatchHooks(removed overlap) = %v, want 1 match", match)
	}
}

func TestMatchHooksIssueTypeNarrows(t *testing.T) {
	hooks := []Hook{{Event: HookEventStatusChange, IssueType: []string{"bug"}, Command: []string{"x"}}}

	match := MatchHooks(hooks, MatchEvent{Event: HookEventStatusChange, IssueType: "bug"})
	if len(match) != 1 {
		t.Fatalf("MatchHooks(issue_type=bug) = %v, want 1 match", match)
	}

	noMatch := MatchHooks(hooks, MatchEvent{Event: HookEventStatusChange, IssueType: "task"})
	if len(noMatch) != 0 {
		t.Fatalf("MatchHooks(issue_type=task) = %v, want no match", noMatch)
	}
}

func TestMatchHooksFileOrder(t *testing.T) {
	hooks := []Hook{
		{Event: HookEventStatusChange, Command: []string{"first"}},
		{Event: HookEventStatusChange, Command: []string{"second"}},
		{Event: HookEventStatusChange, Command: []string{"third"}},
	}
	got := MatchHooks(hooks, MatchEvent{Event: HookEventStatusChange})
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	for i, want := range []string{"first", "second", "third"} {
		if got[i].Command[0] != want {
			t.Fatalf("got[%d].Command[0] = %q, want %q", i, got[i].Command[0], want)
		}
	}
}
