package warnings

import (
	"reflect"
	"strings"
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
	values := []model.Warning{{SourceID: "bad\r\nnext", Reasons: []string{"invalid bdd card ID; skipped record", "skipped dependency to target\r\nnext"}}}
	got := Render(values)
	if want := `warning: bad\r\nnext: invalid bdd card ID; skipped record; skipped dependency to target\r\nnext`; got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
	if strings.Count(got, "\n") != 0 {
		t.Fatalf("Render() emitted more than one physical line: %q", got)
	}
}

func TestRenderAggregatesDuplicateSourcesAndReasons(t *testing.T) {
	values := []model.Warning{
		{SourceID: "same", Reasons: []string{"second", "first", "first"}},
		{SourceID: "same", Reasons: []string{"third", "second"}},
	}
	if got, want := Render(values), "warning: same: first; second; third"; got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
	if got, want := values[0].Reasons, []string{"second", "first", "first"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Render mutated caller input: %#v", got)
	}
}
