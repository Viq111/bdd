package testdata

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedScriptDoesNotModifyInvokingBeads(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, ".beads", "issues.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "seed-fixtures.sh")
	cmd.Dir = filepath.Join(root, "tools", "migrate", "testdata")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed helper: %v\n%s", err, output)
	}
	after, err := os.ReadFile(filepath.Join(root, ".beads", "issues.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("seed helper modified the invoking repository's .beads/issues.jsonl")
	}
}

func TestSeedScriptUsesPublicCommandsForCompleteFixtureShape(t *testing.T) {
	script, err := os.ReadFile("seed-fixtures.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	for _, want := range []string{
		"--id \"$role_id\"", // representative linked IDs
		"--acceptance \"$acceptance\"",
		"--append-notes 'accumulated note'",
		"bd comment \"$role_id\"",
		"bd dep add \"$role_id\" \"$blocker_id\" --type blocks",
		"bd dep add \"$role_id\" \"$related_id\" --type related",
		"bd remember \"remember this $shape fixture\" --key \"$memory_key\"",
		"bd --readonly export --all",
		"bd --readonly version",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("seed script does not create required fixture data with %q", want)
		}
	}
}
