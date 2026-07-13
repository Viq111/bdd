package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// sanitizeForTerminal neutralizes ASCII control bytes (other than the
// newline and tab used for structural formatting) in s. Card titles,
// descriptions, notes, and other free-text fields are arbitrary input
// supplied by whoever creates or updates a card; rendered unsanitized to a
// human-readable terminal, control bytes such as ESC (0x1B) let a card
// body inject terminal escape sequences into anyone running `bdd show` or
// `bdd list` (cursor manipulation, title-bar spoofing, or worse depending
// on the terminal emulator). JSON output is unaffected by this: it never
// calls sanitizeForTerminal, since encoding/json already escapes control
// bytes as \u00XX.
func sanitizeForTerminal(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return '�'
		}
		return r
	}, s)
}

// Streams carries the output destinations and rendering mode shared by
// every command: data goes to Stdout, diagnostics to Stderr, and JSON/
// Silent select the rendering the command should use. A command that
// succeeds must not write incidental diagnostics to Stderr; that
// restriction does not apply to failures, which always report on Stderr
// regardless of JSON or Silent.
type Streams struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
	JSON   bool
	Silent bool
}

// Errorf writes a formatted diagnostic to Stderr. Errors are reported
// unconditionally: JSON and Silent only affect success-path output.
func (s *Streams) Errorf(format string, args ...any) {
	fmt.Fprintf(s.Stderr, format, args...)
}

// JSONEncoder emits singular JSON results: one object per operation, no
// enclosing envelope.
type JSONEncoder struct {
	enc *json.Encoder
}

// NewJSONEncoder returns a JSONEncoder writing to w.
func NewJSONEncoder(w io.Writer) *JSONEncoder {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &JSONEncoder{enc: enc}
}

// Object encodes v as a single JSON value followed by a newline.
func (e *JSONEncoder) Object(v any) error {
	return e.enc.Encode(v)
}

// JSONArray streams a plural JSON result as a single array, one element at
// a time, so a command never has to buffer every result in memory before
// it can emit the first byte.
type JSONArray struct {
	w       io.Writer
	started bool
}

// NewJSONArray returns a JSONArray writing to w. Callers must call Close
// exactly once, even if WriteItem was never called (which emits "[]").
func NewJSONArray(w io.Writer) *JSONArray {
	return &JSONArray{w: w}
}

// WriteItem encodes v as the array's next element.
func (a *JSONArray) WriteItem(v any) error {
	sep := ","
	if !a.started {
		sep = "["
		a.started = true
	}
	if _, err := io.WriteString(a.w, sep); err != nil {
		return err
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}

	_, err := a.w.Write(bytes.TrimRight(buf.Bytes(), "\n"))
	return err
}

// Close terminates the array and writes a trailing newline. Called with no
// prior WriteItem, it emits an empty array ("[]"), never null.
func (a *JSONArray) Close() error {
	if !a.started {
		_, err := io.WriteString(a.w, "[]\n")
		return err
	}
	_, err := io.WriteString(a.w, "]\n")
	return err
}
