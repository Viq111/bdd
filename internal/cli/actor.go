package cli

import (
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// ResolveActor resolves the actor recorded against mutations, following
// the fixed precedence: an explicit --actor flag, then BDD_ACTOR, then
// `git config user.name`, then the OS username. It returns "" if every
// source is empty or unavailable; callers that require a non-empty actor
// must check for that themselves.
func ResolveActor(flagActor string) string {
	if flagActor != "" {
		return flagActor
	}
	if v := os.Getenv("BDD_ACTOR"); v != "" {
		return v
	}
	if v := gitUserName(); v != "" {
		return v
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return ""
}

// gitUserName reads `git config user.name`, returning "" if git is
// unavailable, unconfigured, or fails for any other reason.
func gitUserName() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
