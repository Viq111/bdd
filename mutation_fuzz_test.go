package bdd

import (
	"encoding/json"
	"testing"
)

// FuzzCreateCardDecode feeds arbitrary JSON into CreateCard, the struct
// whose pointer-typed text fields (Description, Reproduction, Design,
// Acceptance) exist specifically to distinguish an omitted JSON property
// from one explicitly set to "". It checks two things: that
// decoding and validating never panics, and that the omitted-vs-empty
// distinction survives encoding/json's decoder exactly as CreateCard's
// contract requires (a present-but-empty key must decode to a non-nil
// pointer to "", and an absent key must decode to nil).
func FuzzCreateCardDecode(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"Title":"t","Type":"bug"}`,
		`{"Title":"t","Type":"bug","Reproduction":"","Acceptance":""}`,
		`{"Title":"t","Type":"bug","Reproduction":null,"Acceptance":""}`,
		`{"Title":"t","Type":"task","Acceptance":""}`,
		`{"Title":"t","Type":"decision","Description":"","Design":""}`,
		`{"Title":"","Type":""}`,
		`{"Priority":-1}`,
		`{"Labels":["a","",null]}`,
		`{"Parents":123}`,
		`null`,
		`[]`,
		`"just a string"`,
		`{"Title":`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var in CreateCard
		if err := json.Unmarshal(data, &in); err != nil {
			return
		}

		// requiredCreateFields must never panic on any decodable input, and
		// must never treat a present empty-string field as missing.
		_ = requiredCreateFields(in)

		var probe struct {
			Description  *string
			Reproduction *string
			Design       *string
			Acceptance   *string
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			return
		}
		if probe.Description != nil && in.Description == nil {
			t.Fatalf("decode lost a present Description field: %s", data)
		}
		if probe.Reproduction != nil && in.Reproduction == nil {
			t.Fatalf("decode lost a present Reproduction field: %s", data)
		}
		if probe.Design != nil && in.Design == nil {
			t.Fatalf("decode lost a present Design field: %s", data)
		}
		if probe.Acceptance != nil && in.Acceptance == nil {
			t.Fatalf("decode lost a present Acceptance field: %s", data)
		}
	})
}

// FuzzUpdateCardDecode feeds arbitrary JSON into UpdateCard and runs
// validateUpdateCard over the result, checking only that validation never
// panics regardless of what shape of input reaches it.
func FuzzUpdateCardDecode(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"Title":""}`,
		`{"Title":"x"}`,
		`{"Priority":-1}`,
		`{"ClearWorktree":true,"Worktree":""}`,
		`{"AddParents":[""]}`,
		`{"AddLabels":["", "ok"]}`,
		`null`,
		`{"Status":"bogus"}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var in UpdateCard
		if err := json.Unmarshal(data, &in); err != nil {
			return
		}
		_ = validateUpdateCard(in)
	})
}
