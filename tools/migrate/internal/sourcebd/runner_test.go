package sourcebd

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunnerUsesReadonlyWorkspaceCleanEnvironmentAndSeparateStreams(t *testing.T) {
	t.Setenv("BD_JSON_ENVELOPE", "wrapped")
	var gotArgs []string
	var gotWorkspace string
	var gotEnv []string
	r := Runner{Workspace: t.TempDir(), RunCommand: func(_ context.Context, binary string, args []string, workspace string, env []string) (Result, error) {
		if binary != "bd" {
			t.Fatalf("binary = %q", binary)
		}
		gotArgs, gotWorkspace, gotEnv = args, workspace, env
		return Result{Stdout: []byte("jsonl"), Stderr: []byte("diagnostic")}, nil
	}}
	result, err := r.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(gotArgs, " "), "--readonly") {
		t.Fatalf("args lack --readonly: %q", gotArgs)
	}
	if gotArgs[1] != "export" || gotArgs[2] != "--all" {
		t.Fatalf("args = %q", gotArgs)
	}
	if gotWorkspace == "" {
		t.Fatal("workspace was not passed")
	}
	if strings.Contains(strings.Join(gotEnv, "\n"), "BD_JSON_ENVELOPE=") {
		t.Fatalf("shape variable was preserved: %q", gotEnv)
	}
	if string(result.Stdout) != "jsonl" || string(result.Stderr) != "diagnostic" {
		t.Fatalf("streams combined: %#v", result)
	}
}

func TestEveryRunnerCommandIsReadonly(t *testing.T) {
	var commands [][]string
	r := Runner{Workspace: t.TempDir(), RunCommand: func(_ context.Context, _ string, args []string, _ string, _ []string) (Result, error) {
		commands = append(commands, args)
		return Result{Stdout: []byte("bd version 1.0.3 (test)\n")}, nil
	}}
	ctx := context.Background()
	if _, err := r.Export(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Version(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Statuses(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Types(ctx); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"status.custom", "types.custom", "issue-prefix"} {
		if _, err := r.Config(ctx, key); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range commands {
		if len(command) == 0 || command[0] != "--readonly" {
			t.Fatalf("runner constructed non-readonly command: %q", command)
		}
	}
}

func TestParseVersion(t *testing.T) {
	accepted := []string{
		"bd version 1.0.0",
		"bd version 1.0.3",
		"bd version 1.0.99",
		"bd version 1.0.3 (1b2dd2cb: main@1b2dd2cb56b3)",
		"bd version 1.0.3-rc.1",
		"bd version 1.0.3+build.7",
	}
	for _, output := range accepted {
		if _, err := ParseVersion(output); err != nil {
			t.Fatalf("ParseVersion(%q) = %v, want accept", output, err)
		}
	}

	rejected := []string{
		"bd version 1.1.0",
		"bd version 0.9.9",
		"bd version 2.0.0",
		"bd version 9.9.9",
	}
	for _, output := range rejected {
		_, err := ParseVersion(output)
		if err == nil {
			t.Fatalf("ParseVersion(%q) accepted unsupported version", output)
		}
		var unsupported *UnsupportedVersionError
		if !errors.As(err, &unsupported) {
			t.Fatalf("ParseVersion(%q) error = %v, want *UnsupportedVersionError", output, err)
		}
	}

	malformed := []string{"", "not a version string", "bd 1.0.3"}
	for _, output := range malformed {
		_, err := ParseVersion(output)
		if err == nil {
			t.Fatalf("ParseVersion(%q) accepted malformed output", output)
		}
		var unsupported *UnsupportedVersionError
		if errors.As(err, &unsupported) {
			t.Fatalf("ParseVersion(%q) returned UnsupportedVersionError for malformed output", output)
		}
		if !strings.Contains(err.Error(), "unsupported bd version output") {
			t.Fatalf("ParseVersion(%q) error = %v, want malformed-output message", output, err)
		}
	}
}
