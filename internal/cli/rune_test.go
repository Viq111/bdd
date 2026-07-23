package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuneSetGetRoundTrip(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "set", "--workspace", dir, "role/programmer",
		"--kind", "role", "--title", "Programmer", "--body", "Implement things."}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune set) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "get", "--workspace", dir, "--json", "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune get) exit = %d, stderr = %q", code, stderr.String())
	}
	var r RuneResult
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if r.Key != "role/programmer" || r.Kind != "role" || r.Title != "Programmer" || r.Body != "Implement things." {
		t.Fatalf("r = %+v, unexpected", r)
	}
	if !r.Enabled {
		t.Fatal("r.Enabled = false, want true (default)")
	}
	if r.Revision != 1 {
		t.Fatalf("r.Revision = %d, want 1", r.Revision)
	}
}

func TestRuneSetBodyFile(t *testing.T) {
	dir := initTestWorkspace(t)
	bodyPath := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyPath, []byte("file body"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "set", "--workspace", dir, "policy/conventions",
		"--kind", "policy", "--title", "Conventions", "--body-file", bodyPath}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune set --body-file) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "get", "--workspace", dir, "--json", "policy/conventions"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune get) exit = %d, stderr = %q", code, stderr.String())
	}
	var r RuneResult
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if r.Body != "file body" {
		t.Fatalf("r.Body = %q, want %q", r.Body, "file body")
	}
}

func TestRuneListAndSearch(t *testing.T) {
	dir := initTestWorkspace(t)

	put := func(key, kind, title, body string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		args := []string{"rune", "set", "--workspace", dir, key, "--kind", kind, "--title", title, "--body", body}
		if code := Run(args, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
			t.Fatalf("Run(rune set %s) exit = %d, stderr = %q", key, code, stderr.String())
		}
	}
	put("role/programmer", "role", "Programmer", "Go implementation work.")
	put("role/qa", "role", "QA", "Reviews changes.")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "list", "--workspace", dir, "--json", "--kind", "role"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune list) exit = %d, stderr = %q", code, stderr.String())
	}
	var summaries []RuneSummaryResult
	if err := json.Unmarshal(stdout.Bytes(), &summaries); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(summaries) != 2 {
		t.Fatalf("len(summaries) = %d, want 2", len(summaries))
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "search", "--workspace", dir, "--json", "Go implementation"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune search) exit = %d, stderr = %q", code, stderr.String())
	}
	var found []RuneSummaryResult
	if err := json.Unmarshal(stdout.Bytes(), &found); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(found) != 1 || found[0].Key != "role/programmer" {
		t.Fatalf("found = %+v, want exactly [role/programmer]", found)
	}
}

func TestRuneSearchUnknownFlagVsArgument(t *testing.T) {
	dir := initTestWorkspace(t)

	cases := []struct {
		arg  string
		want string
	}{
		{"--db", `unknown flag "--db"`},
		{"--db=/tmp/example.sqlite", `unknown flag "--db=/tmp/example.sqlite"`},
	}
	for _, tc := range cases {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"rune", "search", "--workspace", dir, tc.arg}, &stdout, &stderr, "dev", "unspecified")
		if code != ExitUsage {
			t.Fatalf("Run(rune search %s) exit = %d, want %d", tc.arg, code, ExitUsage)
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Fatalf("Run(rune search %s) stderr = %q, want it to contain %q", tc.arg, stderr.String(), tc.want)
		}
	}
}

