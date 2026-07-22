package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/viq111/bdd/tools/migrate/internal/sourcebd"
)

func TestHelpDoesNotTouchWorkspaceOrDestination(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	destination := filepath.Join(root, "uncreated", "bdd.sqlite")
	var stdout, stderr bytes.Buffer
	if got := runMain(context.Background(), []string{"--help", "--workspace", missing, "--destination", destination}, &stdout, &stderr); got != 0 {
		t.Fatalf("help exit = %d, want 0", got)
	}
	if stdout.String() != usage || stderr.Len() != 0 {
		t.Fatalf("help streams = (%q, %q)", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("help touched destination: stat error = %v", err)
	}
}

func TestArgumentErrorsAreExitTwoAndDoNotWriteStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := runMain(context.Background(), []string{"--unknown"}, &stdout, &stderr); got != 2 {
		t.Fatalf("unknown flag exit = %d, want 2", got)
	}
	if stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("unknown flag streams = (%q, %q)", stdout.String(), stderr.String())
	}
}

func TestParseArgsCanonicalizesWorkspaceAndRelativeDestination(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	o, err := parseArgs([]string{"--workspace", root, "--destination", "out/store.sqlite"}, new(bytes.Buffer))
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := canonicalPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if o.workspace != canonicalRoot || o.destination != filepath.Join(canonicalRoot, "out", "store.sqlite") {
		t.Fatalf("options = %#v", o)
	}
}

func TestSourceConfigAcceptsLegacyCustomStatusName(t *testing.T) {
	cfg, err := sourceConfig([]byte(`[{"name":"awaiting_review","category":"wip"}]`), []byte(`[]`), []byte("awaiting_review\n"), nil, []byte("demo\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LegacyStatusCategories["awaiting_review"] != "wip" || cfg.IssuePrefix != "demo" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestSourceConfigAcceptsBD103Envelopes(t *testing.T) {
	statuses := []byte(`{"built_in_statuses":[{"name":"open","category":"active"}],"custom_statuses":[{"name":"reviewing","category":"wip"}],"schema_version":1}`)
	types := []byte(`{"core_types":[{"name":"task"}],"custom_types":["role","runbook"],"schema_version":1}`)
	cfg, err := sourceConfig(statuses, types, []byte("reviewing\n"), []byte("role,runbook\n"), []byte("demo\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StatusCategories["open"] != "active" || cfg.StatusCategories["reviewing"] != "wip" || !cfg.CustomTypes["role"] || !cfg.CustomTypes["runbook"] || cfg.IssuePrefix != "demo" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestRunMainImportsWithBD103CommandEnvelopes(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	bd := filepath.Join(workspace, "fake-bd")
	script := `#!/bin/sh
case "$2" in
version) printf 'bd version 1.0.3\n' ;;
statuses) printf '%s\n' '{"built_in_statuses":[{"name":"open","category":"active"}],"custom_statuses":[{"name":"awaiting_review","category":"wip"}],"schema_version":1}' ;;
types) printf '%s\n' '{"core_types":[{"name":"task"}],"custom_types":["role"],"schema_version":1}' ;;
config) case "$4" in status.custom) printf 'awaiting_review\n' ;; types.custom) printf 'role\n' ;; issue-prefix) printf 'demo\n' ;; esac ;;
export) printf '%s\n' '{"_type":"issue","id":"demo-1","title":"import me","status":"open","issue_type":"task"}' ;;
esac
`
	if err := os.WriteFile(bd, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(workspace, "destination.sqlite")
	var stdout, stderr bytes.Buffer
	if got := runMain(context.Background(), []string{"--workspace", workspace, "--bd", bd, "--destination", destination}, &stdout, &stderr); got != 0 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", got, stdout.String(), stderr.String())
	}
	canonicalDestination, err := canonicalPath(destination)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "wrote to "+canonicalDestination+"\n" || stderr.Len() != 0 {
		t.Fatalf("streams = (%q, %q)", stdout.String(), stderr.String())
	}
}

func TestUnsetIssuePrefixIsInferredFromSourceID(t *testing.T) {
	records, err := sourcebd.ParseJSONL(bytes.NewBufferString(`{"_type":"issue","id":"demo-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := configuredPrefix([]byte("issue-prefix (not set)\n")); got != "" {
		t.Fatalf("configuredPrefix = %q", got)
	}
	if got, err := inferPrefix(records); err != nil || got != "demo" {
		t.Fatalf("inferPrefix = %q, %v", got, err)
	}
}
