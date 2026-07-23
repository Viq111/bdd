// Package e2e_test is QA's black-box, subprocess-level verification of the
// bdd CLI contract (plan sections 7, 19, 23). Unlike cmd/bdd's in-process
// unit tests, every test here execs the actual compiled binary, matching
// how a real agent invokes it and how internal/bench measures latency.
package e2e_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var bddBinary, migrationBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bdd-e2e-bin-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	bddBinary = filepath.Join(dir, "bdd")
	build := exec.Command("go", "build", "-o", bddBinary, "../cmd/bdd")
	if out, err := build.CombinedOutput(); err != nil {
		panic("build bdd: " + err.Error() + "\n" + string(out))
	}
	migrationBinary = filepath.Join(dir, "bdd-migration")
	build = exec.Command("go", "build", "-o", migrationBinary, "../tools/migrate/cmd/bdd-migration")
	if out, err := build.CombinedOutput(); err != nil {
		panic("build bdd-migration: " + err.Error() + "\n" + string(out))
	}

	os.Exit(m.Run())
}

type result struct {
	stdout string
	stderr string
	code   int
}

// dbWorkspace derives the --workspace argument that resolves to db: the
// grandparent directory when db follows the standard <workspace>/.bdd/
// bdd.sqlite layout, or db's own parent directory otherwise (mirroring
// internal/cli's workspaceDir).
func dbWorkspace(db string) string {
	dir := filepath.Dir(db)
	if filepath.Base(dir) == ".bdd" {
		return filepath.Dir(dir)
	}
	return dir
}

func run(t *testing.T, db string, args ...string) result {
	t.Helper()
	full := args
	if db != "" {
		full = append([]string{"--workspace", dbWorkspace(db)}, args...)
	}
	cmd := exec.Command(bddBinary, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = nil
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run %v: %v", full, err)
		}
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func runStdin(t *testing.T, db string, stdin string, args ...string) result {
	t.Helper()
	full := append([]string{"--workspace", dbWorkspace(db)}, args...)
	cmd := exec.Command(bddBinary, full...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run %v: %v", full, err)
		}
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func newWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	r := run(t, "", "init", "--prefix", "qa", dir)
	if r.code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", r.code, r.stderr)
	}
	return filepath.Join(dir, ".bdd", "bdd.sqlite")
}

func cardCount(t *testing.T, db string) int {
	t.Helper()
	r := run(t, db, "list", "--status", "all", "--json")
	if r.code != 0 {
		t.Fatalf("list failed: %s", r.stderr)
	}
	var cards []json.RawMessage
	if err := json.Unmarshal([]byte(r.stdout), &cards); err != nil {
		t.Fatalf("list --json not an array: %v (%s)", err, r.stdout)
	}
	return len(cards)
}

// --- Fast path: version/help never touch a workspace or database. ---

func TestVersionAndHelpFastPath(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"help"}, {}} {
		r := run(t, "/nonexistent/path/should/never/be/opened.sqlite", args...)
		if r.code != 0 {
			t.Fatalf("run(%v) code = %d, want 0 (stderr=%s)", args, r.code, r.stderr)
		}
		if r.stderr != "" {
			t.Fatalf("run(%v) stderr = %q, want empty", args, r.stderr)
		}
	}
}

// --- Missing-required-field contract (section 10, 23). ---

func TestMissingRequiredFieldsListedAllAtOnceNoDBWrite(t *testing.T) {
	db := newWorkspace(t)
	before := cardCount(t, db)

	r := run(t, db, "create", "Broken bug", "--type", "bug")
	if r.code != 2 {
		t.Fatalf("exit code = %d, want 2 (usage)", r.code)
	}
	if r.stdout != "" {
		t.Fatalf("stdout = %q, want empty on validation failure", r.stdout)
	}
	if !strings.Contains(r.stderr, "reproduction") || !strings.Contains(r.stderr, "acceptance") {
		t.Fatalf("stderr = %q, want every missing field named", r.stderr)
	}
	if !strings.Contains(r.stderr, `--reproduce ""`) {
		t.Fatalf("stderr = %q, want a bypass hint showing how to explicitly acknowledge the field", r.stderr)
	}

	after := cardCount(t, db)
	if after != before {
		t.Fatalf("card count changed from %d to %d; a rejected create must not write", before, after)
	}
}

