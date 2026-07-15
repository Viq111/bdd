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

func TestRunCobraUnknownSubcommandAndFlag(t *testing.T) {
	// "config" is a cobra parent command with real (non-disabled) flag
	// parsing; an unmatched child name and an unrecognized flag both flow
	// through cobra's own command tree rather than a legacy hand-rolled
	// parser, and both must still map to ExitUsage.
	var stdout, stderr bytes.Buffer
	code := Run([]string{"config", "bogus"}, &stdout, &stderr, "dev")
	if code != ExitUsage {
		t.Fatalf("Run(config bogus) exit = %d, want %d, stderr = %q", code, ExitUsage, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"config", "--bogus"}, &stdout, &stderr, "dev")
	if code != ExitUsage {
		t.Fatalf("Run(config --bogus) exit = %d, want %d, stderr = %q", code, ExitUsage, stderr.String())
	}
}

func TestRunGlobalFlagAfterSubcommand(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"init", "--prefix", "acme", dir}, &stdout, &stderr, "dev"); code != ExitSuccess {
		t.Fatalf("Run(init) exit = %d, stderr = %q", code, stderr.String())
	}

	// --workspace and --json both appear after the subcommand name; the
	// pre-cobra global-flag pass must still honor them regardless of the
	// cobra command tree built for "status" itself.
	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"status", "--workspace", dir, "--json"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(status) exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"workspace"`) {
		t.Fatalf("stdout = %q, want JSON output", stdout.String())
	}
}

func TestRunSubcommandHelpShowsExample(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"create", "-h"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(create -h) exit = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), `bdd create "Fix login bug"`) {
		t.Fatalf("stdout = %q, want it to contain the Example text", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Flags:") {
		t.Fatalf("stdout = %q, want a Flags section", stdout.String())
	}
}

func TestRunSubcommandHelpShowsGlobalFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"create", "-h"}, &stdout, &stderr, "dev")
	if code != ExitSuccess {
		t.Fatalf("Run(create -h) exit = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"--workspace", "-C", "--db", "--actor", "--json", "--silent"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want it to mention global flag %q", out, want)
		}
	}
}

func TestRunGroupHelpShowsExample(t *testing.T) {
	for _, group := range []string{"config", "rune", "label"} {
		var stdout, stderr bytes.Buffer
		code := Run([]string{group, "-h"}, &stdout, &stderr, "dev")
		if code != ExitSuccess {
			t.Fatalf("Run(%s -h) exit = %d, stderr = %q", group, code, stderr.String())
		}
		out := stdout.String()
		if !strings.Contains(out, "Examples:") {
			t.Fatalf("Run(%s -h) stdout = %q, want an Examples section", group, out)
		}
		if !strings.Contains(out, "--workspace") {
			t.Fatalf("Run(%s -h) stdout = %q, want it to mention global flags", group, out)
		}
	}
}
