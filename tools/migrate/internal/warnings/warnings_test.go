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

func TestRenderEscapesLineTerminatorsInSourceID(t *testing.T) {
	values := []model.Warning{{SourceID: "bad\r\nnext", Reasons: []string{"invalid bdd card ID; skipped record"}}}
	if got, want := Render(values), `warning: bad\r\nnext: invalid bdd card ID; skipped record`; got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}
