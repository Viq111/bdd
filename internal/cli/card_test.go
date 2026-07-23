package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// runCLI is a small test helper: it runs Run with dir injected via
// --workspace and fails the test if the exit code doesn't match want.
func runCLI(t *testing.T, dir string, want int, args ...string) (stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	full := append([]string{}, args...)
	full = append(full, "--workspace", dir)
	code := Run(full, &out, &errBuf, "dev", "unspecified")
	if code != want {
		t.Fatalf("Run(%v) exit = %d, want %d, stdout=%q stderr=%q", args, code, want, out.String(), errBuf.String())
	}
	return out.String(), errBuf.String()
}

func createCard(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"--silent", "create"}, args...)
	out, _ := runCLI(t, dir, ExitSuccess, full...)
	return strings.TrimSpace(out)
}

func TestCreateRequiredFieldsFailure(t *testing.T) {
	dir := initTestWorkspace(t)

	stdout, stderr := runCLI(t, dir, ExitUsage, "create", "--type", "bug", "fix it")
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "reproduction") || !strings.Contains(stderr, "acceptance") {
		t.Fatalf("stderr = %q, want mention of both missing fields", stderr)
	}
	if !strings.Contains(stderr, `--reproduce ""`) || !strings.Contains(stderr, `--acceptance ""`) {
		t.Fatalf("stderr = %q, want the bypass hint", stderr)
	}

	// Confirm nothing was written.
	out, _ := runCLI(t, dir, ExitSuccess, "--json", "list")
	var cards []CardSummaryResult
	if err := json.Unmarshal([]byte(out), &cards); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("cards = %+v, want none written", cards)
	}
}

func TestCreateReproduceEmptyStringAcknowledges(t *testing.T) {
	dir := initTestWorkspace(t)

	stdout, stderr := runCLI(t, dir, ExitSuccess, "--json", "create", "--type", "bug", "--reproduce", "", "--acceptance", "", "fix it")
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	var card CardResult
	if err := json.Unmarshal([]byte(stdout), &card); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", stdout, err)
	}
	if card.Title != "fix it" || card.Type != "bug" {
		t.Fatalf("card = %+v", card)
	}
}

func TestCreateSilentEmitsExactlyID(t *testing.T) {
	dir := initTestWorkspace(t)

	var out, errBuf bytes.Buffer
	code := Run([]string{"--workspace", dir, "--silent", "create", "--type", "chore", "a chore"}, &out, &errBuf, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	if errBuf.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", errBuf.String())
	}
	if !strings.HasSuffix(out.String(), "\n") || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("stdout = %q, want exactly one line", out.String())
	}
	id := strings.TrimSpace(out.String())
	if !strings.HasPrefix(id, "acme-") {
		t.Fatalf("id = %q, want acme- prefix", id)
	}
}

