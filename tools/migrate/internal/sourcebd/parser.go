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
	ID, Title, Description, Design, AcceptanceCriteria, Notes, Status, IssueType string
	Labels                                                                       []string
	Dependencies                                                                 []Dependency
	Comments                                                                     []Comment
}

// Dependency and Comment expose the parts of issue exports that the mapping
// layer needs first. Their Raw fields retain any later-added Beads fields.
type Dependency struct {
	// IssueID identifies the issue that owns this dependency record. In bd
	// 1.0.3, DependsOnID is the other endpoint of a blocks relationship.
	IssueID     string `json:"issue_id"`
	DependsOnID string `json:"depends_on_id"`
	Kind        string `json:"type"`
	Raw         map[string]json.RawMessage
}

type Comment struct {
	ID string `json:"id"`
	// Text is the comment content emitted by bd 1.0.3's live export.
	// Body remains as a compatibility fallback for the pinned fixtures.
	Text string `json:"text"`
	Body string `json:"body"`
	Raw  map[string]json.RawMessage
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
			// ID is part of the issue envelope.  Its absence or an incompatible
			// shape means the record cannot be identified, so fail the snapshot.
			// Other known fields belong to an individual issue and may evolve;
			// retain such an issue raw rather than discarding unrelated records.
			var envelope struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil || envelope.ID == "" {
				return nil, fmt.Errorf("export line %d: incompatible issue envelope: missing or invalid id", line)
			}
			var v struct {
				ID                 string       `json:"id"`
				Title              string       `json:"title"`
				Description        string       `json:"description"`
				Design             string       `json:"design"`
				AcceptanceCriteria string       `json:"acceptance_criteria"`
				Notes              string       `json:"notes"`
				Status             string       `json:"status"`
				IssueType          string       `json:"issue_type"`
				Labels             []string     `json:"labels"`
				Dependencies       []Dependency `json:"dependencies"`
				Comments           []Comment    `json:"comments"`
			}
			if err := json.Unmarshal(data, &v); err != nil {
				records = append(records, RawRecord{base})
				continue
			}
			for i := range v.Dependencies {
				v.Dependencies[i].Raw = rawObject(raw["dependencies"], i)
				// Early sanitized fixtures used issue_id as the endpoint. Keep
				// that representation readable while production uses 1.0.3's
				// explicit depends_on_id field.
				if v.Dependencies[i].DependsOnID == "" {
					v.Dependencies[i].DependsOnID = v.Dependencies[i].IssueID
				}
			}
			for i := range v.Comments {
				v.Comments[i].Raw = rawObject(raw["comments"], i)
				if _, ok := v.Comments[i].Raw["text"]; ok {
					v.Comments[i].Body = v.Comments[i].Text
				}
			}
			records = append(records, Issue{base, v.ID, v.Title, v.Description, v.Design, v.AcceptanceCriteria, v.Notes, v.Status, v.IssueType, v.Labels, v.Dependencies, v.Comments})
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

func rawObject(array json.RawMessage, index int) map[string]json.RawMessage {
	var objects []map[string]json.RawMessage
	if json.Unmarshal(array, &objects) != nil || index >= len(objects) {
		return nil
	}
	return objects[index]
}
