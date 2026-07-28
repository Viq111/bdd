package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// enableHooks writes hooks.yaml with content, then flips hooks.enabled on
// for the workspace at dir.
func enableHooks(t *testing.T, dir, content string) {
	t.Helper()
	writeHooksYAML(t, dir, content)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"config", "set", "hooks.enabled", "true", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(config set) exit = %d, stderr = %q", code, stderr.String())
	}
}

// recordingHook returns a hooks.yaml "command" list for event that appends
// its stdin JSON as one line to outPath, using /bin/sh with no interpolation
// of untrusted input (outPath is a test-controlled temp path).
func recordingHookCommand(t *testing.T, outPath string) []string {
	t.Helper()
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

func mustCommandJSON(t *testing.T, cmd []string) string {
	t.Helper()
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHookFiresOnCreateStatusChange(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "hook-out.log")

	enableHooks(t, dir, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingHookCommand(t, out))+`
`)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--json", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, stderr = %q", code, stderr.String())
	}

	lines := readHookLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("hook fired %d times, want 1 (lines=%v)", len(lines), lines)
	}
	payload := lines[0]
	if payload["event"] != "status-change" {
		t.Fatalf("event = %v, want status-change", payload["event"])
	}
	sc, ok := payload["status_change"].(map[string]any)
	if !ok {
		t.Fatalf("status_change missing or wrong type: %v", payload["status_change"])
	}
	if sc["from"] != "" {
		t.Fatalf("status_change.from = %v, want empty", sc["from"])
	}
	if sc["to"] != "open" {
		t.Fatalf("status_change.to = %v, want open", sc["to"])
	}
	if payload["label_change"] != nil {
		t.Fatalf("label_change = %v, want nil (no label-change on create)", payload["label_change"])
	}
	card, ok := payload["card"].(map[string]any)
	if !ok {
		t.Fatalf("card missing or wrong type: %v", payload["card"])
	}
	if card["status"] != "open" {
		t.Fatalf("card.status = %v, want open", card["status"])
	}
}

func TestHookCreateWithLabelsDoesNotEmitLabelChange(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "hook-out.log")

	enableHooks(t, dir, `
version: 1
hooks:
  - event: label-change
    command: `+mustCommandJSON(t, recordingHookCommand(t, out))+`
`)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--label", "foo", "--json", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, stderr = %q", code, stderr.String())
	}

	if lines := readHookLines(t, out); len(lines) != 0 {
		t.Fatalf("label-change hook fired %d times on create, want 0", len(lines))
	}
}

func TestHookUpdateStatusThenLabelOrder(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "hook-out.log")

	var createOut, createErr bytes.Buffer
	if code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--json", "--workspace", dir}, &createOut, &createErr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, stderr = %q", code, createErr.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createOut.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	enableHooks(t, dir, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingHookCommand(t, out))+`
  - event: label-change
    command: `+mustCommandJSON(t, recordingHookCommand(t, out))+`
`)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"update", created.ID, "--status", "in_progress", "--add-label", "urgent", "--json", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(update) exit = %d, stderr = %q", code, stderr.String())
	}

	lines := readHookLines(t, out)
	if len(lines) != 2 {
		t.Fatalf("hooks fired %d times, want 2 (lines=%v)", len(lines), lines)
	}
	if lines[0]["event"] != "status-change" {
		t.Fatalf("first event = %v, want status-change", lines[0]["event"])
	}
	if lines[1]["event"] != "label-change" {
		t.Fatalf("second event = %v, want label-change", lines[1]["event"])
	}
	sc := lines[0]["status_change"].(map[string]any)
	if sc["from"] != "open" || sc["to"] != "in_progress" {
		t.Fatalf("status_change = %v, want open->in_progress", sc)
	}
	lc := lines[1]["label_change"].(map[string]any)
	added, _ := lc["added"].([]any)
	if len(added) != 1 || added[0] != "urgent" {
		t.Fatalf("label_change.added = %v, want [urgent]", lc["added"])
	}
}