func TestCreatePositionalAndFlagTitleConflict(t *testing.T) {
	dir := initTestWorkspace(t)
	_, stderr := runCLI(t, dir, ExitUsage, "create", "--type", "chore", "--title", "flag title", "positional title")
	if !strings.Contains(stderr, "cannot combine") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestCreateFileVariant(t *testing.T) {
	dir := initTestWorkspace(t)
	f, err := os.CreateTemp(t.TempDir(), "acceptance-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("acceptance from file"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	id := createCard(t, dir, "--type", "task", "--acceptance-file", f.Name(), "task with file")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "show", id)
	var show ShowResult
	if err := json.Unmarshal([]byte(stdout), &show); err != nil {
		t.Fatal(err)
	}
	if show.Acceptance != "acceptance from file" {
		t.Fatalf("acceptance = %q", show.Acceptance)
	}
}

func TestCreateFileVariantRejectsInvalidUTF8(t *testing.T) {
	dir := initTestWorkspace(t)
	f, err := os.CreateTemp(t.TempDir(), "acceptance-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("bad \xff\xfe bytes")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, stderr := runCLI(t, dir, ExitUsage, "create", "--type", "task", "--acceptance-file", f.Name(), "task with bad file")
	if !strings.Contains(stderr, "UTF-8") {
		t.Fatalf("stderr = %q, want mention of UTF-8", stderr)
	}

	out, _ := runCLI(t, dir, ExitSuccess, "--json", "list")
	var cards []CardSummaryResult
	if err := json.Unmarshal([]byte(out), &cards); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("cards = %+v, want none written", cards)
	}
}

func TestCreateStdinRejectsInvalidUTF8(t *testing.T) {
	dir := initTestWorkspace(t)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("bad \xff\xfe bytes"))
	w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	_, stderr := runCLI(t, dir, ExitUsage, "create", "--type", "task", "--title", "t", "--stdin")
	if !strings.Contains(stderr, "UTF-8") {
		t.Fatalf("stderr = %q, want mention of UTF-8", stderr)
	}
}

func TestCreateStdinFillsSoleRequiredField(t *testing.T) {
	dir := initTestWorkspace(t)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("acceptance via stdin")
	w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	var out, errBuf bytes.Buffer
	code := Run([]string{"--workspace", dir, "--json", "create", "--type", "task", "--title", "t", "--stdin"}, &out, &errBuf, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	var card CardResult
	if err := json.Unmarshal(out.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	if card.Acceptance != "acceptance via stdin" {
		t.Fatalf("acceptance = %q", card.Acceptance)
	}
}

func TestCreateStdinAmbiguous(t *testing.T) {
	dir := initTestWorkspace(t)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("x")
	w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	_, stderr := runCLI(t, dir, ExitUsage, "create", "--type", "bug", "--title", "t", "--stdin")
	if !strings.Contains(stderr, "ambiguous") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestShowWorktreeAfterIdentityBlock(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "--worktree", "/does/not/exist", "chore with worktree")

	stdout, _ := runCLI(t, dir, ExitSuccess, "show", id)
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	// id, title, type, status, priority, worktree - in that exact order.
	wantPrefixes := []string{"id:", "title:", "type:", "status:", "priority:", "worktree:"}
	for i, want := range wantPrefixes {
		if !strings.HasPrefix(lines[i], want) {
			t.Fatalf("line %d = %q, want prefix %q (full output:\n%s)", i, lines[i], want, stdout)
		}
	}
	if !strings.Contains(lines[5], "not present locally") {
		t.Fatalf("worktree line = %q, want missing-worktree annotation", lines[5])
	}
}

func TestShowIncludesNotes(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "with notes")
	runCLI(t, dir, ExitSuccess, "note", id, "first note")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "show", id)
	var show ShowResult
	if err := json.Unmarshal([]byte(stdout), &show); err != nil {
		t.Fatal(err)
	}
	if len(show.Notes) != 1 || show.Notes[0].Body != "first note" {
		t.Fatalf("notes = %+v", show.Notes)
	}
}

// TestShowSanitizesControlCharsInTitle guards against terminal escape
// sequence injection: a card title is arbitrary text supplied by whoever
// creates the card, so a title containing raw control bytes (e.g. ESC)
// must not reach the human-readable renderer unsanitized, where it could
// manipulate the viewer's terminal.
func TestShowSanitizesControlCharsInTitle(t *testing.T) {
	dir := initTestWorkspace(t)
	evilTitle := "evil\x1b]0;PWNED\x07title"
	id := createCard(t, dir, "--type", "chore", evilTitle)

	stdout, _ := runCLI(t, dir, ExitSuccess, "show", id)
	if strings.Contains(stdout, "\x1b") || strings.Contains(stdout, "\x07") {
		t.Fatalf("show output contains raw control bytes: %q", stdout)
	}

	// JSON output is untouched: it carries the literal title, escaped by
	// encoding/json rather than replaced.
	jsonOut, _ := runCLI(t, dir, ExitSuccess, "--json", "show", id)
	var show ShowResult
	if err := json.Unmarshal([]byte(jsonOut), &show); err != nil {
		t.Fatal(err)
	}
	if show.Title != evilTitle {
		t.Fatalf("JSON title = %q, want unsanitized %q", show.Title, evilTitle)
	}
}

// TestShowExposesChildren guards against the regression fixed in bdd-ui9:
// `show` must surface a card's children (in both --json and human output),
// not just its parents.
func TestShowExposesChildren(t *testing.T) {
	dir := initTestWorkspace(t)
	parent := createCard(t, dir, "--type", "chore", "parent")
	child := createCard(t, dir, "--type", "chore", "--parent", parent, "child")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "show", parent)
	var show ShowResult
	if err := json.Unmarshal([]byte(stdout), &show); err != nil {
		t.Fatal(err)
	}
	if len(show.Children) != 1 || show.Children[0].ID != child {
		t.Fatalf("children = %+v, want [%s]", show.Children, child)
	}

	humanStdout, _ := runCLI(t, dir, ExitSuccess, "show", parent)
	if !strings.Contains(humanStdout, "children:") || !strings.Contains(humanStdout, child) {
		t.Fatalf("human show output = %q, want a children line referencing %s", humanStdout, child)
	}
}

