package mapping

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/viq111/bdd/tools/migrate/internal/model"
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
	assertFixtureRecordsAccounted(t, r, p)
	if len(p.Runes) != 1 || p.Runes[0].Key != "role/programmer" || len(p.Memories) != 1 {
		t.Fatalf("fixture plans: runes=%#v memories=%#v", p.Runes, p.Memories)
	}
	rune := p.Runes[0]
	if rune.Kind != "role" || rune.Title != "[role] Programmer" || rune.Body != "first line\nsecond line" || !rune.Enabled || !rune.Protected || !reflect.DeepEqual(rune.Metadata, map[string]string{"legacy_system": "beads", "legacy_bd_id": "orcha-wisp-abc", "legacy_status": "awaiting_review"}) {
		t.Fatalf("rune mapping = %#v", rune)
	}
	memory := p.Memories[0]
	if memory.Key != "orcha/agent" || memory.Body != "remember this" || memory.Actor != "bdd-migration" || memory.CreatedAt != nil {
		t.Fatalf("memory mapping = %#v", memory)
	}
	want := "warning: orcha-wisp-abc: skipped dependency kind \"related\" to orcha-related; skipped dependency to orcha-dep because role is imported as a rune; skipped role-attached comments because role is imported as a rune; skipped role-attached notes because role is imported as a rune\nwarning: workspace: built-in status definition \"awaiting_review\" requires no workspace plan; skipped record; built-in type definition \"role\" requires no workspace plan; skipped record"
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
	assertFixtureRecordsAccounted(t, r, p)
	if len(p.Cards) != 1 || len(p.Memories) != 1 || len(p.Notes) != 2 {
		t.Fatalf("fixture plans: cards=%#v memories=%#v notes=%#v", p.Cards, p.Memories, p.Notes)
	}
	card := p.Cards[0]
	if card.ID != "ocp-123" || card.Title != "Migrate OCP" || card.Description != "multiline\ndescription" || card.Design != "keep raw" || card.Acceptance != "all fields" || card.Reproduction != "" || card.Status != "open" || card.Type != "feature" || card.Priority != 0 || card.Assignee != "" || card.Owner != "" || card.Creator != "" || card.ExternalRef != "" || card.Worktree != "" || !reflect.DeepEqual(card.Labels, []string{"release", "日本語"}) || card.CreatedAt == nil || card.CreatedAt.UTC().Format(time.RFC3339Nano) != "2026-07-20T12:00:00Z" || card.UpdatedAt != nil || card.ClosedAt != nil || card.DeferUntil != nil {
		t.Fatalf("card mapping = %#v", card)
	}
	memory := p.Memories[0]
	if memory.Key != "ocp/migration" || memory.Body != "state" || memory.Actor != "bdd-migration" || memory.CreatedAt == nil || memory.CreatedAt.UTC().Format(time.RFC3339Nano) != "2026-07-20T12:00:00Z" {
		t.Fatalf("memory mapping = %#v", memory)
	}
	if got, want := p.Notes[1].SourceKey, "ocp-123/notes/78ec140f79eeb6a673f3f88f0b7bf66cd62b16923ea5d73a47f5474552f595bd"; got != want {
		t.Fatalf("notes source key = %q, want %q", got, want)
	}
	if note := p.Notes[0]; note.CardID != "ocp-123" || note.SourceKey != "ocp-123/comment/ocp-comment-1" || note.SourceKind != "comment" || note.SourceID != "ocp-comment-1" || note.Author != "" || note.Body != `{"kind":"decision"}` || note.CreatedAt != nil {
		t.Fatalf("comment mapping = %#v", note)
	}
	want := "warning: ocp-123: skipped dependency kind \"parent-child\" to ocp-100; skipped dependency to ocp-122 because endpoint was not imported\nwarning: opaque-1: unsupported export record; skipped record\nwarning: workspace: status definition \"verified\" has no category; skipped record"
	if got := warnings.Render(p.Warnings); got != want {
		t.Fatalf("fixture warning = %q, want %q", got, want)
	}
}

