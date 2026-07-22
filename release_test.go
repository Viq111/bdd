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
// every test run (that's covered by manually running `./tools/build.sh --dist`; see
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
// tools/build.sh and release script use to stamp each main package's version
// variable. If either variable is renamed without updating the stamping
// commands, a built binary would silently keep reporting "dev".
func TestReleaseVersionWiringMatchesMain(t *testing.T) {
	for _, versionFile := range []string{"cmd/bdd/version.go", "tools/migrate/cmd/bdd-migration/version.go"} {
		contents, err := os.ReadFile(versionFile)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(contents), `var version = "dev"`) {
			t.Fatalf("%s no longer declares version; update the -ldflags -X main.version= target", versionFile)
		}
		if !strings.Contains(string(contents), `var commit = "unspecified"`) {
			t.Fatalf("%s no longer declares commit; update the -ldflags -X main.commit= target", versionFile)
		}
	}

	const ldflagsTarget = "-X main.version="
	const commitLDFlagsTarget = "-X main.commit="

	buildScript, err := os.ReadFile("tools/build.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, buildCommand := range []string{
		`go build -trimpath -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT" -o "$BIN_DIR/bdd" ./cmd/bdd`,
		`go build -trimpath -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT" -o "$BIN_DIR/bdd-migration" ./tools/migrate/cmd/bdd-migration`,
	} {
		if !strings.Contains(string(buildScript), buildCommand) {
			t.Fatalf("tools/build.sh no longer stamps %s: missing %q", ldflagsTarget, buildCommand)
		}
	}

	release, err := os.ReadFile("scripts/release.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(release), ldflagsTarget) {
		t.Fatalf("scripts/release.sh no longer stamps %s; release archives would report the wrong version", ldflagsTarget)
	}
	if !strings.Contains(string(release), commitLDFlagsTarget) {
		t.Fatalf("scripts/release.sh no longer stamps %s; release archives would report the wrong commit", commitLDFlagsTarget)
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
		t.Fatalf("repeat `./tools/build.sh --dist VERSION=%s` builds produced different SHA256SUMS:\n--- first ---\n%s\n--- second ---\n%s",
			version, first, second)
	}
}
