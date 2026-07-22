package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusesListsBuiltins(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"statuses", "--workspace", dir, "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(statuses) exit = %d, stderr = %q", code, stderr.String())
	}
	var defs []StatusDefResult
	if err := json.Unmarshal(stdout.Bytes(), &defs); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(defs) != 6 {
		t.Fatalf("len(defs) = %d, want 6 built-in statuses", len(defs))
	}
	for _, d := range defs {
		if !d.BuiltIn {
			t.Fatalf("status %q: BuiltIn = false, want true", d.Name)
		}
	}
}

func TestStatusesIncludesCustomAfterConfigSet(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"config", "set", "--workspace", dir, "status.custom", "ready_to_ship:active"}, &stdout, &stderr, "dev", "unspecified"); code != ExitSuccess {
		t.Fatalf("Run(config set) exit = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"statuses", "--workspace", dir, "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(statuses) exit = %d, stderr = %q", code, stderr.String())
	}
	var defs []StatusDefResult
	if err := json.Unmarshal(stdout.Bytes(), &defs); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	found := false
	for _, d := range defs {
		if d.Name == "ready_to_ship" {
			found = true
			if d.BuiltIn {
				t.Fatal("ready_to_ship.BuiltIn = true, want false")
			}
			if d.Category != "active" {
				t.Fatalf("ready_to_ship.Category = %q, want active", d.Category)
			}
		}
	}
	if !found {
		t.Fatal("ready_to_ship not found in statuses output")
	}
}

func TestTypesListsBuiltins(t *testing.T) {
	dir := initTestWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"types", "--workspace", dir, "--json"}, &stdout, &stderr, "dev", "unspecified")
	if code != ExitSuccess {
		t.Fatalf("Run(types) exit = %d, stderr = %q", code, stderr.String())
	}
	var defs []TypeDefResult
	if err := json.Unmarshal(stdout.Bytes(), &defs); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	if len(defs) != 6 {
		t.Fatalf("len(defs) = %d, want 6 built-in types", len(defs))
	}
}

func TestStatusesAndTypesRejectRemovedDBFlag(t *testing.T) {
	dir := initTestWorkspace(t)

	cases := []struct {
		name string
		cmd  string
	}{
		{"statuses", "statuses"},
		{"types", "types"},
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
				args := append(append([]string{}, form.lead...), tc.cmd, "--workspace", dir)
				code := Run(args, &stdout, &stderr, "dev", "unspecified")
				if code != ExitUsage {
					t.Fatalf("Run(%v) exit = %d, want %d, stderr=%q", args, code, ExitUsage, stderr.String())
				}
				want := `bdd: ` + tc.cmd + `: unknown flag "` + form.want + `"`
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("Run(%v) stderr = %q, want it to contain %q", args, stderr.String(), want)
				}
			}
		})
	}
}
