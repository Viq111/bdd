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
	copy := append([]model.Warning(nil), values...)
	sort.Slice(copy, func(i, j int) bool { return copy[i].SourceID < copy[j].SourceID })
	lines := make([]string, 0, len(copy))
	for _, w := range copy {
		reasons := append([]string(nil), w.Reasons...)
		sort.Strings(reasons)
		lines = append(lines, fmt.Sprintf("warning: %s: %s", renderText(w.SourceID), renderText(strings.Join(reasons, "; "))))
	}
	return strings.Join(lines, "\n")
}

// renderText keeps untrusted source text from creating additional physical
// stderr lines. Escaping also makes the original offending bytes visible to
// the operator instead of silently discarding them.
func renderText(s string) string {
	return strings.NewReplacer("\r", "\\r", "\n", "\\n").Replace(s)
}
