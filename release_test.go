package bdd

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestReleaseScriptSyntax catches shell syntax errors in scripts/release.sh
// without paying the cost of actually cross-compiling every platform in
// every test run (that's covered by manually running `make dist`; see
// docs/release.md).
func TestReleaseScriptSyntax(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	out, err := exec.Command("bash", "-n", "scripts/release.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("scripts/release.sh has a syntax error: %v\n%s", err, out)
	}
}

// TestReleaseVersionWiringMatchesMain guards the -ldflags -X path the
// Makefile and release script use to stamp cmd/bdd's version variable: if
// cmd/bdd/main.go's variable is ever renamed without updating the stamping
// commands, `bdd version` would silently keep reporting "dev" in release
// builds instead of the tagged version.
func TestReleaseVersionWiringMatchesMain(t *testing.T) {
	main, err := os.ReadFile("cmd/bdd/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), `var version = "dev"`) {
		t.Fatal(`cmd/bdd/main.go no longer declares "var version = \"dev\""; update the -ldflags -X main.version= target in the Makefile and scripts/release.sh to match`)
	}

	const ldflagsTarget = "-X main.version="

	makefile, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(makefile), ldflagsTarget) {
		t.Fatalf("Makefile no longer stamps %s; local `make build` binaries would report the wrong version", ldflagsTarget)
	}

	release, err := os.ReadFile("scripts/release.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(release), ldflagsTarget) {
		t.Fatalf("scripts/release.sh no longer stamps %s; release archives would report the wrong version", ldflagsTarget)
	}
}

// TestReleaseArchivesAreReproducible is the regression test for bd bdd-f8m:
// running the release script twice for the same commit and version used to
// produce six different SHA256SUMS, because build-time filesystem
// timestamps (directory creation time, archive-time) leaked into the tar.gz
// and zip entries. It runs the real script twice, with a delay in between
// so a timestamp leak would actually manifest, and asserts the checksums
// file is byte-identical both times.
func TestReleaseArchivesAreReproducible(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	if testing.Short() {
		t.Skip("cross-compiles six platforms twice; skipped in -short mode")
	}

	const version = "v0.0.0-repro-test"

	runRelease := func() []byte {
		t.Helper()
		cmd := exec.Command("bash", "scripts/release.sh")
		cmd.Env = append(os.Environ(), "VERSION="+version)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("scripts/release.sh failed: %v\n%s", err, out)
		}
		sums, err := os.ReadFile("dist/SHA256SUMS")
		if err != nil {
			t.Fatalf("reading dist/SHA256SUMS: %v", err)
		}
		return sums
	}

	first := runRelease()

	// The bug this guards against only shows up when build-time clocks
	// differ between runs, so sleep long enough for the wall clock to
	// visibly advance between builds.
	time.Sleep(2 * time.Second)

	second := runRelease()

	if string(first) != string(second) {
		t.Fatalf("repeat `make dist VERSION=%s` builds produced different SHA256SUMS:\n--- first ---\n%s\n--- second ---\n%s",
			version, first, second)
	}
}
