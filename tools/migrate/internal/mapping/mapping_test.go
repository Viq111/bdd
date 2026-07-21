package mapping

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/viq111/bdd/tools/migrate/internal/sourcebd"
	"github.com/viq111/bdd/tools/migrate/internal/warnings"
)

func plan(t *testing.T, data string) (string, error) {
	t.Helper()
	r, err := sourcebd.ParseJSONL(bytes.NewBufferString(data))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Map(r, Config{StatusCategories: map[string]string{"verified": "done"}, CustomTypes: map[string]bool{"custom": true}})
	return warnings.Render(p.Warnings), err
}

func TestOrchaFixtureAccountsForRoleAttachments(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "orcha-bd-1.0.3.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := sourcebd.ParseJSONL(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Map(r, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Runes) != 1 || p.Runes[0].Key != "role/programmer" || len(p.Memories) != 1 {
		t.Fatalf("fixture plans: runes=%#v memories=%#v", p.Runes, p.Memories)
	}
	want := `warning: orcha-wisp-abc: skipped dependency kind "related" to orcha-related; skipped dependency to orcha-dep because role is imported as a rune; skipped role-attached comments because role is imported as a rune; skipped role-attached notes because role is imported as a rune`
	if got := warnings.Render(p.Warnings); got != want {
		t.Fatalf("fixture warning = %q, want %q", got, want)
	}
}

