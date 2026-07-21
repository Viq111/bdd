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
