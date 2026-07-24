package cli

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/viq111/bdd"
)

// TestGoDocHasNoAlwaysFailingSurface runs `go doc github.com/viq111/bdd` and
// checks its output for language a stubbed-out, always-failing method would
// carry ("not implemented", "TODO", "unsupported", "unimplemented"). It is
// a coarse text check layered on top of TestNoAlwaysFailingSurface's AST
// analysis (see surface_guard_test.go in the module root): that test proves
// no such method exists in source; this one proves the package's public
// documentation doesn't describe one either.
func TestGoDocHasNoAlwaysFailingSurface(t *testing.T) {
	out, err := exec.Command("go", "doc", "github.com/viq111/bdd").CombinedOutput()
	if err != nil {
		t.Fatalf("go doc github.com/viq111/bdd: %v\n%s", err, out)
	}
	lower := strings.ToLower(string(out))
	for _, marker := range []string{"not implemented", "unimplemented", "not yet supported", "todo:"} {
		if strings.Contains(lower, marker) {
			t.Errorf("go doc github.com/viq111/bdd contains %q, suggesting an exported member that always fails or is a stub:\n%s", marker, out)
		}
	}
}

// cardFieldAudit is a one-time, living audit (bd bdd-f4t0, finding 8.4) of
// every exported bdd.Card field: is it creatable at CreateCard time, mutable
// via UpdateCard/a lifecycle method, or read-only system/import metadata?
// Every field must fall into a documented category; TestCardFieldsAreAudited
// fails if a new field is added to Card without a matching entry here, so
// the audit can't silently go stale.
var cardFieldAudit = map[string]string{
	"ID":           "read-only: assigned by CreateCard, never settable afterward",
	"Title":        "creatable and mutable: CreateCard.Title, UpdateCard.Title",
	"Type":         "creatable and mutable: CreateCard.Type, UpdateCard.Type",
	"Status":       "mutable: UpdateCard.Status plus the claim/close/reopen/defer/human lifecycle methods",
	"Priority":     "creatable and mutable: CreateCard.Priority, UpdateCard.Priority",
	"Description":  "creatable and mutable: CreateCard.Description, UpdateCard.Description",
	"Reproduction": "creatable and mutable: CreateCard.Reproduction, UpdateCard.Reproduction",
	"Design":       "creatable and mutable: CreateCard.Design, UpdateCard.Design",
	"Acceptance":   "creatable and mutable: CreateCard.Acceptance, UpdateCard.Acceptance",
	"ExternalRef":  "creatable and mutable: CreateCard.ExternalRef, UpdateCard.ExternalRef",
	"Worktree":     "creatable and mutable: CreateCard.Worktree, UpdateCard.Worktree/ClearWorktree",
	"Assignee":     "mutable, but only through the claim/reopen lifecycle methods, not a free-form UpdateCard field",
	"Owner":        "read-only after creation: CreateCard.Owner is import/source metadata by design (see mutation.go), not in UpdateCard",
	"Labels":       "creatable and mutable: CreateCard.Labels, UpdateCard.AddLabels/RemoveLabels",
	"Parents":      "creatable and mutable: CreateCard.Parents, UpdateCard.AddParents/RemoveParents",
	"Children":     "mutable: UpdateCard.AddChildren/RemoveChildren (not settable at creation, only as someone else's parent edge)",
	"CreatedBy":    "read-only: CreateCard.CreatedBy is set once at creation, never updated",
	"CreatedAt":    "read-only: system-managed timestamp",
	"UpdatedAt":    "read-only: system-managed timestamp",
	"StartedAt":    "read-only field, but reachable indirectly: set by ClaimCard, cleared by ReopenCard",
	"ClosedAt":     "read-only field, but reachable indirectly: set by CloseCard/HumanCard, cleared by ReopenCard",
	"DeferUntil":   "mutable: DeferCard",
	"Revision":     "read-only: optimistic-concurrency counter, incremented by every mutation",
}

// TestCardFieldsAreAudited fails if bdd.Card gains or loses an exported
// field without cardFieldAudit being updated to match, keeping the finding
// 8.4 audit from silently going stale.
func TestCardFieldsAreAudited(t *testing.T) {
	typ := reflect.TypeOf(bdd.Card{})
	seen := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		seen[name] = true
		if _, ok := cardFieldAudit[name]; !ok {
			t.Errorf("bdd.Card.%s has no entry in cardFieldAudit; audit it (creatable/mutable/read-only) and add it there", name)
		}
	}
	for name := range cardFieldAudit {
		if !seen[name] {
			t.Errorf("cardFieldAudit has a stale entry %q; bdd.Card no longer has that field", name)
		}
	}
}

// jsonFieldAudit lists every default JSON field CardResult/CardSummaryResult
// emit, confirming none is a constant carried along for its own sake (the
// class of problem the removed `dispatchable` field was: see bd bdd-qumq).
// TestJSONFieldsAreUseful fails if a field is added without a matching
// entry, so a future constant-valued field can't slip back in unnoticed.
var jsonFieldAudit = map[string]bool{
	"id": true, "title": true, "type": true, "status": true, "priority": true,
	"worktree": true, "assignee": true, "owner": true, "labels": true,
	"description": true, "reproduction": true, "design": true, "acceptance": true,
	"external_ref": true, "parents": true, "children": true, "created_by": true,
	"created_at": true, "updated_at": true, "started_at": true, "closed_at": true,
	"defer_until": true, "revision": true,
}

var jsonSummaryFieldAudit = map[string]bool{
	"id": true, "title": true, "type": true, "status": true, "priority": true,
	"worktree": true, "assignee": true, "labels": true,
	"created_at": true, "updated_at": true,
}

func TestJSONFieldsAreUseful(t *testing.T) {
	assertJSONFieldsAudited(t, reflect.TypeOf(CardResult{}), jsonFieldAudit)
	assertJSONFieldsAudited(t, reflect.TypeOf(CardSummaryResult{}), jsonSummaryFieldAudit)
}

func assertJSONFieldsAudited(t *testing.T, typ reflect.Type, audited map[string]bool) {
	t.Helper()
	seen := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "" || name == "-" {
			continue
		}
		seen[name] = true
		if !audited[name] {
			t.Errorf("%s JSON field %q is not in the audit table; confirm it's useful to a normal CLI caller (not a constant like the removed dispatchable field) and add it there", typ.Name(), name)
		}
	}
	for name := range audited {
		if !seen[name] {
			t.Errorf("audit table has a stale entry %q; %s no longer emits that JSON field", name, typ.Name())
		}
	}
}
