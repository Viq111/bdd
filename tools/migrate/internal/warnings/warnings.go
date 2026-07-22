// Package warnings renders migration warnings as the command-line contract.
package warnings

import (
	"fmt"
	"github.com/viq111/bdd/tools/migrate/internal/model"
	"sort"
	"strings"
)

// Render emits exactly one sorted line per source record.
func Render(values []model.Warning) string {
	bySource := make(map[string]map[string]struct{}, len(values))
	for _, warning := range values {
		if bySource[warning.SourceID] == nil {
			bySource[warning.SourceID] = make(map[string]struct{})
		}
		for _, reason := range warning.Reasons {
			bySource[warning.SourceID][reason] = struct{}{}
		}
	}
	sources := make([]string, 0, len(bySource))
	for source := range bySource {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	lines := make([]string, 0, len(sources))
	for _, source := range sources {
		reasons := make([]string, 0, len(bySource[source]))
		for reason := range bySource[source] {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		lines = append(lines, fmt.Sprintf("warning: %s: %s", renderText(source), renderText(strings.Join(reasons, "; "))))
	}
	return strings.Join(lines, "\n")
}

// renderText keeps untrusted source text from creating additional physical
// stderr lines. Escaping also makes the original offending bytes visible to
// the operator instead of silently discarding them.
func renderText(s string) string {
	return strings.NewReplacer("\r", "\\r", "\n", "\\n").Replace(s)
}
