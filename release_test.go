package bdd

import (
	"os"
	"os/exec"
	"strings"
	"testing"
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
