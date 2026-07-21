// Package sourcebd reads Beads through its public, read-only command surface.
package sourcebd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Version is the only Beads version whose export shapes this adapter supports.
const Version = "1.0.3"

// Result keeps streams separate: Beads diagnostics must never contaminate JSONL.
type Result struct{ Stdout, Stderr []byte }

// Runner is the single choke point for every invocation of bd.
type Runner struct {
	Binary    string
	Workspace string
	// RunCommand is a test seam.  Production callers leave it nil.
	RunCommand func(context.Context, string, []string, string, []string) (Result, error)
}

func (r Runner) command(ctx context.Context, args ...string) (Result, error) {
	if r.Binary == "" {
		r.Binary = "bd"
	}
	workspace, err := filepath.Abs(r.Workspace)
	if err != nil {
		return Result{}, fmt.Errorf("resolve workspace: %w", err)
	}
	full := append([]string{"--readonly"}, args...)
	env := cleanEnv(os.Environ())
	if r.RunCommand != nil {
		return r.RunCommand(ctx, r.Binary, full, workspace, env)
	}
	cmd := exec.CommandContext(ctx, r.Binary, full...)
	cmd.Dir, cmd.Env = workspace, env
	var result Result
	cmd.Stdout, cmd.Stderr = (*bytesWriter)(&result.Stdout), (*bytesWriter)(&result.Stderr)
	if err := cmd.Run(); err != nil {
		return result, fmt.Errorf("bd %s: %w: %s", strings.Join(full, " "), err, strings.TrimSpace(string(result.Stderr)))
	}
	return result, nil
}

// bytesWriter appends rather than combining the command's streams.
type bytesWriter []byte

func (w *bytesWriter) Write(p []byte) (int, error) { *w = append(*w, p...); return len(p), nil }

func cleanEnv(env []string) []string {
	result := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, "BD_JSON_ENVELOPE=") {
			result = append(result, entry)
		}
	}
	return result
}

func (r Runner) Export(ctx context.Context) (Result, error) { return r.command(ctx, "export", "--all") }
func (r Runner) Version(ctx context.Context) (string, error) {
	result, err := r.command(ctx, "version")
	if err != nil {
		return "", err
	}
	return ParseVersion(string(result.Stdout))
}
func (r Runner) Statuses(ctx context.Context) (Result, error) {
	return r.command(ctx, "statuses", "--json")
}
func (r Runner) Types(ctx context.Context) (Result, error) { return r.command(ctx, "types", "--json") }
func (r Runner) Config(ctx context.Context, key string) (Result, error) {
	return r.command(ctx, "config", "get", key)
}

// ParseVersion rejects version output outside the fixture compatibility matrix.
func ParseVersion(output string) (string, error) {
	fields := strings.Fields(output)
	if len(fields) < 3 || fields[0] != "bd" || fields[1] != "version" {
		return "", fmt.Errorf("unsupported bd version output %q", strings.TrimSpace(output))
	}
	if fields[2] != Version {
		return "", fmt.Errorf("unsupported bd version %q (supported: %s)", fields[2], Version)
	}
	return fields[2], nil
}