// TestRuneCommandsRejectRemovedDBFlag covers every rune command that takes
// a leading positional key: the removed global --db flag must be
// identified as an unknown flag rather than swallowed as the key or
// misreported through a wrong-arity error.
func TestRuneCommandsRejectRemovedDBFlag(t *testing.T) {
	dir := initTestWorkspace(t)

	cases := []struct {
		name    string
		args    []string
		wantCmd string
	}{
		{"get", []string{"rune", "get", "somekey"}, "rune get"},
		{"set", []string{"rune", "set", "somekey"}, "rune set"},
		{"enable", []string{"rune", "enable", "somekey"}, "rune enable"},
		{"disable", []string{"rune", "disable", "somekey"}, "rune disable"},
		{"remove", []string{"rune", "remove", "somekey"}, "rune remove"},
	}
	dbForms := []struct {
		lead []string
		want string
	}{
		{[]string{"--db", "/tmp/example.sqlite"}, "--db"},
		{[]string{"--db=/tmp/example.sqlite"}, "--db=/tmp/example.sqlite"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, form := range dbForms {
				var stdout, stderr bytes.Buffer
				args := append(append([]string{}, form.lead...), tc.args...)
				args = append(args, "--workspace", dir)
				code := Run(args, &stdout, &stderr, "dev", "unspecified")
				if code != ExitUsage {
					t.Fatalf("Run(%v) exit = %d, want %d, stderr=%q", args, code, ExitUsage, stderr.String())
				}
				want := `bdd: ` + tc.wantCmd + `: unknown flag "` + form.want + `"`
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("Run(%v) stderr = %q, want it to contain %q", args, stderr.String(), want)
				}
			}
		})
	}
}

func TestRuneEnableDisable(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rune", "set", "--workspace", dir, "role/programmer", "--kind", "role", "--title", "P", "--body", "b"}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(rune set) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"rune", "disable", "--workspace", dir, "--json", "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune disable) exit = %d, stderr = %q", code, stderr.String())
	}
	var r RuneResult
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if r.Enabled {
		t.Fatal("r.Enabled = true after disable, want false")
	}

	// Disabled runes are excluded from list without --all.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "list", "--workspace", dir, "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune list) exit = %d, stderr = %q", code, stderr.String())
	}
	var summaries []RuneSummaryResult
	if err := json.Unmarshal(stdout.Bytes(), &summaries); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(summaries) != 0 {
		t.Fatalf("len(summaries) = %d, want 0 (disabled rune hidden by default)", len(summaries))
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "list", "--workspace", dir, "--json", "--all"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune list --all) exit = %d, stderr = %q", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &summaries); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(summaries) != 1 {
		t.Fatalf("len(summaries) = %d, want 1 with --all", len(summaries))
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "enable", "--workspace", dir, "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune enable) exit = %d, stderr = %q", code, stderr.String())
	}
}

func TestRuneRemove(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rune", "set", "--workspace", dir, "role/programmer", "--kind", "role", "--title", "P", "--body", "b"}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(rune set) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"rune", "remove", "--workspace", dir, "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune remove) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "get", "--workspace", dir, "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitNotFound {
		t.Fatalf("Run(rune get) after remove exit = %d, want %d", code, ExitNotFound)
	}
}

func TestRuneProtectedRequiresForce(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "set", "--workspace", dir, "role/programmer",
		"--kind", "role", "--title", "P", "--body", "b", "--protected"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune set --protected) exit = %d, stderr = %q", code, stderr.String())
	}

	// Update without --force must fail with ExitConflict (4).
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "set", "--workspace", dir, "role/programmer",
		"--kind", "role", "--body", "changed"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitConflict {
		t.Fatalf("Run(rune set, protected, no force) exit = %d, want %d, stderr = %q", code, ExitConflict, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty on failure", stdout.String())
	}

	// Disable without --force must also fail with ExitConflict.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "disable", "--workspace", dir, "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitConflict {
		t.Fatalf("Run(rune disable, protected, no force) exit = %d, want %d", code, ExitConflict)
	}

	// Remove without --force must also fail with ExitConflict.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "remove", "--workspace", dir, "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitConflict {
		t.Fatalf("Run(rune remove, protected, no force) exit = %d, want %d", code, ExitConflict)
	}

	// With --force, the update succeeds.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "set", "--workspace", dir, "role/programmer",
		"--kind", "role", "--body", "changed", "--force"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune set, protected, --force) exit = %d, stderr = %q", code, stderr.String())
	}
}