func TestHookClaimEmitsStatusChange(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "hook-out.log")

	var createOut, createErr bytes.Buffer
	if code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--json", "--workspace", dir}, &createOut, &createErr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, stderr = %q", code, createErr.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createOut.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	enableHooks(t, dir, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingHookCommand(t, out))+`
`)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"update", created.ID, "--claim", "--json", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(update --claim) exit = %d, stderr = %q", code, stderr.String())
	}

	lines := readHookLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("hook fired %d times, want 1", len(lines))
	}
	sc := lines[0]["status_change"].(map[string]any)
	if sc["to"] != "in_progress" {
		t.Fatalf("status_change.to = %v, want in_progress", sc["to"])
	}
}

func TestHookLabelAddRemove(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "hook-out.log")

	var createOut, createErr bytes.Buffer
	if code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--label", "keep", "--json", "--workspace", dir}, &createOut, &createErr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, stderr = %q", code, createErr.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createOut.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	enableHooks(t, dir, `
version: 1
hooks:
  - event: label-change
    command: `+mustCommandJSON(t, recordingHookCommand(t, out))+`
`)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"label", "add", created.ID, "urgent", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(label add) exit = %d, stderr = %q", code, stderr.String())
	}
	if code := Run([]string{"label", "remove", created.ID, "keep", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(label remove) exit = %d, stderr = %q", code, stderr.String())
	}

	lines := readHookLines(t, out)
	if len(lines) != 2 {
		t.Fatalf("hook fired %d times, want 2 (lines=%v)", len(lines), lines)
	}
	add := lines[0]["label_change"].(map[string]any)
	addedList, _ := add["added"].([]any)
	if len(addedList) != 1 || addedList[0] != "urgent" {
		t.Fatalf("first label_change.added = %v, want [urgent]", add["added"])
	}
	if removed, ok := add["removed"].([]any); !ok || len(removed) != 0 {
		t.Fatalf("first label_change.removed = %#v, want [] (empty array, not null)", add["removed"])
	}
	remove := lines[1]["label_change"].(map[string]any)
	removedList, _ := remove["removed"].([]any)
	if len(removedList) != 1 || removedList[0] != "keep" {
		t.Fatalf("second label_change.removed = %v, want [keep]", remove["removed"])
	}
	if added, ok := remove["added"].([]any); !ok || len(added) != 0 {
		t.Fatalf("second label_change.added = %#v, want [] (empty array, not null)", remove["added"])
	}

	rawData, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawData), "null") {
		t.Fatalf("hook payload contains null, want [] for unchanged label side: %s", rawData)
	}
}

func TestHookIdempotentLabelAddFiresNothing(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "hook-out.log")

	var createOut, createErr bytes.Buffer
	if code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--label", "keep", "--json", "--workspace", dir}, &createOut, &createErr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, stderr = %q", code, createErr.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createOut.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	enableHooks(t, dir, `
version: 1
hooks:
  - event: label-change
    command: `+mustCommandJSON(t, recordingHookCommand(t, out))+`
`)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"label", "add", created.ID, "keep", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(label add) exit = %d, stderr = %q", code, stderr.String())
	}

	if lines := readHookLines(t, out); len(lines) != 0 {
		t.Fatalf("hook fired %d times for a no-op label add, want 0", len(lines))
	}
}

func TestHookNotFiredWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "hook-out.log")

	// hooks.yaml present but hooks.enabled left at its default (off).
	writeHooksYAML(t, dir, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingHookCommand(t, out))+`
`)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--json", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, stderr = %q", code, stderr.String())
	}

	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("hook output file exists at %s, want no process spawned", out)
	}
}

func TestHookNotFiredWhenNoHooksFlag(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "hook-out.log")

	enableHooks(t, dir, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingHookCommand(t, out))+`
`)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--json", "--workspace", dir, "--no-hooks"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, stderr = %q", code, stderr.String())
	}

	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("hook output file exists at %s, want --no-hooks to suppress firing", out)
	}
}

func TestHookReentrancyGuard(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "hook-out.log")

	enableHooks(t, dir, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingHookCommand(t, out))+`
`)

	t.Setenv("BDD_HOOK_DEPTH", "1")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--json", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, stderr = %q", code, stderr.String())
	}

	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("hook output file exists at %s, want BDD_HOOK_DEPTH to suppress firing", out)
	}
}

