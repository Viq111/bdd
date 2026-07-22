package sourcebd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixturesParseToTypedOrRawRecords(t *testing.T) {
	fixtures, err := filepath.Glob("../../testdata/*.jsonl")
	if err != nil || len(fixtures) != 2 {
		t.Fatalf("fixtures = %v, %v", fixtures, err)
	}
	for _, fixture := range fixtures {
		data, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal(err)
		}
		records, err := ParseJSONL(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: %v", fixture, err)
		}
		if len(records) == 0 {
			t.Fatalf("%s had no records", fixture)
		}
		for _, record := range records {
			if record.RawJSON() == nil {
				t.Fatalf("%s did not retain raw data", record.Type())
			}
		}
		if issue, ok := records[0].(Issue); !ok || len(issue.Labels) == 0 || len(issue.Dependencies) == 0 || len(issue.Comments) == 0 {
			t.Fatalf("%s first record did not preserve issue fields: %#v", fixture, records[0])
		}
	}
}

func TestParseJSONLRejectsIncompatibleEnvelope(t *testing.T) {
	for _, input := range []string{"{}", "{\"_type\":12}", "{\"_type\":\"issue\"}"} {
		if _, err := ParseJSONL(bytes.NewBufferString(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestParseJSONLParsesBD103DependencyEndpoints(t *testing.T) {
	records, err := ParseJSONL(bytes.NewBufferString(`{"_type":"issue","id":"bdd-8urh","dependencies":[{"issue_id":"bdd-8urh","depends_on_id":"bdd-4s2w","type":"blocks"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	issue, ok := records[0].(Issue)
	if !ok || len(issue.Dependencies) != 1 {
		t.Fatalf("record = %#v, want issue with one dependency", records[0])
	}
	dependency := issue.Dependencies[0]
	if dependency.IssueID != "bdd-8urh" || dependency.DependsOnID != "bdd-4s2w" || dependency.Kind != "blocks" {
		t.Fatalf("dependency = %#v", dependency)
	}
}

func TestParseJSONLRetainsUnsupportedIssueAndContinues(t *testing.T) {
	input := `{"_type":"issue","id":"valid","labels":["ok"]}
{"_type":"issue","id":"unsupported","labels":"not-an-array"}
{"_type":"memory","key":"after","value":"still parsed"}`
	records, err := ParseJSONL(bytes.NewBufferString(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("record count = %d, want 3", len(records))
	}
	if _, ok := records[0].(Issue); !ok {
		t.Fatalf("first record = %T, want Issue", records[0])
	}
	if _, ok := records[1].(RawRecord); !ok {
		t.Fatalf("unsupported issue = %T, want RawRecord", records[1])
	}
	if _, ok := records[2].(Memory); !ok {
		t.Fatalf("record after unsupported issue = %T, want Memory", records[2])
	}
}
