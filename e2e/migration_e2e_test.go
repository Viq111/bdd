package e2e_test

// This file intentionally uses only subprocesses and the public bd/bdd
// command surfaces. It is the migration QA harness, rather than a unit-test
// seam for tools/migrate.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

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

func seedMigrationWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	bd := migrationBD(t)
	for _, args := range [][]string{
		{"init", "--non-interactive", "--quiet", "--prefix", "mig", "--skip-agents", "--skip-hooks"},
		{"config", "set", "status.custom", "awaiting_review"},
		{"config", "set", "types.custom", "role"},
		{"create", "--id", "mig-blocker", "--title", "Blocker", "--silent"},
		{"create", "--id", "mig-related", "--title", "Related", "--silent"},
		{"create", "--id", "mig-role", "--title", "[role] Operator", "--type", "role", "--description", "role body", "--labels", "runtime:codex,seeded", "--silent"},
		{"comment", "mig-role", "first comment"},
		{"dep", "add", "mig-role", "mig-blocker", "--type", "blocks"},
		{"dep", "add", "mig-role", "mig-related", "--type", "related"},
		{"remember", "seed memory", "--key", "migration/seed"},
	} {
		r := runCommand(t, workspace, bd, args...)
		if r.code != 0 {
			t.Fatalf("seed bd %v: code=%d stderr=%s", args, r.code, r.stderr)
		}
	}
	return workspace
}

func beadsDigest(t *testing.T, workspace string) string {
	t.Helper()
	root := filepath.Join(workspace, ".beads")
	var names []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			names = append(names, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		rel, _ := filepath.Rel(root, name)
		contents, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte(filepath.ToSlash(rel)))
		h.Write([]byte{0})
		h.Write(contents)
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

func runMigration(t *testing.T, workspace, bd, destination string) result {
	t.Helper()
	before := beadsDigest(t, workspace)
	r := runCommand(t, workspace, migrationBinary, "--workspace", workspace, "--bd", bd, "--destination", destination)
	if after := beadsDigest(t, workspace); after != before {
		t.Fatalf("migration changed source .beads: before=%s after=%s", before, after)
	}
	return r
}

func canonicalDestination(t *testing.T, destination string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(destination)
	if err != nil {
		t.Fatalf("canonicalize destination %q: %v", destination, err)
	}
	return canonical
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

func TestMigrationEndToEndRerunMutationAndReadonlySource(t *testing.T) {
	workspace := seedMigrationWorkspace(t)
	log := filepath.Join(t.TempDir(), "bd.calls")
	shim := recordingBD(t, migrationBD(t), log)
	destination := filepath.Join(t.TempDir(), "destination.sqlite")

	first := runMigration(t, workspace, shim, destination)
	if first.code != 0 || first.stderr != expectedRoleWarnings || first.stdout != "wrote to "+canonicalDestination(t, destination)+"\n" {
		t.Fatalf("first migration = %#v", first)
	}
	assertReadonlyCalls(t, log)
	beforeRerun, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	second := runMigration(t, workspace, shim, destination)
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
}

func TestMigrationHelpFailureAndArchitectureContracts(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	destination := filepath.Join(t.TempDir(), "not-created", "store.sqlite")
	help := runCommand(t, "", migrationBinary, "--help", "--workspace", missing, "--destination", destination)
	if help.code != 0 || help.stderr != "" || !strings.HasPrefix(help.stdout, "Usage:") {
		t.Fatalf("help = %#v", help)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("help touched destination: %v", err)
	}
	bad := runCommand(t, "", migrationBinary, "--workspace", missing)
	if bad.code != 2 || bad.stdout != "" || !strings.Contains(bad.stderr, "no .beads directory") {
		t.Fatalf("missing source = %#v", bad)
	}

	deps := runCommand(t, "..", "go", "list", "-deps", "-f", "{{.ImportPath}}", "./cmd/bdd")
	if deps.code != 0 {
		t.Fatalf("list bdd dependencies: %s", deps.stderr)
	}
	if strings.Contains(deps.stdout, "/tools/migrate") {
		t.Fatalf("bdd dependency graph links migration package:\n%s", deps.stdout)
	}
}

func TestMigrationUnsupportedRecordsWarnButSupportedRecordsImport(t *testing.T) {
	workspace := seedMigrationWorkspace(t)
	bd := migrationBD(t)
	for _, args := range [][]string{
		{"create", "--id", "mig-supported", "--title", "Supported card", "--silent"},
		{"create", "--id", "mig-malformed", "--title", "[role]", "--type", "role", "--silent"},
	} {
		r := runCommand(t, workspace, bd, args...)
		if r.code != 0 {
			t.Fatalf("seed unsupported record %v: %s", args, r.stderr)
		}
	}
	destination := filepath.Join(t.TempDir(), "destination.sqlite")
	r := runMigration(t, workspace, bd, destination)
	if r.code != 0 || r.stdout != "wrote to "+canonicalDestination(t, destination)+"\n" {
		t.Fatalf("unsupported-record migration = %#v", r)
	}
	// Lines are sorted by source ID and reasons within each ID are stable. A
	// malformed role is non-fatal while the ordinary card remains importable.
	wantWarnings := "warning: mig-malformed: role title does not produce a valid rune key; skipped record\n" +
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