func TestHookProcessSeesDepthEnvVar(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "hook-out.log")

	enableHooks(t, dir, `
version: 1
hooks:
  - event: status-change
    command: ["/bin/sh", "-c", `+jsonStr(t, `printf '%s' "$BDD_HOOK_DEPTH" > "$1"`)+`, "--", `+jsonStr(t, out)+`]
`)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--json", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, stderr = %q", code, stderr.String())
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "1" {
		t.Fatalf("BDD_HOOK_DEPTH seen by hook = %q, want \"1\"", data)
	}
}

func jsonStr(t *testing.T, s string) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHookNonZeroExitIsAdvisory(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	enableHooks(t, dir, `
version: 1
hooks:
  - event: status-change
    command: ["/bin/sh", "-c", "exit 3"]
`)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--json", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, want 0 despite hook failure (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "bdd: hook status-change") {
		t.Fatalf("stderr = %q, want a hook-failure diagnostic naming the event", stderr.String())
	}
}

func TestHookTimeoutIsAdvisory(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	enableHooks(t, dir, `
version: 1
hooks:
  - event: status-change
    timeout: 100ms
    command: ["/bin/sh", "-c", "sleep 5"]
`)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--json", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, want 0 despite hook timeout (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "bdd: hook status-change") {
		t.Fatalf("stderr = %q, want a hook-failure diagnostic naming the event", stderr.String())
	}
}

func TestHookNeverFiresWhenMutationFails(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "hook-out.log")

	enableHooks(t, dir, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingHookCommand(t, out))+`
`)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"close", "acme-nonexistent", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code == ExitSuccess {
		t.Fatalf("Run(close nonexistent) exit = 0, want failure")
	}

	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("hook output file exists at %s, want no hook fired on a failed mutation", out)
	}
}

func TestHookFiresOnCloseReopenDefer(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "hook-out.log")

	var createOut, createErr bytes.Buffer
	if code := Run([]string{"create", "a card", "--type", "task", "--acceptance", "x", "--json", "--workspace", dir}, &createOut, &createErr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, stderr = %q", code, createErr.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createOut.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	enableHooks(t, dir, `
version: 1
hooks:
  - event: status-change
    command: `+mustCommandJSON(t, recordingHookCommand(t, out))+`
`)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"close", created.ID, "--workspace", dir}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(close) exit = %d, stderr = %q", code, stderr.String())
	}
	if code := Run([]string{"reopen", created.ID, "--workspace", dir}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(reopen) exit = %d, stderr = %q", code, stderr.String())
	}
	if code := Run([]string{"defer", created.ID, "--workspace", dir}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(defer) exit = %d, stderr = %q", code, stderr.String())
	}

	lines := readHookLines(t, out)
	if len(lines) != 3 {
		t.Fatalf("hook fired %d times, want 3 (close, reopen, defer): %v", len(lines), lines)
	}
	wantTo := []string{"closed", "open", "deferred"}
	for i, want := range wantTo {
		sc := lines[i]["status_change"].(map[string]any)
		if sc["to"] != want {
			t.Fatalf("event %d status_change.to = %v, want %s", i, sc["to"], want)
		}
	}
}

// TestLabelDiffReturnsEmptySlicesNotNil guards the label-change hook payload
// contract: the unchanged side must serialize as [], not null.
func TestLabelDiffReturnsEmptySlicesNotNil(t *testing.T) {
	added, removed := labelDiff([]string{"keep"}, []string{"keep", "urgent"})
	if added == nil || len(added) != 1 || added[0] != "urgent" {
		t.Fatalf("added = %#v, want [urgent]", added)
	}
	if removed == nil || len(removed) != 0 {
		t.Fatalf("removed = %#v, want non-nil empty slice", removed)
	}

	added, removed = labelDiff([]string{"keep", "urgent"}, []string{"keep"})
	if removed == nil || len(removed) != 1 || removed[0] != "urgent" {
		t.Fatalf("removed = %#v, want [urgent]", removed)
	}
	if added == nil || len(added) != 0 {
		t.Fatalf("added = %#v, want non-nil empty slice", added)
	}
}
