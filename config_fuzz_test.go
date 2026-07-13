package bdd

import "testing"

// FuzzParseStatusCustom exercises the status.custom grammar
// ("name:category,name:category,...") with arbitrary input, checking only
// that it never panics and that every successfully parsed entry re-passes
// its own validation (name charset, non-built-in, valid category).
func FuzzParseStatusCustom(f *testing.F) {
	seeds := []string{
		"",
		"   ",
		"triage:active",
		"triage:active,blocked:frozen",
		"triage:active,triage:active",
		"open:active",
		"Triage:active",
		"triage:bogus",
		"triage",
		"triage:",
		":active",
		",",
		"triage:active,",
		"a:active,a:wip",
		"triage:active:extra",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, value string) {
		entries, err := parseStatusCustom(value)
		if err != nil {
			return
		}
		seen := make(map[string]bool)
		for _, e := range entries {
			if !configDefNamePattern.MatchString(e.Name) {
				t.Fatalf("parseStatusCustom(%q) accepted invalid name %q", value, e.Name)
			}
			if isBuiltinStatus(e.Name) {
				t.Fatalf("parseStatusCustom(%q) accepted reserved built-in name %q", value, e.Name)
			}
			if !validStatusCategories[e.Category] {
				t.Fatalf("parseStatusCustom(%q) accepted invalid category %q", value, e.Category)
			}
			if seen[e.Name] {
				t.Fatalf("parseStatusCustom(%q) returned duplicate name %q", value, e.Name)
			}
			seen[e.Name] = true
		}
	})
}

// FuzzParseTypesCustom exercises the types.custom grammar ("name,name,...")
// with arbitrary input, checking only that it never panics and that every
// successfully parsed entry re-passes its own validation.
func FuzzParseTypesCustom(f *testing.F) {
	seeds := []string{
		"",
		"   ",
		"spike",
		"spike,research",
		"spike,spike",
		"bug",
		"Spike",
		"spike ",
		",",
		"spike,",
		"a,a",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, value string) {
		names, err := parseTypesCustom(value)
		if err != nil {
			return
		}
		seen := make(map[string]bool)
		for _, n := range names {
			if !configDefNamePattern.MatchString(n) {
				t.Fatalf("parseTypesCustom(%q) accepted invalid name %q", value, n)
			}
			if isBuiltinType(n) {
				t.Fatalf("parseTypesCustom(%q) accepted reserved built-in name %q", value, n)
			}
			if seen[n] {
				t.Fatalf("parseTypesCustom(%q) returned duplicate name %q", value, n)
			}
			seen[n] = true
		}
	})
}
