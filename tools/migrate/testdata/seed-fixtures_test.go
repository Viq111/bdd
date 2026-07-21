package testdata

import (
	"crypto/sha256"
	"io"
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
	before := treeHash(t, filepath.Join(root, ".beads"))
	cmd := exec.Command("bash", "seed-fixtures.sh")
	cmd.Dir = filepath.Join(root, "tools", "migrate", "testdata")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed helper: %v\n%s", err, output)
	}
	after := treeHash(t, filepath.Join(root, ".beads"))
	if before != after {
		t.Fatal("seed helper modified the invoking repository's .beads directory")
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

func treeHash(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return "absent"
	} else if err != nil {
		t.Fatal(err)
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(h, rel); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(h, file)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(h.Sum(nil))
}
