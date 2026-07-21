package warnings

import (
	"testing"

	"github.com/viq111/bdd/tools/migrate/internal/model"
)

func TestRenderSortsLinesAndReasonsWithoutMutatingInput(t *testing.T) {
	values := []model.Warning{{SourceID: "z", Reasons: []string{"second", "first"}}, {SourceID: "a", Reasons: []string{"only"}}}
	if got, want := Render(values), "warning: a: only\nwarning: z: first; second"; got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
	if values[0].Reasons[0] != "second" {
		t.Fatal("Render mutated warning reasons")
	}
}
