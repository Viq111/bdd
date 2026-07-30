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

// SupportedSeries lists the Beads major.minor series whose export shapes this
// adapter is known to support. Patch releases within a listed series are
// assumed compatible; series outside this list are rejected until they have
// been qualified against real fixtures.
var SupportedSeries = []string{"1.0"}

// UnsupportedVersionError reports a bd version whose major.minor series is not
// in SupportedSeries. It is distinct from malformed-output errors so callers
// can offer an opt-in override for the former but never the latter.
type UnsupportedVersionError struct {
	Version   string
	Supported []string
}

func (e *UnsupportedVersionError) Error() string {
	return fmt.Sprintf("unsupported bd version %q (supported: %s)", e.Version, FormatSupportedSeries(e.Supported))
}

// FormatSupportedSeries renders a list of major.minor series as the "1.0.x"
// style shown in error and warning messages.
func FormatSupportedSeries(series []string) string {
	formatted := make([]string, len(series))
	for i, s := range series {
		formatted[i] = s + ".x"
	}
	return strings.Join(formatted, ", ")
}

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
	full := fields[2]
	if !contains(SupportedSeries, versionSeries(full)) {
		return "", &UnsupportedVersionError{Version: full, Supported: SupportedSeries}
	}
	return full, nil
}

// versionSeries extracts the major.minor series from a version string,
// dropping the patch component and any -rc.N / +build suffix.
func versionSeries(version string) string {
	if i := strings.IndexAny(version, "-+"); i >= 0 {
		version = version[:i]
	}
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}

func contains(series []string, target string) bool {
	for _, s := range series {
		if s == target {
			return true
		}
	}
	return false
}
