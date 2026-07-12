package bdd

import (
	"context"
	"errors"
	"testing"
)

func TestConfigSetGetUnset(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.ConfigSet(ctx, "foo", "bar", "alice"); err != nil {
		t.Fatalf("ConfigSet() error = %v", err)
	}
	got, err := db.ConfigGet(ctx, "foo")
	if err != nil {
		t.Fatalf("ConfigGet() error = %v", err)
	}
	if got != "bar" {
		t.Fatalf("ConfigGet() = %q, want %q", got, "bar")
	}

	list, err := db.ConfigList(ctx)
	if err != nil {
		t.Fatalf("ConfigList() error = %v", err)
	}
	if len(list) != 1 || list[0].Key != "foo" || list[0].Value != "bar" {
		t.Fatalf("ConfigList() = %+v, want [{foo bar}]", list)
	}

	if err := db.ConfigSet(ctx, "foo", "baz", "alice"); err != nil {
		t.Fatalf("ConfigSet() overwrite error = %v", err)
	}
	got, err = db.ConfigGet(ctx, "foo")
	if err != nil || got != "baz" {
		t.Fatalf("ConfigGet() after overwrite = (%q, %v), want (baz, nil)", got, err)
	}

	if err := db.ConfigUnset(ctx, "foo", "alice"); err != nil {
		t.Fatalf("ConfigUnset() error = %v", err)
	}
	if _, err := db.ConfigGet(ctx, "foo"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConfigGet() after unset error = %v, want ErrNotFound", err)
	}
}

func TestConfigGetMissingReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.ConfigGet(ctx, "never-set"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConfigGet() error = %v, want ErrNotFound", err)
	}
}

func TestStatusCustomAddsToStatuses(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.ConfigSet(ctx, ConfigKeyStatusCustom, "qa_testing:wip,on_hold:frozen", "alice"); err != nil {
		t.Fatalf("ConfigSet(status.custom) error = %v", err)
	}

	got, err := db.ConfigGet(ctx, ConfigKeyStatusCustom)
	if err != nil || got != "qa_testing:wip,on_hold:frozen" {
		t.Fatalf("ConfigGet(status.custom) = (%q, %v)", got, err)
	}

	statuses, err := db.Statuses(ctx)
	if err != nil {
		t.Fatalf("Statuses() error = %v", err)
	}
	byName := map[Status]StatusDefinition{}
	for _, s := range statuses {
		byName[s.Name] = s
	}
	if len(statuses) != 8 { // 6 built-in + 2 custom
		t.Fatalf("got %d statuses, want 8: %+v", len(statuses), statuses)
	}
	if s := byName["qa_testing"]; s.Category != StatusCategoryWIP || s.BuiltIn {
		t.Fatalf("qa_testing = %+v, want category wip, built_in false", s)
	}
	if s := byName["on_hold"]; s.Category != StatusCategoryFrozen || s.BuiltIn {
		t.Fatalf("on_hold = %+v, want category frozen, built_in false", s)
	}

	// Replacing the category of an existing custom status (not removing
	// it) is allowed even while unused.
	if err := db.ConfigSet(ctx, ConfigKeyStatusCustom, "qa_testing:done,on_hold:frozen", "alice"); err != nil {
		t.Fatalf("ConfigSet(status.custom) category change error = %v", err)
	}
	statuses, err = db.Statuses(ctx)
	if err != nil {
		t.Fatalf("Statuses() error = %v", err)
	}
	for _, s := range statuses {
		if s.Name == "qa_testing" && s.Category != StatusCategoryDone {
			t.Fatalf("qa_testing category = %q, want done", s.Category)
		}
	}
}

func TestStatusCustomValidation(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		value string
	}{
		{"malformed token", "qa_testing"},
		{"bad category", "qa_testing:bogus"},
		{"reserved built-in name", "open:wip"},
		{"invalid charset", "QA-Testing:wip"},
		{"duplicate name", "qa_testing:wip,qa_testing:frozen"},
		{"empty entry", "qa_testing:wip,,on_hold:frozen"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.ConfigSet(ctx, ConfigKeyStatusCustom, tt.value, "alice")
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("ConfigSet(status.custom=%q) error = %v, want ErrInvalidArgument", tt.value, err)
			}
			if _, getErr := db.ConfigGet(ctx, ConfigKeyStatusCustom); !errors.Is(getErr, ErrNotFound) {
				t.Fatalf("ConfigGet(status.custom) after rejected set = %v, want ErrNotFound (no partial write)", getErr)
			}
		})
	}
}

