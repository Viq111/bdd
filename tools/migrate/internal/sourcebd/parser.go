package sourcebd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Record is an export record. RawJSON retains every source field for diagnostics.
type Record interface {
	Type() string
	RawJSON() map[string]json.RawMessage
}
type baseRecord struct {
	Kind string
	Raw  map[string]json.RawMessage
}

func (r baseRecord) Type() string                        { return r.Kind }
func (r baseRecord) RawJSON() map[string]json.RawMessage { return r.Raw }

type Issue struct {
	baseRecord
	ID, Title string
}
type Memory struct {
	baseRecord
	Key, Value string
}
type Infrastructure struct{ baseRecord }

// RawRecord is retained for individually unsupported, well-formed records.
type RawRecord struct{ baseRecord }

// ParseJSONL parses the version-pinned export envelope. A malformed envelope is
// fatal; a valid but unsupported record remains available to the mapping layer.
func ParseJSONL(input io.Reader) ([]Record, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	var records []Record
	for line := 1; scanner.Scan(); line++ {
		data := bytes.TrimSpace(scanner.Bytes())
		if len(data) == 0 {
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("export line %d: invalid JSON: %w", line, err)
		}
		kindRaw, ok := raw["_type"]
		if !ok {
			return nil, fmt.Errorf("export line %d: incompatible envelope: missing _type", line)
		}
		var kind string
		if err := json.Unmarshal(kindRaw, &kind); err != nil || kind == "" {
			return nil, fmt.Errorf("export line %d: incompatible envelope: _type must be a string", line)
		}
		base := baseRecord{Kind: kind, Raw: raw}
		switch kind {
		case "issue":
			var v struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			}
			_ = json.Unmarshal(data, &v)
			if v.ID == "" {
				return nil, fmt.Errorf("export line %d: incompatible issue envelope: missing id", line)
			}
			records = append(records, Issue{base, v.ID, v.Title})
		case "memory":
			var v struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}
			_ = json.Unmarshal(data, &v)
			records = append(records, Memory{base, v.Key, v.Value})
		case "infrastructure", "config", "status", "type":
			records = append(records, Infrastructure{base})
		default:
			records = append(records, RawRecord{base})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read export: %w", err)
	}
	return records, nil
}
