package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersionFastPathIgnoresWorkspace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// A workspace flag pointing nowhere real must not force discovery or a
	// SQLite open for version/help.
	code := Run([]string{"--workspace", "/does/not/exist", "version"}, &stdout, &stderr, "1.2.3")
	if code != ExitSuccess {
		t.Fatalf("Run(version) exit = %d, stderr = %q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "1.2.3" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "1.2.3")
	}
}

func TestRunHelpNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(nil, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run() exit = %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("stdout = %q, want help text", stdout.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"nope"}, &stdout, &stderr, "dev")
	if code != ExitUsage {
		t.Fatalf("Run(nope) exit = %d, want %d", code, ExitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "nope") {
		t.Fatalf("stderr = %q, want mention of unknown command", stderr.String())
	}
}

func TestRunGlobalFlagBeforeCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--json", "version"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run() exit = %d, stderr = %q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "dev" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "dev")
	}
}
