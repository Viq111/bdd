// Package migration_test is QA's black-box, subprocess-level verification of
// the bd -> bdd migration tool. This file intentionally uses only
// subprocesses and the public bd/bdd command surfaces. It is the migration
// QA harness, rather than a unit-test seam for tools/migrate.
package migration_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

var bddBinary, migrationBinary string

// TestMain skips before building anything when -short is set: the entire
// migration package is slow (bd-dependent, ~186s), unlike the per-test
// testing.Short() skips used elsewhere in this codebase (see snapshot_test.go),
// so the fast lane should not even pay for the two go build invocations.
func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		// Skip the whole package before paying for the two go build
		// invocations below: every test here is bd-dependent and slow.
		fmt.Println("SKIP migration tests: skipped in -short mode")
		os.Exit(0)
	}

	dir, err := os.MkdirTemp("", "bdd-migration-e2e-bin-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	bddBinary = filepath.Join(dir, "bdd")
	build := exec.Command("go", "build", "-o", bddBinary, "../../cmd/bdd")
	if out, err := build.CombinedOutput(); err != nil {
		panic("build bdd: " + err.Error() + "\n" + string(out))
	}
	migrationBinary = filepath.Join(dir, "bdd-migration")
	build = exec.Command("go", "build", "-o", migrationBinary, "../../tools/migrate/cmd/bdd-migration")
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

const expectedRoleWarnings = "warning: mig-role: skipped dependency kind \"related\" to mig-related; skipped dependency to mig-blocker because role is imported as a rune; skipped role-attached comments because role is imported as a rune\n"

func migrationBD(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("bd")
	if err != nil {
		t.Skip("bd is required for migration end-to-end tests: " + err.Error())
	}
	return path
}

func runCommand(t *testing.T, dir, binary string, args ...string) result {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			t.Fatalf("run %s %v: %v", binary, args, err)
		}
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

// seedMigrationTemplate and seedSingleRoleTemplate build each seeded
// workspace's on-disk fixture exactly once per package run, the first time
// any test needs it, via sync.OnceValues. Every test then hands itself a
// fresh copy of the cached template in its own t.TempDir() rather than
// re-running the same ~13 bd subprocess calls. The template directories
// themselves are never written to after they are built; only copies are
// ever mutated.
var (
	seedMigrationTemplate  = sync.OnceValues(buildMigrationWorkspaceTemplate)
	seedSingleRoleTemplate = sync.OnceValues(buildSingleRoleWorkspaceTemplate)
)

func runInDir(dir, binary string, args ...string) error {
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %w: %s", binary, args, err, out)
	}
	return nil
}

func buildMigrationWorkspaceTemplate() (string, error) {
	bd, err := exec.LookPath("bd")
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "bdd-migration-seed-template-")
	if err != nil {
		return "", err
	}
	for _, args := range [][]string{
		{"init", "--non-interactive", "--quiet", "--prefix", "mig", "--skip-agents", "--skip-hooks"},
		{"config", "set", "status.custom", "awaiting_review"},
		{"config", "set", "types.custom", "role,incident"},
		{"create", "--id", "mig-blocker", "--title", "Blocker", "--description", "blocker description", "--design", "blocker design", "--acceptance", "blocker acceptance", "--labels", "source,unicode:日本語", "--silent"},
		{"comment", "mig-blocker", "blocker comment"},
		{"create", "--id", "mig-related", "--title", "Related", "--silent"},
		{"dep", "add", "mig-related", "mig-blocker", "--type", "blocks"},
		{"create", "--id", "mig-custom", "--title", "Custom", "--type", "incident", "--silent"},
		{"update", "mig-custom", "--status", "awaiting_review"},
		{"create", "--id", "mig-role", "--title", "[role] Operator", "--type", "role", "--description", "role body", "--labels", "runtime:codex,seeded", "--silent"},
		{"comment", "mig-role", "first comment"},
		{"dep", "add", "mig-role", "mig-blocker", "--type", "blocks"},
		{"dep", "add", "mig-role", "mig-related", "--type", "related"},
		{"remember", "seed memory", "--key", "migration/seed"},
	} {
		if err := runInDir(dir, bd, args...); err != nil {
			return "", fmt.Errorf("seed migration workspace template: %w", err)
		}
	}
	return dir, nil
}