func TestListDefaultExcludesDoneCards(t *testing.T) {
	dir := initTestWorkspace(t)
	open := createCard(t, dir, "--type", "chore", "open one")
	closed := createCard(t, dir, "--type", "chore", "closed one")
	runCLI(t, dir, ExitSuccess, "close", closed)

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "list")
	var cards []CardSummaryResult
	if err := json.Unmarshal([]byte(stdout), &cards); err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].ID != open {
		t.Fatalf("cards = %+v, want only %s", cards, open)
	}
}

func TestListUnknownFlagVsArgument(t *testing.T) {
	dir := initTestWorkspace(t)

	cases := []struct {
		arg  string
		want string
	}{
		{"--db", `unknown flag "--db"`},
		{"--db=/tmp/example.sqlite", `unknown flag "--db=/tmp/example.sqlite"`},
		{"bogus", `unknown argument "bogus"`},
	}
	for _, tc := range cases {
		_, stderr := runCLI(t, dir, ExitUsage, "list", tc.arg)
		if !strings.Contains(stderr, tc.want) {
			t.Fatalf("Run(list %s) stderr = %q, want it to contain %q", tc.arg, stderr, tc.want)
		}
	}
}

func TestSearchUnknownFlagVsArgument(t *testing.T) {
	dir := initTestWorkspace(t)

	cases := []struct {
		arg  string
		want string
	}{
		{"--db", `unknown flag "--db"`},
		{"--db=/tmp/example.sqlite", `unknown flag "--db=/tmp/example.sqlite"`},
	}
	for _, tc := range cases {
		_, stderr := runCLI(t, dir, ExitUsage, "search", tc.arg)
		if !strings.Contains(stderr, tc.want) {
			t.Fatalf("Run(search %s) stderr = %q, want it to contain %q", tc.arg, stderr, tc.want)
		}
	}

	// A second, trailing flag-shaped argument still hits the loop's own
	// default case (as opposed to the leading-positional query check).
	_, stderr := runCLI(t, dir, ExitUsage, "search", "query", "--db")
	if want := `unknown flag "--db"`; !strings.Contains(stderr, want) {
		t.Fatalf("Run(search query --db) stderr = %q, want it to contain %q", stderr, want)
	}
}

func TestSearchMatchesTitle(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "findable-xyz")
	createCard(t, dir, "--type", "chore", "other")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "search", "findable")
	var cards []CardSummaryResult
	if err := json.Unmarshal([]byte(stdout), &cards); err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].ID != id {
		t.Fatalf("cards = %+v, want only %s", cards, id)
	}
}

func TestReadyExplainListsUnfinishedParent(t *testing.T) {
	dir := initTestWorkspace(t)
	parent := createCard(t, dir, "--type", "chore", "parent")
	child := createCard(t, dir, "--type", "chore", "--parent", parent, "child")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "ready", "--explain", child)
	var r ReadyExplainResult
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatal(err)
	}
	if r.Ready {
		t.Fatalf("r.Ready = true, want false")
	}
	found := false
	for _, reason := range r.Reasons {
		if strings.Contains(reason, parent) {
			found = true
		}
	}
	if !found {
		t.Fatalf("reasons = %v, want a reason mentioning unfinished parent %s", r.Reasons, parent)
	}
}

