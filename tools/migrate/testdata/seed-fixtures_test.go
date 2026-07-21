package testdata

import (
	"os"
	"os/exec"
	"path/filepath"
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