func buildSingleRoleWorkspaceTemplate() (string, error) {
	bd, err := exec.LookPath("bd")
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "bdd-migration-seed-single-role-template-")
	if err != nil {
		return "", err
	}
	for _, args := range [][]string{
		{"init", "--non-interactive", "--quiet", "--prefix", "mig", "--skip-agents", "--skip-hooks"},
		{"config", "set", "status.custom", "awaiting_review"},
		{"config", "set", "types.custom", "role"},
		{"create", "--id", "mig-role", "--title", "[role] Operator", "--type", "role", "--description", "role body", "--silent"},
	} {
		if err := runInDir(dir, bd, args...); err != nil {
			return "", fmt.Errorf("seed single-role workspace template: %w", err)
		}
	}
	return dir, nil
}

func seedMigrationWorkspace(t *testing.T) string {
	t.Helper()
	migrationBD(t) // skip if bd is not on PATH, before touching the cached template
	template, err := seedMigrationTemplate()
	if err != nil {
		t.Fatalf("build migration workspace template: %v", err)
	}
	workspace := t.TempDir()
	if err := os.CopyFS(workspace, os.DirFS(template)); err != nil {
		t.Fatalf("copy migration workspace template: %v", err)
	}
	return workspace
}

func seedSingleRoleWorkspace(t *testing.T) string {
	t.Helper()
	migrationBD(t) // skip if bd is not on PATH, before touching the cached template
	template, err := seedSingleRoleTemplate()
	if err != nil {
		t.Fatalf("build single-role workspace template: %v", err)
	}
	workspace := t.TempDir()
	if err := os.CopyFS(workspace, os.DirFS(template)); err != nil {
		t.Fatalf("copy single-role workspace template: %v", err)
	}
	return workspace
}