func TestReadyOnlyIncludesDispatchableCards(t *testing.T) {
	dir := initTestWorkspace(t)
	parent := createCard(t, dir, "--type", "chore", "parent")
	child := createCard(t, dir, "--type", "chore", "--parent", parent, "child")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "ready")
	var cards []CardSummaryResult
	if err := json.Unmarshal([]byte(stdout), &cards); err != nil {
		t.Fatal(err)
	}
	for _, c := range cards {
		if c.ID == child {
			t.Fatalf("ready list includes blocked child %s", child)
		}
	}
	if len(cards) != 1 || cards[0].ID != parent {
		t.Fatalf("cards = %+v, want only parent %s ready", cards, parent)
	}
}

func TestUpdateMultiAddParentAtomicOnInvalidID(t *testing.T) {
	dir := initTestWorkspace(t)
	a := createCard(t, dir, "--type", "chore", "a")
	b := createCard(t, dir, "--type", "chore", "b")
	c := createCard(t, dir, "--type", "chore", "c")

	_, stderr := runCLI(t, dir, ExitNotFound, "update", c, "--add-parent", a, "--add-parent", "does-not-exist", "--add-parent", b)
	if stderr == "" {
		t.Fatalf("expected stderr diagnostic")
	}

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "show", c)
	var show ShowResult
	if err := json.Unmarshal([]byte(stdout), &show); err != nil {
		t.Fatal(err)
	}
	if len(show.Parents) != 0 {
		t.Fatalf("parents = %+v, want none added (atomic failure)", show.Parents)
	}
}

func TestReadyExplainRespectsLimit(t *testing.T) {
	dir := initTestWorkspace(t)
	createCard(t, dir, "--type", "chore", "a")
	createCard(t, dir, "--type", "chore", "b")
	createCard(t, dir, "--type", "chore", "c")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "ready", "--explain", "--limit", "2")
	var results []ReadyExplainResult
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want exactly 2 (limit not applied)", results)
	}
}

func TestUpdateClaim(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "claim me")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "update", id, "--claim")
	var card CardResult
	if err := json.Unmarshal([]byte(stdout), &card); err != nil {
		t.Fatal(err)
	}
	if card.Status != "in_progress" || card.Assignee == "" {
		t.Fatalf("card = %+v, want claimed", card)
	}
}

func TestUpdateStatusAndFieldsTogether(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "status update")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "update", id, "--priority", "P0", "--worktree", "/tmp/wt")
	var card CardResult
	if err := json.Unmarshal([]byte(stdout), &card); err != nil {
		t.Fatal(err)
	}
	if card.Priority != 0 || card.Worktree != "/tmp/wt" {
		t.Fatalf("card = %+v", card)
	}
}

func TestUpdateClaimWithInvalidFlagDoesNotCommitClaim(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "claim-then-fail")

	// A malformed --priority must be rejected before --claim's mutation is
	// committed, so a single failed `update` invocation leaves nothing
	// half-applied.
	runCLI(t, dir, ExitUsage, "update", id, "--claim", "--priority", "not-a-number")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "show", id)
	var show ShowResult
	if err := json.Unmarshal([]byte(stdout), &show); err != nil {
		t.Fatal(err)
	}
	if show.Status != "open" || show.Assignee != "" {
		t.Fatalf("show = %+v, want unclaimed (update should not have partially applied)", show)
	}
}

func TestUpdateNoFlagsIsUsageError(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "no-op")
	runCLI(t, dir, ExitUsage, "update", id)
}

func TestUpdateClearWorktree(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "--worktree", "/tmp/wt", "clear wt")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "update", id, "--clear-worktree")
	var card CardResult
	if err := json.Unmarshal([]byte(stdout), &card); err != nil {
		t.Fatal(err)
	}
	if card.Worktree != "" {
		t.Fatalf("worktree = %q, want cleared", card.Worktree)
	}
}

func TestNoteStdin(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "notable")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("note from stdin")
	w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	var out, errBuf bytes.Buffer
	code := Run([]string{"--workspace", dir, "--json", "note", id, "--stdin"}, &out, &errBuf, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	var note NoteResult
	if err := json.Unmarshal(out.Bytes(), &note); err != nil {
		t.Fatal(err)
	}
	if note.Body != "note from stdin" {
		t.Fatalf("note = %+v", note)
	}
}

