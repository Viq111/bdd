package cli

import (
	"bytes"
	"testing"
)

func TestJSONEncoderObject(t *testing.T) {
	var buf bytes.Buffer
	err := NewJSONEncoder(&buf).Object(map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("Object() error = %v", err)
	}
	if got, want := buf.String(), "{\"a\":1}\n"; got != want {
		t.Fatalf("Object() = %q, want %q", got, want)
	}
}

func TestJSONArrayEmpty(t *testing.T) {
	var buf bytes.Buffer
	a := NewJSONArray(&buf)
	if err := a.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got, want := buf.String(), "[]\n"; got != want {
		t.Fatalf("empty array = %q, want %q", got, want)
	}
}

func TestJSONArrayMultipleItems(t *testing.T) {
	var buf bytes.Buffer
	a := NewJSONArray(&buf)
	if err := a.WriteItem(map[string]any{"id": 1}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	if err := a.WriteItem(map[string]any{"id": 2}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	want := `[{"id":1},{"id":2}]` + "\n"
	if got := buf.String(); got != want {
		t.Fatalf("array = %q, want %q", got, want)
	}
}