// sourceSnapshot fingerprints exactly what the migration tool itself reads
// from the source (see sourcebd.Runner): version, statuses, types, the three
// config keys it consults, and the full export. Comparing this snapshot
// before and after a migration run proves the migration observed a
// consistent, unmodified source.
//
// The three config keys are read via a single "config list --json" call
// rather than three "config get" calls: "config list" returns every
// configured key (a strict superset of status.custom/types.custom/
// issue-prefix), so the hash still covers everything the individual "get"
// calls covered, plus more, while paying for one bd subprocess instead of
// three. Each bd subprocess against a nontrivial workspace costs hundreds of
// milliseconds even for --readonly reads (Dolt commit-history traversal, not
// process startup), so this materially shrinks a call that every test in
// this package pays for at least twice.
//
// An earlier version of this check hashed the raw bytes under .beads
// instead. That was flaky: bd's embedded Dolt storage engine performs
// journal/manifest housekeeping (compaction, checkpointing) that can touch
// on-disk bytes even for --readonly commands, without any change to the
// logical data the migration tool actually reads. That produced
// intermittent "migration changed source .beads" failures unrelated to the
// migration tool's own behavior.
func sourceSnapshot(t *testing.T, workspace, bd string) string {
	t.Helper()
	h := sha256.New()
	for _, args := range [][]string{
		{"--readonly", "version"},
		{"--readonly", "statuses", "--json"},
		{"--readonly", "types", "--json"},
		{"--readonly", "config", "list", "--json"},
		{"--readonly", "export", "--all"},
	} {
		r := runCommand(t, workspace, bd, args...)
		if r.code != 0 {
			// Surface the failing read directly: folding a nonzero exit code
			// into the hash would otherwise turn a source-read failure into a
			// confusing "migration changed source" hash mismatch instead of
			// naming the command and its error.
			t.Fatalf("source snapshot: bd %s failed: code=%d stderr=%s", strings.Join(args, " "), r.code, r.stderr)
		}
		h.Write([]byte(strings.Join(args, "\x00")))
		h.Write([]byte{0})
		h.Write([]byte(r.stdout))
		h.Write([]byte{0})
		h.Write([]byte(r.stderr))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// recordingBD proves every migration source operation includes --readonly,
// while still exercising a real bd workspace and real bd implementation.
func recordingBD(t *testing.T, real, log string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recording-bd")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// timestampBD keeps every source operation real and read-only, but makes the
// export snapshot's otherwise clock-dependent timestamps explicit. The
// counter is outside the workspace, so the shim cannot mask a source write.
func timestampBD(t *testing.T, real string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "timestamp-bd")
	count := filepath.Join(dir, "exports")
	script := `#!/bin/sh
count=0
if [ -f '` + count + `' ]; then count=$(cat '` + count + `'); fi
if [ "$2" = export ]; then
  count=$((count + 1))
  printf '%s\n' "$count" > '` + count + `'
  if [ "$count" -eq 1 ]; then stamp=2020-01-02T03:04:05Z; else stamp=2021-02-03T04:05:06Z; fi
  '` + real + `' "$@" | sed -E "s/(\\\"(created_at|updated_at)\\\":\\\")[^\\\"]+\\\"/\\1${stamp}\\\"/g"
  exit ${PIPESTATUS:-0}
fi
exec '` + real + `' "$@"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func runMigration(t *testing.T, workspace, bd, destination string) result {
	t.Helper()
	r, _ := runMigrationChain(t, workspace, bd, destination, "")
	return r
}

// runMigrationChain is runMigration, extended so a caller driving several
// migrations back to back over the same workspace can skip recomputing a
// "before" snapshot it already knows: pass the "after" value returned by the
// previous call as knownBefore whenever nothing has written to workspace
// since, and that already-verified value is reused instead of re-running the
// same 7 read-only bd calls to reconfirm a state that has not changed. This
// never skips the check that matters -- the "after" snapshot below is always
// freshly computed -- it only skips re-deriving an already-known "before".
// Pass "" to always compute before fresh (this is what runMigration does).
func runMigrationChain(t *testing.T, workspace, bd, destination, knownBefore string) (result, string) {
	t.Helper()
	// Snapshot through the real bd binary, not the bd argument: bd here may be
	// a shim (recordingBD, timestampBD) whose behavior depends on the exact
	// sequence or count of invocations it sees, which extra snapshot calls
	// would otherwise perturb.
	real := migrationBD(t)
	before := knownBefore
	if before == "" {
		before = sourceSnapshot(t, workspace, real)
	}
	r := runCommand(t, workspace, migrationBinary, "--workspace", workspace, "--bd", bd, "--destination", destination)
	after := sourceSnapshot(t, workspace, real)
	if after != before {
		t.Fatalf("migration changed source: before=%s after=%s", before, after)
	}
	return r, after
}

func canonicalDestination(t *testing.T, destination string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(destination)
	if err != nil {
		t.Fatalf("canonicalize destination %q: %v", destination, err)
	}
	return canonical
}

// runMigrationFromWorkspace intentionally omits --workspace. The command's
// working directory is the source workspace, which is the operator path that
// must discover .beads without a subcommand or explicit source flag.
func runMigrationFromWorkspace(t *testing.T, workspace, bd, destination string) result {
	t.Helper()
	r, _ := runMigrationFromWorkspaceChain(t, workspace, bd, destination, "")
	return r
}

// runMigrationFromWorkspaceChain extends runMigrationFromWorkspace the same
// way runMigrationChain extends runMigration; see its doc comment.
func runMigrationFromWorkspaceChain(t *testing.T, workspace, bd, destination, knownBefore string) (result, string) {
	t.Helper()
	real := migrationBD(t)
	before := knownBefore
	if before == "" {
		before = sourceSnapshot(t, workspace, real)
	}
	r := runCommand(t, workspace, migrationBinary, "--bd", bd, "--destination", destination)
	after := sourceSnapshot(t, workspace, real)
	if after != before {
		t.Fatalf("migration changed source: before=%s after=%s", before, after)
	}
	return r, after
}

func wroteTo(t *testing.T, destination string) string {
	t.Helper()
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		t.Fatalf("canonicalize destination parent: %v", err)
	}
	return "wrote to " + filepath.Join(parent, filepath.Base(destination)) + "\n"
}

func assertReadonlyCalls(t *testing.T, log string) {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.FieldsFunc(strings.TrimSpace(string(b)), func(r rune) bool { return r == '\n' })
	if len(lines) == 0 {
		t.Fatal("migration made no bd calls")
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "--readonly ") {
			t.Fatalf("non-readonly bd invocation: %q", line)
		}
	}
}

func jsonObject(t *testing.T, r result) map[string]any {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("public bdd read failed: %#v", r)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
		t.Fatalf("decode public JSON %q: %v", r.stdout, err)
	}
	return got
}

func assertStrings(t *testing.T, values any, want []string, field string) {
	t.Helper()
	items, ok := values.([]any)
	if !ok || len(items) != len(want) {
		t.Fatalf("%s = %#v, want %#v", field, values, want)
	}
	got := make([]string, len(items))
	for i, item := range items {
		value, ok := item.(string)
		if !ok {
			t.Fatalf("%s = %#v, want strings %#v", field, values, want)
		}
		got[i] = value
	}
	sort.Strings(got)
	want = append([]string(nil), want...)
	sort.Strings(want)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

func assertImportedPublicState(t *testing.T, destination, wantMemory, wantRoleTitle, wantBlockerDescription string, wantLabels, wantNotes []string) {
	t.Helper()
	blocker := jsonObject(t, run(t, destination, "show", "mig-blocker", "--json"))
	for key, want := range map[string]string{
		"id": "mig-blocker", "title": "Blocker", "description": wantBlockerDescription,
		"design": "blocker design", "acceptance": "blocker acceptance",
	} {
		if blocker[key] != want {
			t.Fatalf("blocker %s = %#v, want %q", key, blocker[key], want)
		}
	}
	assertStrings(t, blocker["labels"], wantLabels, "blocker labels")
	notes, ok := blocker["notes"].([]any)
	if !ok || len(notes) != len(wantNotes) {
		t.Fatalf("blocker notes = %#v, want %d notes", blocker["notes"], len(wantNotes))
	}
	gotNotes := make([]string, len(notes))
	for i, note := range notes {
		record, ok := note.(map[string]any)
		if !ok {
			t.Fatalf("blocker note = %#v", note)
		}
		body, ok := record["body"].(string)
		if !ok {
			t.Fatalf("blocker note = %#v", note)
		}
		gotNotes[i] = body
	}
	sort.Strings(gotNotes)
	wantNotes = append([]string(nil), wantNotes...)
	sort.Strings(wantNotes)
	if strings.Join(gotNotes, "\x00") != strings.Join(wantNotes, "\x00") {
		t.Fatalf("blocker notes = %#v, want %#v", gotNotes, wantNotes)
	}
	parents := run(t, destination, "parents", "mig-related", "--json")
	if parents.code != 0 || !strings.Contains(parents.stdout, "mig-blocker") {
		t.Fatalf("public edge read = %#v", parents)
	}
	custom := jsonObject(t, run(t, destination, "show", "mig-custom", "--json"))
	if custom["status"] != "awaiting_review" || custom["type"] != "incident" {
		t.Fatalf("custom card = %#v", custom)
	}
	types := run(t, destination, "types", "--json")
	statuses := run(t, destination, "statuses", "--json")
	if types.code != 0 || !strings.Contains(types.stdout, "incident") || statuses.code != 0 || !strings.Contains(statuses.stdout, "awaiting_review") {
		t.Fatalf("custom definitions missing: types=%#v statuses=%#v", types, statuses)
	}
	memory := jsonObject(t, run(t, destination, "memory", "get", "migration/seed", "--json"))
	if memory["body"] != wantMemory {
		t.Fatalf("memory = %#v", memory)
	}
	rune := jsonObject(t, run(t, destination, "rune", "get", "role/operator", "--json"))
	metadataText, ok := rune["metadata"].(string)
	metadata := map[string]any{}
	if !ok || json.Unmarshal([]byte(metadataText), &metadata) != nil {
		t.Fatalf("role metadata = %#v", rune["metadata"])
	}
	if rune["key"] != "role/operator" || rune["kind"] != "role" || rune["title"] != wantRoleTitle || rune["body"] != "role body" || rune["protected"] != true || metadata["legacy_bd_id"] != "mig-role" {
		t.Fatalf("role rune = %#v", rune)
	}
}

func TestMigrationEndToEndRerunMutationAndReadonlySource(t *testing.T) {
	t.Parallel()
	workspace := seedMigrationWorkspace(t)
	log := filepath.Join(t.TempDir(), "bd.calls")
	shim := recordingBD(t, migrationBD(t), log)
	destination := filepath.Join(t.TempDir(), ".bdd", "bdd.sqlite")

	first, afterFirst := runMigrationFromWorkspaceChain(t, workspace, shim, destination, "")
	if first.code != 0 || first.stderr != expectedRoleWarnings || first.stdout != wroteTo(t, destination) {
		t.Fatalf("first migration = %#v", first)
	}
	assertReadonlyCalls(t, log)
	assertImportedPublicState(t, destination, "seed memory", "[role] Operator", "blocker description", []string{"source", "unicode:日本語"}, []string{"blocker comment"})
	beforeRerun, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing has written to workspace since first's "after" snapshot (the
	// two calls above only read the destination), so second's "before" is
	// already known -- reuse it instead of re-querying the source.
	second, _ := runMigrationChain(t, workspace, shim, destination, afterFirst)
	if second.code != 0 || second.stdout != first.stdout || second.stderr != first.stderr {
		t.Fatalf("identical rerun = %#v, want %#v", second, first)
	}
	afterRerun, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeRerun, afterRerun) {
		t.Fatal("identical rerun changed the destination database")
	}

	// Destination-owned data must survive synchronization with the source.
	owned := run(t, destination, "create", "Destination only", "--type", "chore", "--silent")
	if owned.code != 0 {
		t.Fatalf("create destination-only card: %s", owned.stderr)
	}
	for _, args := range [][]string{
		{"create", "--id", "mig-new", "--title", "New source card", "--labels", "new-label", "--silent"},
		{"update", "mig-blocker", "--description", "changed blocker description", "--remove-label", "source", "--add-label", "changed"},
		{"comment", "mig-blocker", "second blocker comment"},
		{"update", "mig-role", "--title", "[role] Updated Operator", "--add-label", "changed"},
		{"comment", "mig-role", "second comment"},
		{"remember", "changed memory", "--key", "migration/seed"},
		{"dep", "add", "mig-new", "mig-blocker", "--type", "blocks"},
	} {
		r := runCommand(t, workspace, migrationBD(t), args...)
		if r.code != 0 {
			t.Fatalf("mutate bd %v: %s", args, r.stderr)
		}
	}
	third := runMigration(t, workspace, shim, destination)
	if third.code != 0 {
		t.Fatalf("mutated migration: %#v", third)
	}
	assertImportedPublicState(t, destination, "changed memory", "[role] Updated Operator", "changed blocker description", []string{"changed", "unicode:日本語"}, []string{"blocker comment", "second blocker comment"})
	newParents := run(t, destination, "parents", "mig-new", "--json")
	if newParents.code != 0 || !strings.Contains(newParents.stdout, "mig-blocker") {
		t.Fatalf("mutated public edge read = %#v", newParents)
	}
	listed := run(t, destination, "list", "--json")
	if listed.code != 0 || !strings.Contains(listed.stdout, "mig-new") || !strings.Contains(listed.stdout, strings.TrimSpace(owned.stdout)) {
		t.Fatalf("destination did not converge or preserve owned data: %#v", listed)
	}

	// Removing source data never deletes its already-migrated destination card.
	deleted := runCommand(t, workspace, migrationBD(t), "delete", "mig-new", "--force")
	if deleted.code != 0 {
		t.Fatalf("delete source card: %s", deleted.stderr)
	}
	fourth := runMigration(t, workspace, shim, destination)
	if fourth.code != 0 {
		t.Fatalf("post-delete migration: %#v", fourth)
	}
	listed = run(t, destination, "list", "--json")
	if !strings.Contains(listed.stdout, "mig-new") {
		t.Fatalf("source deletion removed destination record: %s", listed.stdout)
	}
	assertReadonlyCalls(t, log)
}

func TestMigrationSingleValidRoleBecomesProtectedRune(t *testing.T) {
	t.Parallel()
	workspace := seedSingleRoleWorkspace(t)
	destination := filepath.Join(t.TempDir(), ".bdd", "bdd.sqlite")
	r := runMigration(t, workspace, migrationBD(t), destination)
	if r.code != 0 || r.stdout != wroteTo(t, destination) || r.stderr != "" {
		t.Fatalf("single-role migration = %#v", r)
	}
	rune := jsonObject(t, run(t, destination, "rune", "get", "role/operator", "--json"))
	if rune["protected"] != true {
		t.Fatalf("migrated role is not protected: %#v", rune)
	}
}

func TestMigrationReconcilesSourceTimestamps(t *testing.T) {
	t.Parallel()
	workspace := seedMigrationWorkspace(t)
	shim := timestampBD(t, migrationBD(t))
	destination := filepath.Join(t.TempDir(), ".bdd", "bdd.sqlite")
	first := runMigration(t, workspace, shim, destination)
	if first.code != 0 || first.stdout != wroteTo(t, destination) {
		t.Fatalf("timestamp initial migration = %#v", first)
	}
	initial := jsonObject(t, run(t, destination, "show", "mig-blocker", "--json"))
	for _, field := range []string{"created_at", "updated_at"} {
		if initial[field] != "2020-01-02T03:04:05Z" {
			t.Fatalf("initial %s = %#v, want fixture timestamp", field, initial[field])
		}
	}
	changed := runCommand(t, workspace, migrationBD(t), "update", "mig-blocker", "--title", "Timestamp changed")
	if changed.code != 0 {
		t.Fatalf("mutate timestamp source: %#v", changed)
	}
	second := runMigration(t, workspace, shim, destination)
	if second.code != 0 || second.stdout != wroteTo(t, destination) {
		t.Fatalf("timestamp rerun = %#v", second)
	}
	reconciled := jsonObject(t, run(t, destination, "show", "mig-blocker", "--json"))
	if reconciled["title"] != "Timestamp changed" {
		t.Fatalf("timestamp source mutation did not upsert card: %#v", reconciled)
	}
	for _, field := range []string{"created_at", "updated_at"} {
		if reconciled[field] != "2021-02-03T04:05:06Z" {
			t.Fatalf("reconciled %s = %#v, want fixture timestamp", field, reconciled[field])
		}
	}
}

func TestMigrationHelpFailureAndArchitectureContracts(t *testing.T) {
	t.Parallel()
	workspace := seedMigrationWorkspace(t)
	log := filepath.Join(t.TempDir(), "bd.calls")
	shim := recordingBD(t, migrationBD(t), log)
	destination := filepath.Join(t.TempDir(), "not-created", "store.sqlite")
	for _, flag := range []string{"--help", "-h"} {
		help := runCommand(t, "", migrationBinary, flag, "--workspace", workspace, "--bd", shim, "--destination", destination)
		if help.code != 0 || help.stderr != "" || !strings.HasPrefix(help.stdout, "Usage:") {
			t.Fatalf("help %s = %#v", flag, help)
		}
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("help invoked bd or otherwise accessed the source: %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("help touched destination: %v", err)
	}
	badArgs := runCommand(t, "", migrationBinary, "--not-a-real-flag")
	if badArgs.code != 2 || badArgs.stdout != "" || !strings.Contains(badArgs.stderr, "flag provided but not defined") {
		t.Fatalf("bad arguments = %#v", badArgs)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	bad := runCommand(t, "", migrationBinary, "--workspace", missing)
	if bad.code != 2 || bad.stdout != "" || !strings.Contains(bad.stderr, "no .beads directory") {
		t.Fatalf("missing source = %#v", bad)
	}

	deps := runCommand(t, "../..", "go", "list", "-deps", "-f", "{{.ImportPath}}", "./cmd/bdd")
	if deps.code != 0 {
		t.Fatalf("list bdd dependencies: %s", deps.stderr)
	}
	if strings.Contains(deps.stdout, "/tools/migrate") {
		t.Fatalf("bdd dependency graph links migration package:\n%s", deps.stdout)
	}
}

func TestMigrationUnsupportedRecordsWarnButSupportedRecordsImport(t *testing.T) {
	t.Parallel()
	workspace := seedMigrationWorkspace(t)
	bd := migrationBD(t)
	for _, args := range [][]string{
		{"create", "--id", "mig-supported", "--title", "Supported card", "--silent"},
		{"create", "--id", "mig-malformed", "--title", "[role]", "--type", "role", "--silent"},
		{"config", "set", "types.custom", "role,incident,infrastructure"},
		{"create", "--id", "mig-infrastructure", "--title", "Infrastructure", "--type", "infrastructure", "--silent"},
	} {
		r := runCommand(t, workspace, bd, args...)
		if r.code != 0 {
			t.Fatalf("seed unsupported record %v: %s", args, r.stderr)
		}
	}
	destination := filepath.Join(t.TempDir(), ".bdd", "bdd.sqlite")
	r := runMigration(t, workspace, bd, destination)
	if r.code != 0 || r.stdout != wroteTo(t, destination) {
		t.Fatalf("unsupported-record migration = %#v", r)
	}
	// Lines are sorted by source ID and reasons within each ID are stable. A
	// malformed role is non-fatal while the ordinary card remains importable.
	wantWarnings := "warning: mig-infrastructure: unsupported issue type \"infrastructure\"; skipped record\n" +
		"warning: mig-malformed: role title does not produce a valid rune key; skipped record\n" +
		expectedRoleWarnings
	if r.stderr != wantWarnings {
		t.Fatalf("warning output = %q, want %q", r.stderr, wantWarnings)
	}
	listed := run(t, destination, "list", "--json")
	if listed.code != 0 || !strings.Contains(listed.stdout, "mig-supported") {
		t.Fatalf("supported record was not imported: %#v", listed)
	}
}

func TestMigrationCorruptDestinationDoesNotPartiallyUpsert(t *testing.T) {
	t.Parallel()
	workspace := seedMigrationWorkspace(t)
	destination := filepath.Join(t.TempDir(), "corrupt.sqlite")
	contents := []byte("not a sqlite database")
	if err := os.WriteFile(destination, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	r := runMigration(t, workspace, migrationBD(t), destination)
	if r.code != 1 || r.stdout != "" {
		t.Fatalf("corrupt destination = %#v", r)
	}
	after, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(contents, after) {
		t.Fatal("failed migration modified corrupt destination")
	}
}

func TestMigrationNewDestinationPublicationFailureLeavesNoArtifact(t *testing.T) {
	t.Parallel()
	workspace := seedMigrationWorkspace(t)
	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("destination parent blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parentFile, "store.sqlite")
	r := runMigration(t, workspace, migrationBD(t), destination)
	if r.code != 1 || r.stdout != "" {
		t.Fatalf("new destination publication failure = %#v", r)
	}
	if contents, err := os.ReadFile(parentFile); err != nil || string(contents) != "destination parent blocker" {
		t.Fatalf("failed new destination migration changed its parent blocker: contents=%q err=%v", contents, err)
	}
}

func TestMigrationDestinationOwnedCollisionsWarnAndPreserveRecords(t *testing.T) {
	t.Parallel()
	workspace := seedMigrationWorkspace(t)
	destinationDir := t.TempDir()
	destination := filepath.Join(destinationDir, ".bdd", "bdd.sqlite")
	if init := run(t, "", "init", "--prefix", "mig", destinationDir); init.code != 0 {
		t.Fatalf("initialize destination: %#v", init)
	}
	ownedCard := run(t, destination, "create", "Native card", "--type", "chore", "--silent")
	if ownedCard.code != 0 {
		t.Fatalf("create owned card: %#v", ownedCard)
	}
	ownedID := strings.TrimSpace(ownedCard.stdout)
	ownedRune := run(t, destination, "rune", "set", "role/collision", "--kind", "role", "--title", "Native rune", "--body", "native")
	if ownedRune.code != 0 {
		t.Fatalf("create owned rune: %#v", ownedRune)
	}
	bd := migrationBD(t)
	for _, args := range [][]string{
		{"create", "--id", ownedID, "--title", "Source overwrite attempt", "--silent"},
		{"create", "--id", "mig-role-collision", "--title", "[role] Collision", "--type", "role", "--silent"},
	} {
		if seeded := runCommand(t, workspace, bd, args...); seeded.code != 0 {
			t.Fatalf("seed collision %v: %#v", args, seeded)
		}
	}
	r := runMigration(t, workspace, bd, destination)
	// ownedID carries a random suffix (see insertCardRow), so its position
	// among the sorted-by-source-ID warning lines is not fixed relative to
	// the other, statically-named sources. Sort by source ID, the same key
	// the tool sorts its output by, instead of assuming a fixed order.
	type sourceLine struct{ source, line string }
	wantSourceLines := []sourceLine{
		{ownedID, "warning: " + ownedID + ": destination-owned card ID collision; skipped record"},
		{"mig-role", strings.TrimSuffix(expectedRoleWarnings, "\n")},
		{"mig-role-collision", "warning: mig-role-collision: destination-owned rune key collision; skipped record"},
	}
	sort.Slice(wantSourceLines, func(i, j int) bool { return wantSourceLines[i].source < wantSourceLines[j].source })
	wantLines := make([]string, len(wantSourceLines))
	for i, sl := range wantSourceLines {
		wantLines[i] = sl.line
	}
	wantWarnings := strings.Join(wantLines, "\n") + "\n"
	if r.code != 0 || r.stderr != wantWarnings || r.stdout != wroteTo(t, destination) {
		t.Fatalf("collision migration = %#v", r)
	}
	card := jsonObject(t, run(t, destination, "show", ownedID, "--json"))
	if card["title"] != "Native card" {
		t.Fatalf("destination-owned card overwritten: %#v", card)
	}
	rune := jsonObject(t, run(t, destination, "rune", "get", "role/collision", "--json"))
	if rune["title"] != "Native rune" || rune["protected"] != false {
		t.Fatalf("destination-owned rune overwritten: %#v", rune)
	}
}

func TestMigrationTransactionFailureRollsBackEarlierUpserts(t *testing.T) {
	t.Parallel()
	workspace := seedMigrationWorkspace(t)
	bd := migrationBD(t)
	if seeded := runCommand(t, workspace, bd, "create", "--id", "mig-fail", "--title", "Transaction failure", "--silent"); seeded.code != 0 {
		t.Fatalf("seed failure card: %#v", seeded)
	}
	destinationDir := t.TempDir()
	if init := run(t, "", "init", "--prefix", "mig", destinationDir); init.code != 0 {
		t.Fatalf("initialize destination: %#v", init)
	}
	destination := filepath.Join(destinationDir, ".bdd", "bdd.sqlite")
	native := run(t, destination, "create", "Preexisting", "--type", "chore", "--silent")
	if native.code != 0 {
		t.Fatalf("create native card: %#v", native)
	}
	nativeID := strings.TrimSpace(native.stdout)
	before := run(t, destination, "show", nativeID, "--json")
	if before.code != 0 || !strings.Contains(before.stdout, nativeID) {
		t.Fatalf("pre-failure destination does not contain native card: %#v", before)
	}
	// The trigger is harness-only fault injection. It fires after the sorted
	// import has already attempted earlier cards, exercising SQLite rollback in
	// the real compiled migration executable rather than a sink test seam.
	db, err := sql.Open("sqlite", destination)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TRIGGER migration_fail_before_card BEFORE INSERT ON cards WHEN NEW.id = 'mig-fail' BEGIN SELECT RAISE(ABORT, 'injected transaction failure'); END`)
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("install failure trigger: exec=%v close=%v", err, closeErr)
	}
	r, afterR := runMigrationChain(t, workspace, bd, destination, "")
	if r.code != 1 || r.stdout != "" || !strings.Contains(r.stderr, "injected transaction failure") {
		t.Fatalf("injected failure = %#v", r)
	}
	after := run(t, destination, "show", nativeID, "--json")
	if after.code != 0 || after.stdout != before.stdout {
		t.Fatalf("transaction failure changed native card: before=%#v after=%#v", before, after)
	}
	for _, id := range []string{"mig-blocker", "mig-fail"} {
		if got := run(t, destination, "show", id, "--json"); got.code == 0 {
			t.Fatalf("transaction failure left partial source record %s: %#v", id, got)
		}
	}
	db, err = sql.Open("sqlite", destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TRIGGER migration_fail_before_card`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Nothing has written to workspace since r's "after" snapshot (the trigger
	// install/drop and reads above only touch the destination sqlite file
	// directly), so retry's "before" is already known.
	retry, _ := runMigrationChain(t, workspace, bd, destination, afterR)
	if retry.code != 0 || retry.stdout != wroteTo(t, destination) {
		t.Fatalf("retry after rollback = %#v", retry)
	}
	if got := run(t, destination, "show", nativeID, "--json"); got.code != 0 || got.stdout != before.stdout {
		t.Fatalf("retry changed native destination record: before=%#v after=%#v", before, got)
	}
	if got := run(t, destination, "show", "mig-fail", "--json"); got.code != 0 {
		t.Fatalf("retry did not converge imported source record: %#v", got)
	}
}
