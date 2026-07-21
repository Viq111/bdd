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
