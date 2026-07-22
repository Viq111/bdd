package model

import (
	"reflect"
	"testing"
	"time"
)

func TestCanonicalizeIsStableAndExcludesHashFields(t *testing.T) {
	when := time.Date(2026, 7, 21, 12, 0, 0, 0, time.FixedZone("source", -4*60*60))
	p := Plan{
		Cards:    []CardPlan{{ID: "b", Labels: []string{"z", "a"}, CreatedAt: &when, Hash: "old-card"}},
		Runes:    []RunePlan{{Key: "role/z", Hash: "old-rune"}},
		Memories: []MemoryPlan{{Key: "memory", Hash: "old-memory"}},
		Notes:    []NotePlan{{CardID: "b", SourceKind: "notes", SourceID: "id", Hash: "old-note"}},
		Edges:    []EdgePlan{{ParentID: "a", ChildID: "b", Hash: "old-edge"}},
	}
	p.Canonicalize()
	first := p
	p.Canonicalize()
	if !reflect.DeepEqual(first, p) {
		t.Fatalf("Canonicalize changed the plan on a second call:\nfirst=%#v\nsecond=%#v", first, p)
	}
	if p.Cards[0].HashVersion != HashVersion || p.Cards[0].Labels[0] != "a" {
		t.Fatalf("card not canonicalized: %#v", p.Cards[0])
	}
}

func TestCanonicalizeDeduplicatesEdgesAndAggregatesWarnings(t *testing.T) {
	p := Plan{
		Edges: []EdgePlan{{ParentID: "a", ChildID: "b"}, {ParentID: "a", ChildID: "b"}},
		Warnings: []Warning{
			{SourceID: "z", Reasons: []string{"second", "first"}},
			{SourceID: "a", Reasons: []string{"only"}},
			{SourceID: "z", Reasons: []string{"first"}},
			{Reasons: []string{"workspace reason"}},
		},
	}

	p.Canonicalize()

	if len(p.Edges) != 1 || p.Edges[0].ParentID != "a" || p.Edges[0].ChildID != "b" {
		t.Fatalf("edges = %#v", p.Edges)
	}
	if got, want := p.Warnings, []Warning{
		{SourceID: "a", Reasons: []string{"only"}},
		{SourceID: "workspace", Reasons: []string{"workspace reason"}},
		{SourceID: "z", Reasons: []string{"first", "second"}},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %#v, want %#v", got, want)
	}
}
