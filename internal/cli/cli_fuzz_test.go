package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// FuzzParseGlobalFlags feeds arbitrary whitespace-separated tokens through
// ParseGlobalFlags, checking only that it never panics and that every
// token it returns in rest also appeared in the input (it must not
// fabricate or drop non-flag arguments).
func FuzzParseGlobalFlags(f *testing.F) {
	seeds := []string{
		"",
		"--workspace",
		"--workspace /tmp",
		"--db=/tmp/x.sqlite create --title t --type bug",
		"--json --silent create",
		"--actor= create",
		"-C /tmp --db",
		"create --title --workspace",
		"--workspace=--db=weird",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		args := strings.Fields(s)
		before := map[string]int{}
		for _, a := range args {
			before[a]++
		}

		_, rest, err := ParseGlobalFlags(args)
		if err != nil {
			return
		}
		for _, a := range rest {
			if before[a] == 0 {
				t.Fatalf("ParseGlobalFlags(%v) fabricated argument %q in rest", args, a)
			}
		}
	})
}

// FuzzRun drives the full CLI entry point with arbitrary argument vectors
// against a fresh, isolated workspace, checking only that it never panics
// and always returns one of the three documented exit codes. This is the
// broadest fuzz target for CLI argument parsing: every subcommand's flag
// parser (create, update, config, snapshot, restore, ...) is reachable
// from here.
func FuzzRun(f *testing.F) {
	seeds := []string{
		"create\x00--title\x00t\x00--type\x00bug\x00--reproduce\x00r\x00--acceptance\x00a",
		"create\x00--type\x00task\x00--acceptance\x00",
		"update\x00bdd-000000\x00--priority\x00abc",
		"config\x00set\x00status.custom\x00triage:active",
		"config\x00set\x00types.custom\x00spike",
		"snapshot\x00--output\x00out.sqlite",
		"restore\x00missing.sqlite\x00--force",
		"--json\x00list",
		"note\x00bdd-000000\x00--stdin",
		"",
		"\x00\x00\x00",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	// Run hardcodes Stdin to the process's os.Stdin (it takes no Streams
	// parameter), so any fuzzed "--stdin" flag would otherwise block on
	// whatever this test binary happened to inherit. Point it at
	// /dev/null for the whole fuzz run: any stdin read then completes
	// immediately with EOF instead of hanging the fuzzer.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		f.Fatalf("opening %s: %v", os.DevNull, err)
	}
	f.Cleanup(func() { devNull.Close() })
	prevStdin := os.Stdin
	os.Stdin = devNull
	f.Cleanup(func() { os.Stdin = prevStdin })

	f.Fuzz(func(t *testing.T, raw string) {
		args := strings.Split(raw, "\x00")
		if len(args) == 1 && args[0] == "" {
			args = nil
		}
		if len(args) > 64 {
			t.Skip("too many arguments to be a useful fuzz case")
		}

		dir := t.TempDir()
		args = append([]string{"--workspace", dir}, args...)

		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr, "fuzz", "unspecified")
		switch code {
		case ExitSuccess, ExitOther, ExitUsage, ExitNotFound, ExitConflict:
		default:
			t.Fatalf("Run(%v) returned undocumented exit code %d", args, code)
		}
	})
}
