package cli

import "testing"

func TestResolveActorFlagWins(t *testing.T) {
	t.Setenv("BDD_ACTOR", "env-actor")
	if got := ResolveActor("flag-actor"); got != "flag-actor" {
		t.Fatalf("ResolveActor() = %q, want %q", got, "flag-actor")
	}
}

func TestResolveActorEnvFallback(t *testing.T) {
	t.Setenv("BDD_ACTOR", "env-actor")
	if got := ResolveActor(""); got != "env-actor" {
		t.Fatalf("ResolveActor() = %q, want %q", got, "env-actor")
	}
}