func TestOCPFixtureMapsEverySupportedRecord(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ocp-bd-1.0.3.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := sourcebd.ParseJSONL(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Map(r, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Cards) != 1 || len(p.Memories) != 1 || len(p.Notes) != 2 {
		t.Fatalf("fixture plans: cards=%#v memories=%#v notes=%#v", p.Cards, p.Memories, p.Notes)
	}
	want := "warning: ocp-123: skipped dependency kind \"parent-child\" to ocp-100; skipped dependency to ocp-122 because endpoint was not imported\nwarning: opaque-1: unsupported export record; skipped record"
	if got := warnings.Render(p.Warnings); got != want {
		t.Fatalf("fixture warning = %q, want %q", got, want)
	}
}

func TestMappingMalformedDuplicateAndAnonymousComments(t *testing.T) {
	data := `{"_type":"issue","id":"bad\n","title":"bad","status":"open","issue_type":"task"}
{"_type":"issue","id":"badrole","title":"[role] !!!","status":"open","issue_type":"role"}
{"_type":"issue","id":"role1","title":"[role] QA - team","status":"open","issue_type":"role"}
{"_type":"issue","id":"role2","title":"[role] qa — duplicate","status":"open","issue_type":"role"}
{"_type":"issue","id":"card","title":"card","status":"custom-status","issue_type":"custom-type","comments":[{"body":"same","author":"a","created_at":"2026-01-02T00:00:00Z"},{"body":"same","author":"a","created_at":"2026-01-02T00:00:00Z"},{"body":"later","author":"b","created_at":"2026-01-03T00:00:00Z"}]}`
	r, err := sourcebd.ParseJSONL(bytes.NewBufferString(data))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Map(r, Config{StatusCategories: map[string]string{"custom-status": "done"}, CustomTypes: map[string]bool{"custom-type": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Cards) != 1 || p.Cards[0].Status != "custom-status" || len(p.Workspace.Statuses) != 1 || len(p.Workspace.Types) != 1 {
		t.Fatalf("custom mapping %#v", p)
	}
	if len(p.Runes) != 1 || p.Runes[0].Metadata["legacy_bd_id"] != "role1" {
		t.Fatalf("role mapping %#v", p.Runes)
	}
	if len(p.Notes) != 2 || p.Notes[0].Body != "same" || p.Notes[1].Body != "later" {
		t.Fatalf("comment ordering %#v", p.Notes)
	}
	want := "warning: bad\n: invalid bdd card ID; skipped record\nwarning: badrole: role title does not produce a valid rune key; skipped record\nwarning: card: ambiguous identical comment; collapsed duplicate\nwarning: role2: duplicate rune key \"role/qa\"; skipped record"
	if got := warnings.Render(p.Warnings); got != want {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}

func TestCardFieldsReproductionNotesAndEdges(t *testing.T) {
	data := `{"_type":"issue","id":"child","title":"Bug","description":"intro\n## Steps TO reproduce\n1. run\n### detail\nx\n## Next\ny","status":"open","issue_type":"bug","priority":3,"assignee":"unclaimed","owner":"owner","created_by":"creator","external_ref":"ref","metadata":{"worktree":"wt"},"created_at":"2026-01-01T00:00:00+01:00","updated_at":null,"labels":["z","a"],"notes":"snapshot","comments":[{"id":"c","body":"comment","author":"me","created_at":"2026-01-02T00:00:00Z"}],"dependencies":[{"issue_id":"parent","type":"blocks"},{"issue_id":"other","type":"related"}]}
{"_type":"issue","id":"parent","title":"P","status":"open","issue_type":"task"}`
	r, _ := sourcebd.ParseJSONL(bytes.NewBufferString(data))
	p, err := Map(r, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Cards) != 2 || p.Cards[0].ID != "child" {
		t.Fatalf("cards %#v", p.Cards)
	}
	c := p.Cards[0]
	if c.Description == c.Reproduction || c.Reproduction != "1. run\n### detail\nx" || c.Assignee != "" || c.CreatedAt.UTC().Format("15:04") != "23:00" {
		t.Fatalf("card mapping %#v", c)
	}
	if len(p.Notes) != 2 || p.Notes[0].SourceKind != "notes" || len(p.Edges) != 1 || p.Edges[0].ParentID != "parent" || p.Edges[0].ChildID != "child" {
		t.Fatalf("notes/edges %#v %#v", p.Notes, p.Edges)
	}
	if got := warnings.Render(p.Warnings); got != `warning: child: skipped dependency kind "related" to other` {
		t.Fatalf("warnings %q", got)
	}
}

func TestRolesWarningsCyclesAndDeterminism(t *testing.T) {
	data := `{"_type":"issue","id":"r1","title":"[ROLE] Programmer — implementation","description":"body","status":"closed","issue_type":"role"}
{"_type":"issue","id":"r2","title":"[role] Programmer - duplicate","status":"open","issue_type":"role"}
{"_type":"issue","id":"a","title":"a","status":"verified","issue_type":"custom","dependencies":[{"issue_id":"b","type":"blocks"}]}
{"_type":"issue","id":"b","title":"b","status":"verified","issue_type":"custom","dependencies":[{"issue_id":"a","type":"blocks"}]}`
	r, _ := sourcebd.ParseJSONL(bytes.NewBufferString(data))
	_, err := Map(r, Config{StatusCategories: map[string]string{"verified": "done"}, CustomTypes: map[string]bool{"custom": true}})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle error %v", err)
	}
	data = strings.Replace(data, `"dependencies":[{"issue_id":"a","type":"blocks"}]`, `"dependencies":[]`, 1)
	r, _ = sourcebd.ParseJSONL(bytes.NewBufferString(data))
	p1, e := Map(r, Config{StatusCategories: map[string]string{"verified": "done"}, CustomTypes: map[string]bool{"custom": true}})
	if e != nil {
		t.Fatal(e)
	}
	p2, _ := Map(r, Config{StatusCategories: map[string]string{"verified": "done"}, CustomTypes: map[string]bool{"custom": true}})
	if len(p1.Runes) != 1 || p1.Runes[0].Key != "role/programmer" || p1.Runes[0].Enabled {
		t.Fatalf("runes %#v", p1.Runes)
	}
	if warnings.Render(p1.Warnings) != warnings.Render(p2.Warnings) || p1.Cards[0].Hash != p2.Cards[0].Hash {
		t.Fatal("mapping was not deterministic")
	}
}

func TestNoReproductionIsNotWarning(t *testing.T) {
	got, err := plan(t, `{"_type":"issue","id":"bug","title":"b","description":"no heading","status":"open","issue_type":"bug"}`)
	if err != nil || got != "" {
		t.Fatalf("%q %v", got, err)
	}
}