func TestStatusCustomRemovalBlockedWhenInUse(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.ConfigSet(ctx, ConfigKeyStatusCustom, "qa_testing:wip", "alice"); err != nil {
		t.Fatalf("ConfigSet(status.custom) error = %v", err)
	}

	card, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	qaTesting := Status("qa_testing")
	if _, err := db.UpdateCard(ctx, card.ID, UpdateCard{Status: &qaTesting, Actor: "alice"}); err != nil {
		t.Fatalf("UpdateCard(status=qa_testing) error = %v", err)
	}

	if err := db.ConfigUnset(ctx, ConfigKeyStatusCustom, "alice"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ConfigUnset(status.custom) while in use error = %v, want ErrInvalidArgument", err)
	}
	if err := db.ConfigSet(ctx, ConfigKeyStatusCustom, "on_hold:frozen", "alice"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ConfigSet(status.custom) dropping in-use status error = %v, want ErrInvalidArgument", err)
	}

	// The rejected writes must not have touched status_definitions or
	// config: qa_testing is still valid to filter/read by.
	got, err := db.ConfigGet(ctx, ConfigKeyStatusCustom)
	if err != nil || got != "qa_testing:wip" {
		t.Fatalf("ConfigGet(status.custom) after rejected unset/set = (%q, %v), want (qa_testing:wip, nil)", got, err)
	}
}

func TestStatusCustomCategoryChangeBlockedWhenInUse(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.ConfigSet(ctx, ConfigKeyStatusCustom, "qa_testing:wip", "alice"); err != nil {
		t.Fatalf("ConfigSet(status.custom) error = %v", err)
	}

	card, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardTypeChore, CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard() error = %v", err)
	}
	qaTesting := Status("qa_testing")
	if _, err := db.UpdateCard(ctx, card.ID, UpdateCard{Status: &qaTesting, Actor: "alice"}); err != nil {
		t.Fatalf("UpdateCard(status=qa_testing) error = %v", err)
	}

	if err := db.ConfigSet(ctx, ConfigKeyStatusCustom, "qa_testing:done", "alice"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ConfigSet(status.custom) reclassifying in-use status error = %v, want ErrInvalidArgument", err)
	}

	// The rejected write must not have touched status_definitions or config.
	got, err := db.ConfigGet(ctx, ConfigKeyStatusCustom)
	if err != nil || got != "qa_testing:wip" {
		t.Fatalf("ConfigGet(status.custom) after rejected category change = (%q, %v), want (qa_testing:wip, nil)", got, err)
	}
	statuses, err := db.Statuses(ctx)
	if err != nil {
		t.Fatalf("Statuses() error = %v", err)
	}
	for _, s := range statuses {
		if s.Name == "qa_testing" && s.Category != StatusCategoryWIP {
			t.Fatalf("qa_testing category = %q, want wip (unchanged)", s.Category)
		}
	}
}

func TestTypesCustomAddsToTypes(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.ConfigSet(ctx, ConfigKeyTypesCustom, "incident,experiment", "alice"); err != nil {
		t.Fatalf("ConfigSet(types.custom) error = %v", err)
	}

	types, err := db.Types(ctx)
	if err != nil {
		t.Fatalf("Types() error = %v", err)
	}
	if len(types) != 8 { // 6 built-in + 2 custom
		t.Fatalf("got %d types, want 8: %+v", len(types), types)
	}

	// Custom types carry no extra required-field rules in v1.
	card, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardType("incident"), CreatedBy: "alice"})
	if err != nil {
		t.Fatalf("CreateCard(type=incident) error = %v", err)
	}
	if card.Type != "incident" {
		t.Fatalf("card.Type = %q, want incident", card.Type)
	}
}

func TestTypesCustomValidation(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		value string
	}{
		{"reserved built-in name", "task"},
		{"invalid charset", "Incident"},
		{"duplicate name", "incident,incident"},
		{"empty entry", "incident,,experiment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := db.ConfigSet(ctx, ConfigKeyTypesCustom, tt.value, "alice"); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("ConfigSet(types.custom=%q) error = %v, want ErrInvalidArgument", tt.value, err)
			}
		})
	}
}

func TestTypesCustomRemovalBlockedWhenInUse(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.ConfigSet(ctx, ConfigKeyTypesCustom, "incident", "alice"); err != nil {
		t.Fatalf("ConfigSet(types.custom) error = %v", err)
	}
	if _, err := db.CreateCard(ctx, CreateCard{Title: "x", Type: CardType("incident"), CreatedBy: "alice"}); err != nil {
		t.Fatalf("CreateCard(type=incident) error = %v", err)
	}

	if err := db.ConfigUnset(ctx, ConfigKeyTypesCustom, "alice"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ConfigUnset(types.custom) while in use error = %v, want ErrInvalidArgument", err)
	}
}