func TestExplicitEmptyAcknowledgesRequiredField(t *testing.T) {
	db := newWorkspace(t)
	r := run(t, db, "create", "Ack bug", "--type", "bug", "--reproduce", "", "--acceptance", "", "--silent")
	if r.code != 0 {
		t.Fatalf("code = %d, want 0 (empty string explicitly acknowledges); stderr=%s", r.code, r.stderr)
	}
}

// --- create --silent exact output (section 19). ---

func TestCreateSilentEmitsOnlyID(t *testing.T) {
	db := newWorkspace(t)
	r := run(t, db, "create", "Chore", "--type", "chore", "--silent")
	if r.code != 0 {
		t.Fatalf("code = %d, stderr=%s", r.code, r.stderr)
	}
	if r.stderr != "" {
		t.Fatalf("stderr = %q, want empty", r.stderr)
	}
	if !strings.HasSuffix(r.stdout, "\n") {
		t.Fatalf("stdout = %q, want trailing newline", r.stdout)
	}
	id := strings.TrimSpace(r.stdout)
	if strings.Contains(id, " ") || strings.Contains(id, "\n") || id == "" {
		t.Fatalf("stdout = %q, want exactly the ID plus a newline", r.stdout)
	}
	if !strings.HasPrefix(id, "qa-") {
		t.Fatalf("id = %q, want qa-<suffix>", id)
	}
}

// --- Both flag syntaxes (section 19, 23). ---

func TestBothFlagSyntaxes(t *testing.T) {
	db := newWorkspace(t)

	r1 := run(t, db, "create", "SpaceForm", "--type", "chore", "--silent")
	if r1.code != 0 {
		t.Fatalf("--flag value form failed: %s", r1.stderr)
	}

	r2 := run(t, db, "create", "EqForm", "--type=chore", "--silent")
	if r2.code != 0 {
		t.Fatalf("--flag=value form failed: %s", r2.stderr)
	}
}

// --- Repeated parent/child flags, atomic multi-edge update (section 14, 23). ---