func TestNoteStdinRejectsInvalidUTF8(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "notable")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("bad \xff\xfe bytes"))
	w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	_, stderr := runCLI(t, dir, ExitUsage, "note", id, "--stdin")
	if !strings.Contains(stderr, "UTF-8") {
		t.Fatalf("stderr = %q, want mention of UTF-8", stderr)
	}
}

func TestCloseReopenLifecycle(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "close me")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "close", id, "wrapping up")
	var closed CardResult
	if err := json.Unmarshal([]byte(stdout), &closed); err != nil {
		t.Fatal(err)
	}
	if closed.Status != "closed" {
		t.Fatalf("status = %q, want closed", closed.Status)
	}

	stdout, _ = runCLI(t, dir, ExitSuccess, "--json", "reopen", id)
	var reopened CardResult
	if err := json.Unmarshal([]byte(stdout), &reopened); err != nil {
		t.Fatal(err)
	}
	if reopened.Status != "open" {
		t.Fatalf("status = %q, want open", reopened.Status)
	}
}

func TestDeferWithUntil(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "defer me")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "defer", id, "--until", "2099-01-01T00:00:00Z")
	var card CardResult
	if err := json.Unmarshal([]byte(stdout), &card); err != nil {
		t.Fatal(err)
	}
	if card.Status != "deferred" {
		t.Fatalf("status = %q, want deferred", card.Status)
	}
	if card.Priority < 0 {
		t.Fatal("unreachable")
	}
}

func TestHumanLabelExcludesFromReady(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "needs human")

	runCLI(t, dir, ExitSuccess, "human", id, "please check")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "ready")
	var cards []CardSummaryResult
	if err := json.Unmarshal([]byte(stdout), &cards); err != nil {
		t.Fatal(err)
	}
	for _, c := range cards {
		if c.ID == id {
			t.Fatalf("ready list includes human-flagged card %s", id)
		}
	}
}

func TestParentsAndChildren(t *testing.T) {
	dir := initTestWorkspace(t)
	parent := createCard(t, dir, "--type", "chore", "parent")
	child := createCard(t, dir, "--type", "chore", "--parent", parent, "child")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "parents", child)
	var parents []CardRefResult
	if err := json.Unmarshal([]byte(stdout), &parents); err != nil {
		t.Fatal(err)
	}
	if len(parents) != 1 || parents[0].ID != parent {
		t.Fatalf("parents = %+v", parents)
	}

	stdout, _ = runCLI(t, dir, ExitSuccess, "--json", "children", parent)
	var children []CardRefResult
	if err := json.Unmarshal([]byte(stdout), &children); err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].ID != child {
		t.Fatalf("children = %+v", children)
	}
}

func TestLabelAddRemoveList(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "labelled")

	runCLI(t, dir, ExitSuccess, "label", "add", id, "urgent")
	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "label", "list", id)
	var labels []string
	if err := json.Unmarshal([]byte(stdout), &labels); err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || labels[0] != "urgent" {
		t.Fatalf("labels = %v", labels)
	}

	runCLI(t, dir, ExitSuccess, "label", "remove", id, "urgent")
	stdout, _ = runCLI(t, dir, ExitSuccess, "--json", "label", "list", id)
	if err := json.Unmarshal([]byte(stdout), &labels); err != nil {
		t.Fatal(err)
	}
	if len(labels) != 0 {
		t.Fatalf("labels = %v, want none", labels)
	}
}

func TestDeleteWithoutForceRefuses(t *testing.T) {
	dir := initTestWorkspace(t)
	id := createCard(t, dir, "--type", "chore", "keep me")

	runCLI(t, dir, ExitUsage, "delete", id)

	// Confirm the card is untouched.
	runCLI(t, dir, ExitSuccess, "show", id)
}