func TestFixtureMappingDeterminism(t *testing.T) {
	for _, fixture := range []string{"ocp-bd-1.0.3.jsonl", "orcha-bd-1.0.3.jsonl"} {
		t.Run(fixture, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", fixture))
			if err != nil {
				t.Fatal(err)
			}
			records, err := sourcebd.ParseJSONL(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			first, err := Map(records, Config{})
			if err != nil {
				t.Fatal(err)
			}
			second, err := Map(records, Config{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(canonicalHashes(first), canonicalHashes(second)) {
				t.Fatalf("canonical hashes differ:\nfirst=%#v\nsecond=%#v", canonicalHashes(first), canonicalHashes(second))
			}
			if warnings.Render(first.Warnings) != warnings.Render(second.Warnings) {
				t.Fatalf("warnings differ:\nfirst=%q\nsecond=%q", warnings.Render(first.Warnings), warnings.Render(second.Warnings))
			}
		})
	}
}

func canonicalHashes(p model.Plan) []string {
	var hashes []string
	for _, value := range p.Cards {
		hashes = append(hashes, "card:"+value.Hash)
	}
	for _, value := range p.Runes {
		hashes = append(hashes, "rune:"+value.Hash)
	}
	for _, value := range p.Memories {
		hashes = append(hashes, "memory:"+value.Hash)
	}
	for _, value := range p.Notes {
		hashes = append(hashes, "note:"+value.Hash)
	}
	for _, value := range p.Edges {
		hashes = append(hashes, "edge:"+value.Hash)
	}
	return hashes
}

func assertFixtureRecordsAccounted(t *testing.T, records []sourcebd.Record, p model.Plan) {
	t.Helper()
	for _, record := range records {
		switch value := record.(type) {
		case sourcebd.Issue:
			if hasCard(p, value.ID) || hasRune(p, value.ID) || hasWarning(p, value.ID) {
				continue
			}
			t.Errorf("issue %q has no plan value or warning", value.ID)
		case sourcebd.Memory:
			if !hasMemory(p, value.Key) {
				t.Errorf("memory %q has no plan value", value.Key)
			}
		case sourcebd.Infrastructure:
			if !hasWorkspaceDefinition(p, value.RawJSON()) && !hasWorkspaceWarningForDefinition(p, value.RawJSON()) {
				t.Errorf("infrastructure record %#v has no plan value or warning", value.RawJSON())
			}
		case sourcebd.RawRecord:
			id := rawString(value.RawJSON(), "id", "workspace")
			if !hasWarning(p, id) {
				t.Errorf("raw record %q has no warning", id)
			}
		}
	}
}

func hasCard(p model.Plan, id string) bool {
	for _, value := range p.Cards {
		if value.ID == id {
			return true
		}
	}
	return false
}

func hasRune(p model.Plan, id string) bool {
	for _, value := range p.Runes {
		if value.Metadata["legacy_bd_id"] == id {
			return true
		}
	}
	return false
}

func hasMemory(p model.Plan, key string) bool {
	for _, value := range p.Memories {
		if value.Key == key {
			return true
		}
	}
	return false
}

func hasWorkspaceDefinition(p model.Plan, raw map[string]json.RawMessage) bool {
	name := rawString(raw, "name", "")
	kind := rawString(raw, "kind", "")
	if kind == "" {
		_ = json.Unmarshal(raw["_type"], &kind)
	}
	if kind == "status" || kind == "custom_status" {
		for _, value := range p.Workspace.Statuses {
			if value.Name == name {
				return true
			}
		}
	}
	if kind == "type" || kind == "custom_type" {
		for _, value := range p.Workspace.Types {
			if value.Name == name {
				return true
			}
		}
	}
	return false
}

func hasWarning(p model.Plan, id string) bool {
	for _, value := range p.Warnings {
		if value.SourceID == id {
			return true
		}
	}
	return false
}

func hasWorkspaceWarningForDefinition(p model.Plan, raw map[string]json.RawMessage) bool {
	kind := rawString(raw, "kind", "")
	if kind == "" {
		_ = json.Unmarshal(raw["_type"], &kind)
	}
	name := rawString(raw, "name", "")
	var prefixes []string
	switch kind {
	case "status", "custom_status":
		prefixes = []string{"status definition \"" + name + "\"", "built-in status definition \"" + name + "\""}
	case "type", "custom_type":
		prefixes = []string{"built-in type definition \"" + name + "\""}
	default:
		prefixes = []string{"unsupported infrastructure record"}
	}
	for _, value := range p.Warnings {
		if value.SourceID != "workspace" {
			continue
		}
		for _, reason := range value.Reasons {
			for _, prefix := range prefixes {
				if strings.HasPrefix(reason, prefix) {
					return true
				}
			}
		}
	}
	return false
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
	if len(p.Cards) != 1 || p.Cards[0].Status != "custom-status" || !reflect.DeepEqual(p.Workspace.Statuses, []model.StatusPlan{{Name: "custom-status", Category: "done"}}) || !reflect.DeepEqual(p.Workspace.Types, []model.TypePlan{{Name: "custom-type"}}) {
		t.Fatalf("custom mapping %#v", p)
	}
	if len(p.Runes) != 1 || p.Runes[0].Metadata["legacy_bd_id"] != "role1" {
		t.Fatalf("role mapping %#v", p.Runes)
	}
	if len(p.Notes) != 2 || p.Notes[0].Body != "same" || p.Notes[1].Body != "later" {
		t.Fatalf("comment ordering %#v", p.Notes)
	}
	want := "warning: bad\\n: invalid bdd card ID; skipped record\nwarning: badrole: role title does not produce a valid rune key; skipped record\nwarning: card: ambiguous identical comment; collapsed duplicate\nwarning: role2: duplicate rune key \"role/qa\"; skipped record"
	if got := warnings.Render(p.Warnings); got != want {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}

func TestMappingSkipsRolesWithConflictingLegacyID(t *testing.T) {
	records, err := sourcebd.ParseJSONL(bytes.NewBufferString(`{"_type":"issue","id":"same-id","title":"[role] Programmer","status":"open","issue_type":"role"}
{"_type":"issue","id":"same-id","title":"[role] QA","status":"open","issue_type":"role"}`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Map(records, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Runes) != 0 {
		t.Fatalf("runes = %#v, want no ambiguous role imports", p.Runes)
	}
	want := "warning: same-id: conflicting legacy ID maps to multiple rune keys; skipped role for rune key \"role/programmer\"; conflicting legacy ID maps to multiple rune keys; skipped role for rune key \"role/qa\""
	if got := warnings.Render(p.Warnings); got != want {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
	reversed, err := sourcebd.ParseJSONL(bytes.NewBufferString(`{"_type":"issue","id":"same-id","title":"[role] QA","status":"open","issue_type":"role"}
{"_type":"issue","id":"same-id","title":"[role] Programmer","status":"open","issue_type":"role"}`))
	if err != nil {
		t.Fatal(err)
	}
	reversedPlan, err := Map(reversed, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := warnings.Render(reversedPlan.Warnings); got != want {
		t.Fatalf("reversed warnings = %q, want %q", got, want)
	}
}

func TestMappingCollapsesRepeatedRoleExportRecords(t *testing.T) {
	records, err := sourcebd.ParseJSONL(bytes.NewBufferString(`{"_type":"issue","id":"mig-role","title":"[role] Operator","description":"role body","status":"open","issue_type":"role"}
{"_type":"issue","id":"mig-role","title":"[role] Operator","description":"role body","status":"open","issue_type":"role"}`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Map(records, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Runes) != 1 {
		t.Fatalf("runes = %#v, want one collapsed role", p.Runes)
	}
	rune := p.Runes[0]
	if rune.Key != "role/operator" || !rune.Protected || rune.Metadata["legacy_bd_id"] != "mig-role" {
		t.Fatalf("rune = %#v", rune)
	}
	if got := warnings.Render(p.Warnings); got != "" {
		t.Fatalf("warnings = %q, want none", got)
	}
}

func TestMappingSkipsUnsupportedStatusAndTypes(t *testing.T) {
	data := `{"_type":"issue","id":"supported","title":"supported","status":"verified","issue_type":"custom"}
{"_type":"issue","id":"unknown-status","title":"unknown status","status":"missing-category","issue_type":"task"}
{"_type":"issue","id":"unsupported-type","title":"unsupported type","status":"open","issue_type":"unknown"}
{"_type":"issue","id":"infrastructure-type","title":"infrastructure type","status":"open","issue_type":"infrastructure"}`
	r, err := sourcebd.ParseJSONL(bytes.NewBufferString(data))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Map(r, Config{StatusCategories: map[string]string{"verified": "done"}, CustomTypes: map[string]bool{"custom": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Cards) != 1 || p.Cards[0].ID != "supported" || p.Cards[0].Status != "verified" || p.Cards[0].Type != "custom" {
		t.Fatalf("supported card mapping %#v", p.Cards)
	}
	if !reflect.DeepEqual(p.Workspace.Statuses, []model.StatusPlan{{Name: "verified", Category: "done"}}) || !reflect.DeepEqual(p.Workspace.Types, []model.TypePlan{{Name: "custom"}}) {
		t.Fatalf("workspace mapping %#v", p.Workspace)
	}
	want := "warning: infrastructure-type: unsupported issue type \"infrastructure\"; skipped record\nwarning: unknown-status: status \"missing-category\" has no category; skipped record\nwarning: unsupported-type: unsupported issue type \"unknown\"; skipped record"
	if got := warnings.Render(p.Warnings); got != want {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}

func TestCardFieldsReproductionNotesAndEdges(t *testing.T) {
	data := `{"_type":"issue","id":"child","title":"Bug","description":"intro\n## Steps TO reproduce\n1. run\n### detail\nx\n## Next\ny","status":"open","issue_type":"bug","priority":3,"assignee":"unclaimed","owner":"owner","created_by":"creator","external_ref":"ref","metadata":{"worktree":"wt"},"created_at":"2026-01-01T00:00:00+01:00","updated_at":null,"labels":["z","a"],"notes":"snapshot","comments":[{"id":"c","body":"comment","author":"me","created_at":"2026-01-02T00:00:00Z"}],"dependencies":[{"issue_id":"child","depends_on_id":"parent","type":"blocks"},{"issue_id":"child","depends_on_id":"other","type":"related"}]}
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
	if c.ID != "child" || c.Title != "Bug" || c.Description != "intro\n## Steps TO reproduce\n1. run\n### detail\nx\n## Next\ny" || c.Description == c.Reproduction || c.Reproduction != "1. run\n### detail\nx" || c.Design != "" || c.Acceptance != "" || c.Status != "open" || c.Type != "bug" || c.Priority != 3 || c.Assignee != "" || c.Owner != "owner" || c.Creator != "creator" || c.ExternalRef != "ref" || c.Worktree != "wt" || !reflect.DeepEqual(c.Labels, []string{"a", "z"}) || c.CreatedAt == nil || c.CreatedAt.UTC().Format(time.RFC3339Nano) != "2025-12-31T23:00:00Z" || c.UpdatedAt != nil || c.ClosedAt != nil || c.DeferUntil != nil {
		t.Fatalf("card mapping %#v", c)
	}
	if len(p.Notes) != 2 || p.Notes[0].CardID != "child" || p.Notes[0].SourceKind != "notes" || p.Notes[0].SourceID != "16a0eeb0791b6c92451fd284dd9f599e0a7dbe7f6ebea6e2d2d06c7f74aec112" || p.Notes[0].SourceKey != "child/notes/16a0eeb0791b6c92451fd284dd9f599e0a7dbe7f6ebea6e2d2d06c7f74aec112" || p.Notes[0].Body != "snapshot" || p.Notes[0].CreatedAt != nil || p.Notes[1].SourceKey != "child/comment/c" || p.Notes[1].Author != "me" || p.Notes[1].Body != "comment" || p.Notes[1].CreatedAt == nil || p.Notes[1].CreatedAt.UTC().Format(time.RFC3339Nano) != "2026-01-02T00:00:00Z" || len(p.Edges) != 1 || p.Edges[0].ParentID != "parent" || p.Edges[0].ChildID != "child" {
		t.Fatalf("notes/edges %#v %#v", p.Notes, p.Edges)
	}
	if got := warnings.Render(p.Warnings); got != `warning: child: skipped dependency kind "related" to other` {
		t.Fatalf("warnings %q", got)
	}
}

func TestRolesWarningsCyclesAndDeterminism(t *testing.T) {
	data := `{"_type":"issue","id":"r1","title":"[ROLE] Programmer — implementation","description":"body","status":"closed","issue_type":"role"}
{"_type":"issue","id":"r2","title":"[role] Programmer - duplicate","status":"open","issue_type":"role"}
{"_type":"issue","id":"a","title":"a","status":"verified","issue_type":"custom","dependencies":[{"issue_id":"a","depends_on_id":"b","type":"blocks"}]}
{"_type":"issue","id":"b","title":"b","status":"verified","issue_type":"custom","dependencies":[{"issue_id":"b","depends_on_id":"a","type":"blocks"}]}`
	r, _ := sourcebd.ParseJSONL(bytes.NewBufferString(data))
	_, err := Map(r, Config{StatusCategories: map[string]string{"verified": "done"}, CustomTypes: map[string]bool{"custom": true}})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle error %v", err)
	}
	data = strings.Replace(data, `"dependencies":[{"issue_id":"b","depends_on_id":"a","type":"blocks"}]`, `"dependencies":[]`, 1)
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

func TestMappingSkipsKeylessMemoriesAndDefaultsEmptyActors(t *testing.T) {
	records, err := sourcebd.ParseJSONL(bytes.NewBufferString(`{"_type":"memory","key":"","value":"lost"}
{"_type":"memory","key":"kept","value":"body","actor":""}`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Map(records, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Memories) != 1 || p.Memories[0].Key != "kept" || p.Memories[0].Body != "body" || p.Memories[0].Actor != "bdd-migration" {
		t.Fatalf("memory mapping = %#v", p.Memories)
	}
	if got, want := warnings.Render(p.Warnings), "warning: workspace: memory record has no key; skipped record"; got != want {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}

func TestMappingRejectsConflictingCustomStatusDefinitions(t *testing.T) {
	records, err := sourcebd.ParseJSONL(bytes.NewBufferString(`{"_type":"status","name":"verified","category":"active"}
{"_type":"status","name":"verified","category":"done"}`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Map(records, Config{})
	if err == nil || !strings.Contains(err.Error(), `incompatible status definitions for "verified"`) {
		t.Fatalf("Map() error = %v, want incompatible status definition error", err)
	}
	if len(p.Workspace.Statuses) != 0 {
		t.Fatalf("Map() workspace statuses = %#v, want no plan", p.Workspace.Statuses)
	}
}

func TestMappingDoesNotLeakFixtureDefinitionsIntoConfig(t *testing.T) {
	cfg := Config{
		StatusCategories:       map[string]string{},
		CustomTypes:            map[string]bool{},
		LegacyStatusCategories: map[string]string{"legacy": "done"},
	}
	wantCfg := Config{
		StatusCategories:       map[string]string{},
		CustomTypes:            map[string]bool{},
		LegacyStatusCategories: map[string]string{"legacy": "done"},
	}
	definitions, err := sourcebd.ParseJSONL(bytes.NewBufferString(`{"_type":"infrastructure","kind":"custom_status","name":"verified","category":"done"}
{"_type":"infrastructure","kind":"custom_type","name":"custom"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Map(definitions, cfg); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, wantCfg) {
		t.Fatalf("Map mutated config: got %#v, want %#v", cfg, wantCfg)
	}

	withoutDefinitions, err := sourcebd.ParseJSONL(bytes.NewBufferString(`{"_type":"issue","id":"custom-type","title":"custom type","status":"open","issue_type":"custom"}
{"_type":"issue","id":"custom-status","title":"custom status","status":"verified","issue_type":"task"}`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Map(withoutDefinitions, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Cards) != 0 {
		t.Fatalf("cards accepted with leaked definitions: %#v", p.Cards)
	}
	wantWarnings := "warning: custom-status: status \"verified\" has no category; skipped record\nwarning: custom-type: unsupported issue type \"custom\"; skipped record"
	if got := warnings.Render(p.Warnings); got != wantWarnings {
		t.Fatalf("warnings = %q, want %q", got, wantWarnings)
	}
}
