package cli

import (
	"os"
	"testing"
)

func TestResolveWorkspaceFlagWins(t *testing.T) {
	t.Setenv("BDD_WORKSPACE", "/env/ws")
	dir, source := ResolveWorkspace("/flag/ws")
	if dir != "/flag/ws" || source != "flag" {
		t.Fatalf("ResolveWorkspace() = (%q, %q), want (%q, %q)", dir, source, "/flag/ws", "flag")
	}
}

func TestResolveWorkspaceEnvFallback(t *testing.T) {
	t.Setenv("BDD_WORKSPACE", "/env/ws")
	dir, source := ResolveWorkspace("")
	if dir != "/env/ws" || source != "env" {
		t.Fatalf("ResolveWorkspace() = (%q, %q), want (%q, %q)", dir, source, "/env/ws", "env")
	}
}

func TestResolveWorkspaceEmptyEnvIgnored(t *testing.T) {
	t.Setenv("BDD_WORKSPACE", "")
	dir, source := ResolveWorkspace("")
	if dir != "" || source != "cwd" {
		t.Fatalf("ResolveWorkspace() = (%q, %q), want (%q, %q)", dir, source, "", "cwd")
	}
}

func TestResolveWorkspaceUnsetEnvFallsBackToCwd(t *testing.T) {
	prev, wasSet := os.LookupEnv("BDD_WORKSPACE")
	if err := os.Unsetenv("BDD_WORKSPACE"); err != nil {
		t.Fatalf("os.Unsetenv(BDD_WORKSPACE) error = %v", err)
	}
	t.Cleanup(func() {
		if wasSet {
			os.Setenv("BDD_WORKSPACE", prev)
		}
	})

	dir, source := ResolveWorkspace("")
	if dir != "" || source != "cwd" {
		t.Fatalf("ResolveWorkspace() = (%q, %q), want (%q, %q)", dir, source, "", "cwd")
	}
}
