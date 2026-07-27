package bdd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"go.yaml.in/yaml/v3"
)

// hooksFileName is the fixed name of the hooks configuration file, discovered
// in the same .bdd/ directory that holds the database.
const hooksFileName = "hooks.yaml"

// hooksSchemaVersion is the only "version" value hooks.yaml currently
// accepts.
const hooksSchemaVersion = 1

// ConfigKeyHooksEnabled is the config key gating whether a present, valid
// hooks.yaml is actually consulted. It defaults to false: hooks.yaml is
// versioned and can execute arbitrary commands, so a fresh clone must never
// auto-execute it just by virtue of the file existing.
const ConfigKeyHooksEnabled = "hooks.enabled"

// HookEvent identifies the kind of card change a hook fires on.
type HookEvent string

// The event kinds hooks.yaml accepts in its v1 schema.
const (
	HookEventStatusChange HookEvent = "status-change"
	HookEventLabelChange  HookEvent = "label-change"
)

// Hook is one parsed and validated entry from hooks.yaml.
type Hook struct {
	// Event is the event kind this hook fires on.
	Event HookEvent

	// FromStatus and ToStatus filter a status-change hook; empty means
	// match any status. Values within a filter are OR'ed.
	FromStatus []string
	ToStatus   []string

	// IssueType filters either event kind by card type; empty means match
	// any type. Values within the filter are OR'ed.
	IssueType []string

	// Added and Removed filter a label-change hook by the labels the event
	// added or removed; empty means match any delta. Values within a
	// filter are OR'ed.
	Added   []string
	Removed []string

	// Command is the argv to run, never empty.
	Command []string

	// Timeout bounds how long the command may run. Defaults to 10s when
	// hooks.yaml omits it.
	Timeout time.Duration
}

// HooksFile is the parsed, validated content of a hooks.yaml.
type HooksFile struct {
	// Version is the schema version the file declared.
	Version int

	// Hooks lists every hook, in file order.
	Hooks []Hook
}

// HooksPath returns the path hooks.yaml would live at for this workspace:
// alongside the database file db discovered on Open, in the same .bdd/
// directory. This performs no additional directory discovery of its own.
func (db *DB) HooksPath() string {
	return filepath.Join(filepath.Dir(db.path), hooksFileName)
}

// HooksEnabled reports whether the hooks.enabled config key is set to a
// truthy value. A key that has never been set, or holds an unparseable
// value, is treated as disabled: hooks.yaml requires an explicit opt-in per
// clone.
func (db *DB) HooksEnabled(ctx context.Context) (bool, error) {
	value, err := db.ConfigGet(ctx, ConfigKeyHooksEnabled)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	enabled, _ := strconv.ParseBool(value)
	return enabled, nil
}

// LoadHooksFile reads and strictly parses the hooks.yaml at path. A missing
// file is not an error: it yields a zero-value HooksFile (no hooks). Any
// other read or validation failure names path and, for a per-hook error,
// the offending hook's index.
func LoadHooksFile(path string) (*HooksFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &HooksFile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("bdd: hooks.yaml %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("bdd: hooks.yaml %s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		return &HooksFile{}, nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("bdd: hooks.yaml %s: top-level document must be a mapping", path)
	}

	var version int
	var versionSeen bool
	var hooksNode *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode, valNode := root.Content[i], root.Content[i+1]
		switch keyNode.Value {
		case "version":
			if err := valNode.Decode(&version); err != nil {
				return nil, fmt.Errorf("bdd: hooks.yaml %s: version: %w", path, err)
			}
			versionSeen = true
		case "hooks":
			hooksNode = valNode
		default:
			return nil, fmt.Errorf("bdd: hooks.yaml %s: unknown top-level key %q", path, keyNode.Value)
		}
	}
	if !versionSeen || version != hooksSchemaVersion {
		return nil, fmt.Errorf("bdd: hooks.yaml %s: unrecognized version %d (want %d)", path, version, hooksSchemaVersion)
	}

	var hooks []Hook
	if hooksNode != nil {
		if hooksNode.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("bdd: hooks.yaml %s: hooks must be a list", path)
		}
		for i, hookNode := range hooksNode.Content {
			hook, err := parseHook(hookNode)
			if err != nil {
				return nil, fmt.Errorf("bdd: hooks.yaml %s: hook %d: %w", path, i, err)
			}
			hooks = append(hooks, hook)
		}
	}

	return &HooksFile{Version: version, Hooks: hooks}, nil
}