func TestDeleteForceRemovesCardAndReportsEdges(t *testing.T) {
	dir := initTestWorkspace(t)
	parent := createCard(t, dir, "--type", "chore", "parent")
	child := createCard(t, dir, "--type", "chore", "--parent", parent, "child")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "delete", child, "--force")
	var result DeleteResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.RemovedParents) != 1 || result.RemovedParents[0] != parent {
		t.Fatalf("removed parents = %v, want [%s]", result.RemovedParents, parent)
	}

	runCLI(t, dir, ExitNotFound, "show", child)
}

func TestDeleteMissingIDIsNotFound(t *testing.T) {
	dir := initTestWorkspace(t)
	runCLI(t, dir, ExitNotFound, "delete", "does-not-exist", "--force")
}

func TestShowMissingCardIsNotFound(t *testing.T) {
	dir := initTestWorkspace(t)
	runCLI(t, dir, ExitNotFound, "show", "does-not-exist")
}

func TestCreateLabelsAndParentsRepeatable(t *testing.T) {
	dir := initTestWorkspace(t)
	p1 := createCard(t, dir, "--type", "chore", "p1")
	p2 := createCard(t, dir, "--type", "chore", "p2")

	id := createCard(t, dir, "--type", "chore", "--label", "a", "--label", "b", "--parent", p1, "--parent", p2, "multi")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "show", id)
	var show ShowResult
	if err := json.Unmarshal([]byte(stdout), &show); err != nil {
		t.Fatal(err)
	}
	if len(show.Labels) != 2 || len(show.Parents) != 2 {
		t.Fatalf("show = %+v", show)
	}
}

func TestListLabelFiltersAndCombine(t *testing.T) {
	dir := initTestWorkspace(t)
	both := createCard(t, dir, "--type", "chore", "--label", "a", "--label", "b", "both")
	createCard(t, dir, "--type", "chore", "--label", "a", "only-a")

	stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "list", "--label", "a", "--label", "b")
	var cards []CardSummaryResult
	if err := json.Unmarshal([]byte(stdout), &cards); err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].ID != both {
		t.Fatalf("cards = %+v, want only %s", cards, both)
	}
}

// TestCardCommandsRejectRemovedDBFlag covers every card-scoped command that
// takes a leading positional (id, or id+label): the removed global --db
// flag, appearing before the subcommand, must be identified as an unknown
// flag rather than swallowed as the id/label or misreported through a
// wrong-arity error.
func TestCardCommandsRejectRemovedDBFlag(t *testing.T) {
	dir := initTestWorkspace(t)

	cases := []struct {
		name    string
		args    []string
		wantCmd string
	}{
		{"show", []string{"show", "bdd-1"}, "show"},
		{"note", []string{"note", "bdd-1", "hi"}, "note"},
		{"close", []string{"close", "bdd-1"}, "close"},
		{"reopen", []string{"reopen", "bdd-1"}, "reopen"},
		{"defer", []string{"defer", "bdd-1"}, "defer"},
		{"human", []string{"human", "bdd-1"}, "human"},
		{"update", []string{"update", "bdd-1", "--claim"}, "update"},
		{"parents", []string{"parents", "bdd-1"}, "parents"},
		{"children", []string{"children", "bdd-1"}, "children"},
		{"label-add", []string{"label", "add", "bdd-1", "x"}, "label add"},
		{"label-remove", []string{"label", "remove", "bdd-1", "x"}, "label remove"},
		{"label-list", []string{"label", "list", "bdd-1"}, "label list"},
	}
	dbForms := []struct {
		lead []string // tokens preceding the subcommand
		want string   // flag token the error message must name
	}{
		{[]string{"--db", "/tmp/example.sqlite"}, "--db"},
		{[]string{"--db=/tmp/example.sqlite"}, "--db=/tmp/example.sqlite"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, form := range dbForms {
				args := append(append([]string{}, form.lead...), tc.args...)
				_, stderr := runCLI(t, dir, ExitUsage, args...)
				want := `bdd: ` + tc.wantCmd + `: unknown flag "` + form.want + `"`
				if !strings.Contains(stderr, want) {
					t.Fatalf("Run(%v) stderr = %q, want it to contain %q", args, stderr, want)
				}
			}
		})
	}
}