func TestRuneExportIsUnknownSubcommand(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rune", "set", "--workspace", dir, "role/programmer", "--kind", "role", "--title", "Programmer", "--body", "Body text"}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(rune set) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"rune", "export", "--workspace", dir, "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(rune export) exit = %d, want %d (unknown subcommand), stderr = %q", code, ExitUsage, stderr.String())
	}
}

func TestRuneGetMissingIsNotFound(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "get", "--workspace", dir, "role/nope"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitNotFound {
		t.Fatalf("Run(rune get) exit = %d, want %d", code, ExitNotFound)
	}
	if !strings.HasPrefix(stderr.String(), "bdd: rune get:") {
		t.Fatalf("stderr = %q, want prefix %q", stderr.String(), "bdd: rune get:")
	}
	if strings.Contains(stderr.String(), "rune show") {
		t.Fatalf("stderr = %q, must not reference retired %q command", stderr.String(), "rune show")
	}
}

// TestRuneSetGetErrorPrefixes is a regression test for bd bdd-jlte: handler
// errors for `rune set`/`rune get` must identify themselves by their current
// names, not the retired `rune put`/`rune show` names.
func TestRuneSetGetErrorPrefixes(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "set", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(rune set, no key) exit = %d, want %d", code, ExitUsage)
	}
	if got, want := stderr.String(), "bdd: rune set: key is required\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "get", "--workspace", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(rune get, no key) exit = %d, want %d", code, ExitUsage)
	}
	if got, want := stderr.String(), "bdd: rune get: expected exactly one key argument\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

// TestRuneSetFlagsBeforeKey is a regression test for bd bdd-j2us: `rune set`
// treated args[0] as the key unconditionally, so any flag placed before the
// positional key was misparsed as the key itself.
func TestRuneSetFlagsBeforeKey(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "set", "--workspace", dir,
		"--kind", "role", "--title", "Programmer", "--body", "Implement things.", "role/programmer"},
		&stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune set, flags before key) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "get", "--workspace", dir, "--json", "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune get) exit = %d, stderr = %q", code, stderr.String())
	}
	var r RuneResult
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if r.Key != "role/programmer" || r.Kind != "role" || r.Title != "Programmer" || r.Body != "Implement things." {
		t.Fatalf("r = %+v, unexpected", r)
	}
}

// TestRuneSearchFlagsBeforeText is a regression test for bd bdd-j2us: `rune
// search` treated args[0] as the search text unconditionally, so any flag
// placed before the positional text was misparsed as the text itself.
func TestRuneSearchFlagsBeforeText(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rune", "set", "--workspace", dir, "role/programmer",
		"--kind", "role", "--title", "Programmer", "--body", "Implement things."}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(rune set) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"rune", "search", "--workspace", dir, "--kind", "role", "programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune search, flags before text) exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "role/programmer") {
		t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), "role/programmer")
	}
}

// TestRuneSetKeyInterleavedWithFlags is a regression test for bd bdd-j2us:
// the key may appear between flags, not just before or after all of them.
func TestRuneSetKeyInterleavedWithFlags(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "set", "--workspace", dir,
		"--kind", "role", "role/programmer", "--title", "Programmer", "--body", "Implement things."},
		&stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune set, key interleaved) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"rune", "get", "--workspace", dir, "--json", "role/programmer"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune get) exit = %d, stderr = %q", code, stderr.String())
	}
	var r RuneResult
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if r.Key != "role/programmer" || r.Kind != "role" || r.Title != "Programmer" {
		t.Fatalf("r = %+v, unexpected", r)
	}
}

// TestRuneSetRejectsDuplicatePositional ensures a second bare positional
// argument is rejected as an unexpected argument rather than silently
// overwriting or ignoring the key.
func TestRuneSetRejectsDuplicatePositional(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "set", "--workspace", dir, "role/programmer", "role/qa",
		"--kind", "role", "--title", "P", "--body", "b"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(rune set, two positionals) exit = %d, want %d, stderr = %q", code, ExitUsage, stderr.String())
	}
	if !strings.Contains(stderr.String(), `unexpected argument "role/qa"`) {
		t.Fatalf("stderr = %q, want it to mention the duplicate positional", stderr.String())
	}
}