// parseHook validates and decodes one entry of the hooks.yaml "hooks" list.
func parseHook(node *yaml.Node) (Hook, error) {
	if node.Kind != yaml.MappingNode {
		return Hook{}, fmt.Errorf("must be a mapping")
	}

	var eventNode, fromStatusNode, toStatusNode, issueTypeNode, addedNode, removedNode, commandNode, timeoutNode *yaml.Node
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
		switch keyNode.Value {
		case "event":
			eventNode = valNode
		case "from_status":
			fromStatusNode = valNode
		case "to_status":
			toStatusNode = valNode
		case "issue_type":
			issueTypeNode = valNode
		case "added":
			addedNode = valNode
		case "removed":
			removedNode = valNode
		case "command":
			commandNode = valNode
		case "timeout":
			timeoutNode = valNode
		default:
			return Hook{}, fmt.Errorf("unknown key %q", keyNode.Value)
		}
	}

	if eventNode == nil {
		return Hook{}, fmt.Errorf("missing required key \"event\"")
	}
	var eventStr string
	if err := eventNode.Decode(&eventStr); err != nil {
		return Hook{}, fmt.Errorf("event: %w", err)
	}

	var h Hook
	switch HookEvent(eventStr) {
	case HookEventStatusChange, HookEventLabelChange:
		h.Event = HookEvent(eventStr)
	default:
		return Hook{}, fmt.Errorf("unknown event %q (want %q or %q)", eventStr, HookEventStatusChange, HookEventLabelChange)
	}

	if h.Event == HookEventLabelChange {
		if fromStatusNode != nil {
			return Hook{}, fmt.Errorf("from_status is not valid for %s hooks", HookEventLabelChange)
		}
		if toStatusNode != nil {
			return Hook{}, fmt.Errorf("to_status is not valid for %s hooks", HookEventLabelChange)
		}
	}
	if h.Event == HookEventStatusChange {
		if addedNode != nil {
			return Hook{}, fmt.Errorf("added is not valid for %s hooks", HookEventStatusChange)
		}
		if removedNode != nil {
			return Hook{}, fmt.Errorf("removed is not valid for %s hooks", HookEventStatusChange)
		}
	}

	var err error
	if h.FromStatus, err = decodeStringList(fromStatusNode, "from_status"); err != nil {
		return Hook{}, err
	}
	if h.ToStatus, err = decodeStringList(toStatusNode, "to_status"); err != nil {
		return Hook{}, err
	}
	if h.IssueType, err = decodeStringList(issueTypeNode, "issue_type"); err != nil {
		return Hook{}, err
	}
	if h.Added, err = decodeStringList(addedNode, "added"); err != nil {
		return Hook{}, err
	}
	if h.Removed, err = decodeStringList(removedNode, "removed"); err != nil {
		return Hook{}, err
	}

	if commandNode == nil {
		return Hook{}, fmt.Errorf("missing required key \"command\"")
	}
	if err := commandNode.Decode(&h.Command); err != nil {
		return Hook{}, fmt.Errorf("command: %w", err)
	}
	if len(h.Command) == 0 {
		return Hook{}, fmt.Errorf("command must be a non-empty list")
	}

	h.Timeout = 10 * time.Second
	if timeoutNode != nil {
		var s string
		if err := timeoutNode.Decode(&s); err != nil {
			return Hook{}, fmt.Errorf("timeout: %w", err)
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return Hook{}, fmt.Errorf("timeout %q: %w", s, err)
		}
		h.Timeout = d
	}

	return h, nil
}

// decodeStringList decodes n (nil when the key was omitted) into a string
// slice, wrapping any decode error with field for a clearer message.
func decodeStringList(n *yaml.Node, field string) ([]string, error) {
	if n == nil {
		return nil, nil
	}
	var out []string
	if err := n.Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: %w", field, err)
	}
	return out, nil
}

// MatchEvent describes one derived card event to test against a set of
// hooks: which filters apply depends on Event.
type MatchEvent struct {
	// Event is the kind of change that occurred.
	Event HookEvent

	// FromStatus and ToStatus are the card's status before and after a
	// status-change event.
	FromStatus string
	ToStatus   string

	// IssueType is the card's type.
	IssueType string

	// Added and Removed are the labels a label-change event added or
	// removed.
	Added   []string
	Removed []string
}

// MatchHooks returns, in file order, every hook in hooks whose event and
// filters match ev. Filters within a single hook are OR'ed on their own
// values and AND'ed against each other; an omitted filter matches
// everything.
func MatchHooks(hooks []Hook, ev MatchEvent) []Hook {
	var out []Hook
	for _, h := range hooks {
		if hookMatches(h, ev) {
			out = append(out, h)
		}
	}
	return out
}

func hookMatches(h Hook, ev MatchEvent) bool {
	if h.Event != ev.Event {
		return false
	}
	if !matchesOne(h.IssueType, ev.IssueType) {
		return false
	}

	switch ev.Event {
	case HookEventStatusChange:
		return matchesOne(h.FromStatus, ev.FromStatus) && matchesOne(h.ToStatus, ev.ToStatus)
	case HookEventLabelChange:
		return matchesAny(h.Added, ev.Added) && matchesAny(h.Removed, ev.Removed)
	default:
		return false
	}
}

// matchesOne reports whether value satisfies filter: an empty filter
// matches everything, otherwise value must equal one of filter's entries.
func matchesOne(filter []string, value string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, v := range filter {
		if v == value {
			return true
		}
	}
	return false
}

// matchesAny reports whether values satisfies filter: an empty filter
// matches everything, otherwise at least one entry of values must appear in
// filter.
func matchesAny(filter, values []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, v := range values {
		for _, f := range filter {
			if v == f {
				return true
			}
		}
	}
	return false
}