// TestCardOptionalTrailingFieldsRejectRemovedDBFlag covers commands whose
// removed --db exposure isn't in the leading id but in an optional
// free-form trailing positional (close/human's [reason]): --db there must
// still be rejected as an unknown flag, not accepted as literal reason
// text that lets the command mutate the card (bd bdd-md64).
func TestCardOptionalTrailingFieldsRejectRemovedDBFlag(t *testing.T) {
	dir := initTestWorkspace(t)

	cases := []struct {
		name    string
		cmd     string
		wantCmd string
	}{
		{"close", "close", "close"},
		{"human", "human", "human"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := createCard(t, dir, "--type", "chore", "trailing-db-"+tc.name)

			_, stderr := runCLI(t, dir, ExitUsage, tc.cmd, id, "--db")
			want := `bdd: ` + tc.wantCmd + `: unknown flag "--db"`
			if !strings.Contains(stderr, want) {
				t.Fatalf("Run(%s %s --db) stderr = %q, want it to contain %q", tc.cmd, id, stderr, want)
			}

			// The rejected command must not have mutated the card: it
			// should still be open, and not flagged human, with --db
			// nowhere in its state (reason, notes, or labels).
			stdout, _ := runCLI(t, dir, ExitSuccess, "--json", "show", id)
			var show ShowResult
			if err := json.Unmarshal([]byte(stdout), &show); err != nil {
				t.Fatal(err)
			}
			if show.Status != "open" {
				t.Fatalf("card %s status = %q after rejected %s --db, want %q (no mutation)", id, show.Status, tc.cmd, "open")
			}
			for _, l := range show.Labels {
				if l == "human" {
					t.Fatalf("card %s has human label after rejected %s --db, want no mutation", id, tc.cmd)
				}
			}
		})
	}
}

func TestListUnknownStatusExitsUsageWithChoices(t *testing.T) {
	dir := initTestWorkspace(t)
	createCard(t, dir, "--type", "chore", "a chore")

	_, stderr := runCLI(t, dir, ExitUsage, "list", "--status", "opne")
	if !strings.Contains(stderr, `"opne"`) || !strings.Contains(stderr, "open") {
		t.Fatalf("stderr = %q, want the bad value and valid choices", stderr)
	}
}

func TestListUnknownStatusCategoryExitsUsage(t *testing.T) {
	dir := initTestWorkspace(t)
	_, stderr := runCLI(t, dir, ExitUsage, "list", "--status-category", "actve")
	if !strings.Contains(stderr, `"actve"`) {
		t.Fatalf("stderr = %q, want the bad value", stderr)
	}
}

func TestListUnknownTypeExitsUsage(t *testing.T) {
	dir := initTestWorkspace(t)
	_, stderr := runCLI(t, dir, ExitUsage, "list", "--type", "tesk")
	if !strings.Contains(stderr, `"tesk"`) {
		t.Fatalf("stderr = %q, want the bad value", stderr)
	}
}

func TestListAllIncludesDoneCards(t *testing.T) {
	dir := initTestWorkspace(t)
	openID := createCard(t, dir, "--type", "chore", "stays open")
	closedID := createCard(t, dir, "--type", "chore", "gets closed")
	runCLI(t, dir, ExitSuccess, "close", closedID)

	out, _ := runCLI(t, dir, ExitSuccess, "--json", "list", "--all")
	var cards []CardSummaryResult
	if err := json.Unmarshal([]byte(out), &cards); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range cards {
		ids = append(ids, c.ID)
	}
	if !containsStr(ids, openID) || !containsStr(ids, closedID) {
		t.Fatalf("list --all = %v, want to contain both %s and %s", ids, openID, closedID)
	}

	withoutAll, _ := runCLI(t, dir, ExitSuccess, "--json", "list")
	var withoutAllCards []CardSummaryResult
	if err := json.Unmarshal([]byte(withoutAll), &withoutAllCards); err != nil {
		t.Fatal(err)
	}
	for _, c := range withoutAllCards {
		if c.ID == closedID {
			t.Fatalf("list (no --all) = %+v, want to exclude closed card %s", withoutAllCards, closedID)
		}
	}
}

func containsStr(items []string, s string) bool {
	for _, it := range items {
		if it == s {
			return true
		}
	}
	return false
}
