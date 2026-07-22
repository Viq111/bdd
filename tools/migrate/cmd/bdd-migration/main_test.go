package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/viq111/bdd"
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
	cfg, err := sourceConfig([]byte(`{"built_in_statuses":[{"name":"open","category":"active"}],"custom_statuses":[{"name":"awaiting_review","category":"wip"}],"schema_version":1}`), []byte(`{"core_types":[{"name":"task"}],"custom_types":["role"],"schema_version":1}`), []byte("awaiting_review\n"), nil, []byte("demo\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StatusCategories["open"] != "active" || cfg.StatusCategories["awaiting_review"] != "wip" || !cfg.CustomTypes["role"] || cfg.LegacyStatusCategories["awaiting_review"] != "wip" || cfg.IssuePrefix != "demo" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestRunMainImportsBD103ConfigurationEnvelopes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bd command uses a POSIX shell script")
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(workspace, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	bd := filepath.Join(root, "bd")
	responses := map[string]string{
		"version":                  `bd version 1.0.3`,
		"statuses --json":          `{"built_in_statuses":[{"name":"open","category":"active"}],"custom_statuses":[{"name":"verified","category":"active"}],"schema_version":1}`,
		"types --json":             `{"core_types":[{"name":"task"}],"custom_types":["release"],"schema_version":1}`,
		"config get status.custom": `verified:active`,
		"config get types.custom":  `release`,
		"config get issue-prefix":  `source`,
		"export --all":             `{"_type":"issue","id":"source-1","title":"Imported","status":"verified","issue_type":"release"}`,
	}
	var script strings.Builder
	script.WriteString("#!/bin/sh\nshift\ncase \"$*\" in\n")
	for args, output := range responses {
		fmt.Fprintf(&script, "\"%s\") printf '%%s\\n' '%s' ;;\n", args, output)
	}
	script.WriteString("*) exit 1 ;;\nesac\n")
	if err := os.WriteFile(bd, []byte(script.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "destination.sqlite")
	var stdout, stderr bytes.Buffer
	if got := runMain(context.Background(), []string{"--workspace", workspace, "--bd", bd, "--destination", destination}, &stdout, &stderr); got != 0 {
		t.Fatalf("runMain exit = %d, stderr = %s", got, stderr.String())
	}
	db, err := bdd.Open(context.Background(), bdd.OpenOptions{Path: destination})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cards, err := db.ListCards(context.Background(), bdd.ListOptions{})
	if err != nil || len(cards) != 1 || cards[0].ID != "source-1" || cards[0].Status != "verified" || cards[0].Type != "release" {
		t.Fatalf("imported cards = %#v, %v", cards, err)
	}
}
