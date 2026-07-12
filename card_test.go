package bdd

import "testing"

func TestSixBuiltinTypes(t *testing.T) {
	types := []CardType{
		CardTypeBug, CardTypeTask, CardTypeFeature,
		CardTypeEpic, CardTypeDecision, CardTypeChore,
	}
	if len(types) != 6 {
		t.Fatalf("got %d built-in types, want 6", len(types))
	}
}

func TestSixBuiltinStatuses(t *testing.T) {
	if len(BuiltinStatusCategories) != 6 {
		t.Fatalf("got %d built-in statuses, want 6", len(BuiltinStatusCategories))
	}

	validCategories := map[StatusCategory]bool{
		StatusCategoryActive: true,
		StatusCategoryWIP:    true,
		StatusCategoryDone:   true,
		StatusCategoryFrozen: true,
	}
	for status, category := range BuiltinStatusCategories {
		if !validCategories[category] {
			t.Fatalf("status %q has unknown category %q", status, category)
		}
	}
}
