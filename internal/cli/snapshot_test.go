package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func initWorkspace(t *testing.T, dir string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"init", "--prefix", "acme", dir}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(init) exit = %d, stderr = %q", code, stderr.String())
	}
}

func TestSnapshotDefaultOutputAndHumanOutput(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "snapshot"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(snapshot) exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	wantPath := filepath.Join(dir, ".bdd", "backup.sqlite")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected snapshot at %s: %v", wantPath, err)
	}
	if !strings.Contains(stdout.String(), wantPath) {
		t.Fatalf("stdout = %q, want mention of %s", stdout.String(), wantPath)
	}
}

func TestSnapshotExplicitOutputJSON(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)
	out := filepath.Join(dir, "custom-backup.sqlite")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "snapshot", "--output", out, "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(snapshot) exit = %d, stderr = %q", code, stderr.String())
	}

	var result SnapshotResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if result.Path != out {
		t.Fatalf("result.Path = %q, want %q", result.Path, out)
	}
	if result.SchemaVersion == 0 {
		t.Fatal("result.SchemaVersion = 0, want > 0")
	}
}

func TestSnapshotSilentEmitsOnlyPath(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "snapshot", "--silent"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(snapshot) exit = %d, stderr = %q", code, stderr.String())
	}

	wantPath := filepath.Join(dir, ".bdd", "backup.sqlite")
	if got := strings.TrimSpace(stdout.String()); got != wantPath {
		t.Fatalf("stdout = %q, want %q", got, wantPath)
	}
}

func TestSnapshotRejectsUnknownFlag(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "snapshot", "--bogus"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(snapshot) exit = %d, want %d", code, ExitUsage)
	}
}

func TestSnapshotUnknownFlagVsArgument(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	cases := []struct {
		arg  string
		want string
	}{
		{"--db", `unknown flag "--db"`},
		{"--db=/tmp/example.sqlite", `unknown flag "--db=/tmp/example.sqlite"`},
		{"bogus", `unknown argument "bogus"`},
	}
	for _, tc := range cases {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"--workspace", dir, "snapshot", tc.arg}, &stdout, &stderr, "dev", "unspecified")
		if code != ExitUsage {
			t.Fatalf("Run(snapshot %s) exit = %d, want %d", tc.arg, code, ExitUsage)
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Fatalf("Run(snapshot %s) stderr = %q, want it to contain %q", tc.arg, stderr.String(), tc.want)
		}
	}
}

func TestRestoreRequiresForce(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "snapshot"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(snapshot) exit = %d, stderr = %q", code, stderr.String())
	}
	snapshotPath := filepath.Join(dir, ".bdd", "backup.sqlite")

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dir, "restore", snapshotPath}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(restore) exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "--force") {
		t.Fatalf("stderr = %q, want mention of --force", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty on failure", stdout.String())
	}
}

func TestRestoreRequiresSourceArgument(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "restore", "--force"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitUsage {
		t.Fatalf("Run(restore) exit = %d, want %d", code, ExitUsage)
	}
}

func TestRestoreMissingSnapshotIsNotFound(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", dir, "restore", filepath.Join(dir, "nope.sqlite"), "--force"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitNotFound {
		t.Fatalf("Run(restore) exit = %d, want %d, stderr = %q", code, ExitNotFound, stderr.String())
	}
}

func TestSnapshotThenRestoreRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	initWorkspace(t, srcDir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", srcDir, "create", "--type", "chore", "hello world"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(create) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", srcDir, "snapshot", "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(snapshot) exit = %d, stderr = %q", code, stderr.String())
	}
	var snap SnapshotResult
	if err := json.Unmarshal(stdout.Bytes(), &snap); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}

	dstDir := t.TempDir()
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dstDir, "restore", snap.Path, "--force", "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(restore) exit = %d, stderr = %q", code, stderr.String())
	}
	var restore RestoreResult
	if err := json.Unmarshal(stdout.Bytes(), &restore); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	if restore.BackupPath != "" {
		t.Fatalf("restore.BackupPath = %q, want empty (nothing existed at target)", restore.BackupPath)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dstDir, "search", "hello", "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(search) exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "hello world") {
		t.Fatalf("stdout = %q, want restored card", stdout.String())
	}
}

func TestRestoreBacksUpExistingTarget(t *testing.T) {
	srcDir := t.TempDir()
	initWorkspace(t, srcDir)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--workspace", srcDir, "snapshot", "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(snapshot) exit = %d, stderr = %q", code, stderr.String())
	}
	var snap SnapshotResult
	if err := json.Unmarshal(stdout.Bytes(), &snap); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}

	dstDir := t.TempDir()
	initWorkspace(t, dstDir)

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--workspace", dstDir, "restore", snap.Path, "--force", "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(restore) exit = %d, stderr = %q", code, stderr.String())
	}
	var restore RestoreResult
	if err := json.Unmarshal(stdout.Bytes(), &restore); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	if restore.BackupPath == "" {
		t.Fatal("restore.BackupPath = empty, want a backup of the pre-existing target")
	}
	if _, err := os.Stat(restore.BackupPath); err != nil {
		t.Fatalf("expected backup at %s: %v", restore.BackupPath, err)
	}
}