// TestRuneSearchRejectsDuplicatePositional mirrors
// TestRuneSetRejectsDuplicatePositional for `rune search`.
func TestRuneSearchRejectsDuplicatePositional(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "search", "--workspace", dir, "foo", "bar"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(rune search, two positionals) exit = %d, want %d, stderr = %q", code, ExitUsage, stderr.String())
	}
	if !strings.Contains(stderr.String(), `unexpected argument "bar"`) {
		t.Fatalf("stderr = %q, want it to mention the duplicate positional", stderr.String())
	}
}

// TestRuneSearchRejectsUnknownFlag ensures an unrecognized flag-shaped token
// is rejected rather than silently accepted or misparsed as search text.
func TestRuneSearchRejectsUnknownFlag(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "search", "--workspace", dir, "--bogus", "foo"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(rune search, unknown flag) exit = %d, want %d, stderr = %q", code, ExitUsage, stderr.String())
	}
	if !strings.Contains(stderr.String(), `unknown flag "--bogus"`) {
		t.Fatalf("stderr = %q, want it to mention the unknown flag", stderr.String())
	}
}

func TestRuneSetCreateOnlyRejectsExisting(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rune", "set", "--workspace", dir, "role/programmer", "--kind", "role", "--title", "P", "--body", "b"}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(rune set) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"rune", "set", "--workspace", dir, "role/programmer", "--kind", "role", "--body", "b2", "--create-only"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitConflict {
		t.Fatalf("Run(rune set --create-only) exit = %d, want %d", code, ExitConflict)
	}
}

// TestRuneSetExecutablePathWithGlobalFlagFirst is a regression test for bd
// bdd-9b9h: QA's reproduction placed the global --workspace flag before the
// subcommand (as `bdd --workspace <dir> rune set ...`, the documented
// invocation shape), rather than after it like the rest of this file's
// tests. That shape reaches the same Run -> cobra dispatch path, so this
// pins it directly instead of only ever exercising --workspace after the
// subcommand name. It also exercises the exact key/kind pairing documented
// in the `rune set` help example (cobra_tree.go), so a future edit that
// makes that example violate the "<kind>/<name>" key grammar fails loudly
// here instead of only surfacing when a user copies it.
func TestRuneSetExecutablePathWithGlobalFlagFirst(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "rune", "set", "doc/review-checklist",
		"--kind", "doc", "--title", "Review checklist", "--body", "...", "--prime", "required"},
		&stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune set, global flag before subcommand) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "rune", "get", "--json", "doc/review-checklist"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(rune get, global flag before subcommand) exit = %d, stderr = %q", code, stderr.String())
	}
	var r RuneResult
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if r.Key != "doc/review-checklist" || r.Kind != "doc" || r.Prime != "required" {
		t.Fatalf("r = %+v, unexpected", r)
	}
}

// TestRuneSetRejectsMalformedKeyWithClearMessage is a regression test for bd
// bdd-9b9h: a rune key without a "<kind>/<name>" separator produced "missing
// required field(s): key", which reads as though the key was omitted when
// it was actually present but malformed. That misleading wording is what
// caused QA's reproduction (a key with no "/") to be misdiagnosed as a
// broken Cobra dispatch. This pins the corrected, self-explanatory message.
func TestRuneSetRejectsMalformedKeyWithClearMessage(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"rune", "set", "--workspace", dir, "required-rune",
		"--kind", "instruction", "--title", "Required rune", "--body", "read this fully"},
		&stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(rune set, malformed key) exit = %d, stderr = %q", code, stderr.String())
	}
	if got, want := stderr.String(), `bdd: rune set: bdd: invalid argument: key "required-rune" must have the form "<kind>/<name>" (each segment lowercase, starting with a letter, e.g. "doc/review-checklist")`+"\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}