func TestRepeatedParentFlagsAtomic(t *testing.T) {
	db := newWorkspace(t)
	p1 := strings.TrimSpace(run(t, db, "create", "P1", "--type", "chore", "--silent").stdout)
	p2 := strings.TrimSpace(run(t, db, "create", "P2", "--type", "chore", "--silent").stdout)
	c := strings.TrimSpace(run(t, db, "create", "C", "--type", "chore", "--silent").stdout)

	r := run(t, db, "update", c, "--add-parent", p1, "--add-parent", p2, "--json")
	if r.code != 0 {
		t.Fatalf("multi-parent update failed: %s", r.stderr)
	}

	var card struct {
		Parents []struct {
			ID string `json:"id"`
		} `json:"parents"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &card); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, r.stdout)
	}
	if len(card.Parents) != 2 {
		t.Fatalf("parents = %v, want both p1 and p2 from one atomic update", card.Parents)
	}
}

func TestSelfEdgeAndCycleRejected(t *testing.T) {
	db := newWorkspace(t)
	a := strings.TrimSpace(run(t, db, "create", "A", "--type", "chore", "--silent").stdout)

	r := run(t, db, "update", a, "--add-parent", a)
	if r.code != 2 && r.code != 4 {
		t.Fatalf("self-edge: code = %d, want a usage(2) or conflict(4) rejection, not success", r.code)
	}

	b := strings.TrimSpace(run(t, db, "create", "B", "--type", "chore", "--parent", a, "--silent").stdout)
	rc := run(t, db, "update", a, "--add-parent", b)
	if rc.code != 4 {
		t.Fatalf("cycle: code = %d, want 4 (conflict)", rc.code)
	}
}

// --- File and stdin input (section 19, 23). ---

func TestStdinNote(t *testing.T) {
	db := newWorkspace(t)
	c := strings.TrimSpace(run(t, db, "create", "C", "--type", "chore", "--silent").stdout)

	r := runStdin(t, db, "note body from stdin\n", "note", c, "--stdin", "--json")
	if r.code != 0 {
		t.Fatalf("stdin note failed: %s", r.stderr)
	}
	if !strings.Contains(r.stdout, "note body from stdin") {
		t.Fatalf("stdout = %q, want the stdin body recorded", r.stdout)
	}
}

func TestDescriptionFile(t *testing.T) {
	db := newWorkspace(t)
	f := filepath.Join(t.TempDir(), "desc.txt")
	if err := os.WriteFile(f, []byte("description from a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := run(t, db, "create", "FileDesc", "--type", "chore", "--description-file", f, "--json")
	if r.code != 0 {
		t.Fatalf("--description-file failed: %s", r.stderr)
	}
	if !strings.Contains(r.stdout, "description from a file") {
		t.Fatalf("stdout = %q, want file content in description", r.stdout)
	}
}

// --- Golden human output: Worktree near the top of `show` (section 9, 23). ---

func TestShowHumanWorktreeNearTop(t *testing.T) {
	db := newWorkspace(t)
	c := strings.TrimSpace(run(t, db, "create", "WT", "--type", "chore", "--worktree", ".worktrees/x", "--silent").stdout)

	r := run(t, db, "show", c)
	if r.code != 0 {
		t.Fatalf("show failed: %s", r.stderr)
	}
	lines := strings.Split(strings.TrimRight(r.stdout, "\n"), "\n")
	worktreeLine := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "worktree:") {
			worktreeLine = i
			break
		}
	}
	if worktreeLine == -1 {
		t.Fatalf("show output has no worktree line:\n%s", r.stdout)
	}
	// "Near the top": identity, title, status, and priority all come first
	// per plan section 9, so worktree must appear within the first several
	// lines, not buried after unrelated fields.
	if worktreeLine > 5 {
		t.Fatalf("worktree line at index %d, want it near the top (<=5):\n%s", worktreeLine, r.stdout)
	}
}

// --- JSON conventions: singular object, plural array, RFC3339, null/empty (section 19, 23). ---

func TestJSONConventions(t *testing.T) {
	db := newWorkspace(t)
	c := strings.TrimSpace(run(t, db, "create", "J", "--type", "chore", "--silent").stdout)

	show := run(t, db, "show", c, "--json")
	if !strings.HasPrefix(strings.TrimSpace(show.stdout), "{") {
		t.Fatalf("show --json = %q, want a single JSON object", show.stdout)
	}

	var card map[string]any
	if err := json.Unmarshal([]byte(show.stdout), &card); err != nil {
		t.Fatalf("unmarshal show: %v", err)
	}
	if card["labels"] == nil {
		t.Fatalf("labels = nil, want [] not null")
	}
	if labels, ok := card["labels"].([]any); !ok || len(labels) != 0 {
		t.Fatalf("labels = %v, want empty array", card["labels"])
	}
	if card["started_at"] != nil {
		t.Fatalf("started_at = %v, want null for an unstarted card", card["started_at"])
	}
	createdAt, _ := card["created_at"].(string)
	if _, err := time.Parse(time.RFC3339Nano, createdAt); err != nil {
		if _, err2 := time.Parse(time.RFC3339, createdAt); err2 != nil {
			t.Fatalf("created_at = %q, want RFC3339", createdAt)
		}
	}

	list := run(t, db, "list", "--json")
	trimmed := strings.TrimSpace(list.stdout)
	if !strings.HasPrefix(trimmed, "[") {
		t.Fatalf("list --json = %q, want a JSON array (plural, no envelope)", list.stdout)
	}

	empty := run(t, db, "list", "--status", "closed", "--json")
	if strings.TrimSpace(empty.stdout) != "[]" {
		t.Fatalf("empty list --json = %q, want exactly []", empty.stdout)
	}
}

// --- Stream discipline: no stderr on success under --json/--silent (section 19, 23). ---

func TestNoStderrOnSuccess(t *testing.T) {
	db := newWorkspace(t)
	c := strings.TrimSpace(run(t, db, "create", "Quiet", "--type", "chore", "--silent").stdout)

	for _, args := range [][]string{
		{"show", c, "--json"},
		{"create", "Another", "--type", "chore", "--silent"},
		{"list", "--json"},
	} {
		r := run(t, db, args...)
		if r.code != 0 {
			t.Fatalf("run(%v) failed unexpectedly: %s", args, r.stderr)
		}
		if r.stderr != "" {
			t.Fatalf("run(%v) stderr = %q, want empty on success", args, r.stderr)
		}
	}
}

// --- Exit-code contract: 0/2/3/4/1 (section 19, 23). ---

func TestExitCodeContract(t *testing.T) {
	db := newWorkspace(t)

	if r := run(t, db, "show", strings.TrimSpace(run(t, db, "create", "OK", "--type", "chore", "--silent").stdout), "--json"); r.code != 0 {
		t.Fatalf("success case: code = %d, want 0", r.code)
	}

	if r := run(t, db, "create", "Bad", "--type", "bug"); r.code != 2 {
		t.Fatalf("usage case: code = %d, want 2", r.code)
	}

	if r := run(t, db, "show", "qa-doesnotexist"); r.code != 3 {
		t.Fatalf("not-found case: code = %d, want 3", r.code)
	}

	claimed := strings.TrimSpace(run(t, db, "create", "Claim", "--type", "chore", "--silent").stdout)
	run(t, db, "--actor", "alice", "update", claimed, "--claim")
	if r := run(t, db, "--actor", "bob", "update", claimed, "--claim"); r.code != 4 {
		t.Fatalf("conflict case: code = %d, want 4", r.code)
	}

	garbage := filepath.Join(t.TempDir(), ".bdd", "bdd.sqlite")
	if err := os.MkdirAll(filepath.Dir(garbage), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(garbage, []byte("not a sqlite file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := run(t, garbage, "show", "qa-abc"); r.code != 1 {
		t.Fatalf("other-failure case: code = %d, want 1 (stderr=%s)", r.code, r.stderr)
	}
}

// --- Protected rune mutations/removal require --force (section 12, 23). ---

func TestProtectedRuneRequiresForce(t *testing.T) {
	db := newWorkspace(t)
	run(t, db, "rune", "set", "role/qa", "--kind", "role", "--title", "QA", "--body", "body", "--protected")

	if r := run(t, db, "rune", "set", "role/qa", "--kind", "role", "--title", "QA2", "--body", "body2"); r.code != 4 {
		t.Fatalf("update without --force: code = %d, want 4", r.code)
	}
	if r := run(t, db, "rune", "disable", "role/qa"); r.code != 4 {
		t.Fatalf("disable without --force: code = %d, want 4", r.code)
	}
	if r := run(t, db, "rune", "remove", "role/qa"); r.code != 4 {
		t.Fatalf("remove without --force: code = %d, want 4", r.code)
	}
	if r := run(t, db, "rune", "remove", "role/qa", "--force"); r.code != 0 {
		t.Fatalf("remove with --force: code = %d, want 0 (stderr=%s)", r.code, r.stderr)
	}
}

// --- prime: memory inclusion and context limits (section 11, 19, 23). ---

func TestPrimeMemoryInclusionAndLimits(t *testing.T) {
	db := newWorkspace(t)
	run(t, db, "memory", "set", "first memory body", "--key", "k1")
	run(t, db, "memory", "set", "second memory body", "--key", "k2")

	compact := run(t, db, "prime")
	if compact.code != 0 {
		t.Fatalf("prime failed: %s", compact.stderr)
	}
	if !strings.Contains(compact.stdout, "k1") || !strings.Contains(compact.stdout, "k2") {
		t.Fatalf("prime output missing memories:\n%s", compact.stdout)
	}

	none := run(t, db, "prime", "--no-memories")
	if strings.Contains(none.stdout, "first memory body") {
		t.Fatalf("--no-memories still printed memory bodies:\n%s", none.stdout)
	}

	limited := run(t, db, "prime", "--memory-limit", "1", "--json")
	var out struct {
		OptionalContext []struct {
			Type string `json:"type"`
		} `json:"optional_context"`
		Omitted struct {
			Memories struct {
				Total    int `json:"total"`
				Returned int `json:"returned"`
			} `json:"memories"`
		} `json:"omitted"`
	}
	if err := json.Unmarshal([]byte(limited.stdout), &out); err != nil {
		t.Fatalf("prime --json unmarshal: %v (%s)", err, limited.stdout)
	}
	if out.Omitted.Memories.Total != 2 || out.Omitted.Memories.Returned != 1 {
		t.Fatalf("--memory-limit 1 omitted.memories = %+v, want total=2 returned=1", out.Omitted.Memories)
	}
	memCount := 0
	for _, e := range out.OptionalContext {
		if e.Type == "memory" {
			memCount++
		}
	}
	if memCount != 1 {
		t.Fatalf("--memory-limit 1 returned %d memory entries in optional_context, want 1", memCount)
	}

	full := run(t, db, "prime", "--full")
	if full.code != 0 {
		t.Fatalf("prime --full failed: %s", full.stderr)
	}
	if !strings.Contains(full.stdout, "k1") || !strings.Contains(full.stdout, "k2") {
		t.Fatalf("prime --full output missing memories:\n%s", full.stdout)
	}
}

// --- prime: a memory marked --prime required is inlined, not just
// summarized (bd bdd-lrak). ---

func TestPrimeRequiredMemoryIsInlined(t *testing.T) {
	db := newWorkspace(t)
	run(t, db, "memory", "set", "always run the race tests", "--key", "testing-race", "--prime", "required")
	run(t, db, "memory", "set", "prefer small PRs", "--key", "pr-size")

	compact := run(t, db, "prime", "--json")
	if compact.code != 0 {
		t.Fatalf("prime --json failed: %s", compact.stderr)
	}
	var out struct {
		RequiredMemories []struct {
			Type string `json:"type"`
			Key  string `json:"key"`
			Body string `json:"body"`
		} `json:"required_memories"`
	}
	if err := json.Unmarshal([]byte(compact.stdout), &out); err != nil {
		t.Fatalf("prime --json unmarshal: %v (%s)", err, compact.stdout)
	}
	found := false
	for _, e := range out.RequiredMemories {
		if e.Type == "memory" && e.Key == "testing-race" {
			found = true
			if e.Body != "always run the race tests" {
				t.Fatalf("required memory body = %q, want full body", e.Body)
			}
		}
	}
	if !found {
		t.Fatalf("required_memories missing testing-race memory: %+v", out.RequiredMemories)
	}
}

// --- delete requires --force (section 14, 23). ---

func TestDeleteRequiresForce(t *testing.T) {
	db := newWorkspace(t)
	c := strings.TrimSpace(run(t, db, "create", "ToDelete", "--type", "chore", "--silent").stdout)

	if r := run(t, db, "delete", c); r.code != 2 {
		t.Fatalf("delete without --force: code = %d, want 2", r.code)
	}
	if r := run(t, db, "delete", c, "--force"); r.code != 0 {
		t.Fatalf("delete with --force: code = %d, want 0 (stderr=%s)", r.code, r.stderr)
	}
}

// --- Label add/remove idempotence (section 10, 16, 23). ---

func TestLabelIdempotence(t *testing.T) {
	db := newWorkspace(t)
	c := strings.TrimSpace(run(t, db, "create", "L", "--type", "chore", "--silent").stdout)

	for i := 0; i < 2; i++ {
		if r := run(t, db, "label", "add", c, "dup"); r.code != 0 {
			t.Fatalf("label add[%d]: code = %d, want 0", i, r.code)
		}
	}
	if r := run(t, db, "label", "remove", c, "never-added"); r.code != 0 {
		t.Fatalf("removing an absent label: code = %d, want 0 (no-op success)", r.code)
	}
}

// --- Per-command -h contract: every subcommand and every subcommand-group
// member supports -h with a Usage: line and an Examples: section, and
// never touches a workspace or database (cobra migration, bd bdd-s7m). This
// table is the external contract: it must be updated by hand whenever a
// command is added, so a missing entry (and therefore a missing -h) is
// caught by review rather than by reflecting cobra's own command tree. ---

var allSubcommands = [][]string{
	{"init"},
	{"status"},
	{"config"},
	{"config", "get"},
	{"config", "set"},
	{"config", "unset"},
	{"config", "list"},
	{"statuses"},
	{"types"},
	{"memory"},
	{"memory", "set"},
	{"memory", "get"},
	{"memory", "list"},
	{"memory", "search"},
	{"memory", "remove"},
	{"rune"},
	{"rune", "set"},
	{"rune", "get"},
	{"rune", "list"},
	{"rune", "search"},
	{"rune", "enable"},
	{"rune", "disable"},
	{"rune", "remove"},
	{"create"},
	{"show"},
	{"list"},
	{"search"},
	{"ready"},
	{"update"},
	{"note"},
	{"close"},
	{"reopen"},
	{"defer"},
	{"human"},
	{"parents"},
	{"children"},
	{"label"},
	{"label", "add"},
	{"label", "remove"},
	{"label", "list"},
	{"delete"},
	{"snapshot"},
	{"restore"},
	{"prime"},
}

func TestHelpFlagEverySubcommand(t *testing.T) {
	dbDir := t.TempDir()
	db := filepath.Join(dbDir, "should-never-be-opened.sqlite")

	for _, cmd := range allSubcommands {
		name := strings.Join(cmd, " ")
		t.Run(name, func(t *testing.T) {
			r := run(t, db, append(append([]string{}, cmd...), "-h")...)
			if r.code != 0 {
				t.Fatalf("bdd %s -h: code = %d, want 0 (stderr=%s)", name, r.code, r.stderr)
			}
			if !strings.Contains(r.stdout, "Usage:") {
				t.Fatalf("bdd %s -h: stdout missing Usage: line:\n%s", name, r.stdout)
			}
			if !strings.Contains(r.stdout, "Examples:") {
				t.Fatalf("bdd %s -h: stdout missing Examples: section:\n%s", name, r.stdout)
			}
			if _, err := os.Stat(db); err == nil {
				t.Fatalf("bdd %s -h: created a database file at %s", name, db)
			}
		})
	}
}

// --- Post-migration exit-code contract, black-box side (bd bdd-s7m). ---

func TestUnknownCommandAndFlagExitUsage(t *testing.T) {
	db := newWorkspace(t)

	if r := run(t, db, "nosuchcommand"); r.code != 2 {
		t.Fatalf("unknown command: code = %d, want 2 (stderr=%s)", r.code, r.stderr)
	}
	if r := run(t, db, "create", "--nope"); r.code != 2 {
		t.Fatalf("unknown flag on real command: code = %d, want 2 (stderr=%s)", r.code, r.stderr)
	}
}

// TestRemovedDBFlagRejected verifies the global --db flag (removed in
// favor of --workspace and upward .bdd discovery) is no longer recognized
// and is identified as an unknown flag -- not a positional argument -- in
// both its "--db <path>" and "--db=<path>" forms, and regardless of which
// subcommand it precedes. Every subcommand listed here once had its own
// arg-parsing loop that unconditionally reported any unrecognized token as
// an "unknown argument", even flag-shaped ones like --db.
func TestRemovedDBFlagRejected(t *testing.T) {
	db := newWorkspace(t)

	cases := []struct {
		name    string
		dbArgs  []string // tokens preceding the subcommand
		wantArg string   // flag token the error message must name
		subArgs []string
	}{
		{"status/space", []string{"--db", db}, "--db", []string{"status"}},
		{"status/equals", []string{"--db=" + db}, "--db=" + db, []string{"status"}},
		{"list/space", []string{"--db", db}, "--db", []string{"list"}},
		{"list/equals", []string{"--db=" + db}, "--db=" + db, []string{"list"}},
		{"search/space", []string{"--db", db}, "--db", []string{"search", "query"}},
		{"search/equals", []string{"--db=" + db}, "--db=" + db, []string{"search", "query"}},
		{"prime/space", []string{"--db", db}, "--db", []string{"prime"}},
		{"prime/equals", []string{"--db=" + db}, "--db=" + db, []string{"prime"}},
		{"snapshot/space", []string{"--db", db}, "--db", []string{"snapshot"}},
		{"snapshot/equals", []string{"--db=" + db}, "--db=" + db, []string{"snapshot"}},
		{"rune-search/space", []string{"--db", db}, "--db", []string{"rune", "search", "query"}},
		{"rune-search/equals", []string{"--db=" + db}, "--db=" + db, []string{"rune", "search", "query"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append(append([]string{}, tc.dbArgs...), tc.subArgs...)
			cmd := exec.Command(bddBinary, args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			code := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					code = exitErr.ExitCode()
				} else {
					t.Fatalf("run: %v", err)
				}
			}
			if code != 2 {
				t.Fatalf("bdd %v: code = %d, want 2 (unknown flag); stderr=%s", args, code, stderr.String())
			}
			want := `unknown flag "` + tc.wantArg + `"`
			if !strings.Contains(stderr.String(), want) {
				t.Fatalf("bdd %v: stderr = %q, want it to contain %q", args, stderr.String(), want)
			}
		})
	}
}

// TestRemovedDBFlagRejectedInTrailingPositional is a regression test for
// bd bdd-md64: --db appearing after the required id, in an optional
// free-form positional slot (close/human's [reason]), must be rejected as
// an unknown flag before any mutation happens -- not accepted as literal
// reason text.
func TestRemovedDBFlagRejectedInTrailingPositional(t *testing.T) {
	db := newWorkspace(t)
	id := run(t, db, "create", "--type", "chore", "--silent", "trailing --db regression").stdout
	id = strings.TrimSpace(id)

	cases := []string{"close", "human"}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			r := run(t, db, cmd, id, "--db")
			if r.code != 2 {
				t.Fatalf("bdd %s %s --db: code = %d, want 2 (unknown flag); stderr=%s", cmd, id, r.code, r.stderr)
			}
			want := `unknown flag "--db"`
			if !strings.Contains(r.stderr, want) {
				t.Fatalf("bdd %s %s --db: stderr = %q, want it to contain %q", cmd, id, r.stderr, want)
			}

			show := run(t, db, "show", id, "--json")
			if show.code != 0 {
				t.Fatalf("bdd show %s: code = %d, stderr=%s", id, show.code, show.stderr)
			}
			if strings.Contains(show.stdout, `"status":"closed"`) || strings.Contains(show.stdout, `"labels":["human"]`) {
				t.Fatalf("bdd show %s after rejected %s --db: stdout = %q, want no mutation", id, cmd, show.stdout)
			}
		})
	}
}
