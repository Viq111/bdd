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

// TestReleaseVersionWiringMatchesMain guards the version scheme: the
// version literal in each main package is the single source of truth (bumped
// by hand per release), while commit is still stamped at build time via
// -ldflags -X main.commit=. If either goes out of sync, a built binary would
// report the wrong commit, or a release would reintroduce ldflags version
// stamping that fights the source literal.
func TestReleaseVersionWiringMatchesMain(t *testing.T) {
	for _, versionFile := range []string{"cmd/bdd/version.go", "tools/migrate/cmd/bdd-migration/version.go"} {
		contents, err := os.ReadFile(versionFile)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(contents), `var version = "0.1.0"`) {
			t.Fatalf("%s no longer declares version = \"0.1.0\"", versionFile)
		}
		if !strings.Contains(string(contents), `var commit = "unspecified"`) {
			t.Fatalf("%s no longer declares commit; update the -ldflags -X main.commit= target", versionFile)
		}
	}

	const versionLDFlagsTarget = "-X main.version="
	const commitLDFlagsTarget = "-X main.commit="

	buildScript, err := os.ReadFile("tools/build.sh")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(buildScript), versionLDFlagsTarget) {
		t.Fatalf("tools/build.sh stamps %s; version.go's literal should be the only source of truth", versionLDFlagsTarget)
	}
	for _, buildCommand := range []string{
		`go build -trimpath -ldflags "-X main.commit=$COMMIT" -o "$BIN_DIR/bdd" ./cmd/bdd`,
		`go build -trimpath -ldflags "-X main.commit=$COMMIT" -o "$BIN_DIR/bdd-migration" ./tools/migrate/cmd/bdd-migration`,
	} {
		if !strings.Contains(string(buildScript), buildCommand) {
			t.Fatalf("tools/build.sh no longer stamps %s: missing %q", commitLDFlagsTarget, buildCommand)
		}
	}

	release, err := os.ReadFile("scripts/release.sh")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(release), versionLDFlagsTarget) {
		t.Fatalf("scripts/release.sh stamps %s; version.go's literal should be the only source of truth", versionLDFlagsTarget)
	}
	if !strings.Contains(string(release), commitLDFlagsTarget) {
		t.Fatalf("scripts/release.sh no longer stamps %s; release archives would report the wrong commit", commitLDFlagsTarget)
	}
}

// TestReleaseArchivesAreReproducible is the regression test: running the
// release script twice for the same commit and version used to
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
