// Package diagrams implements the engineering-diagrams Generator for the
// release-docs subsystem. The generator fetches user-selected committed Mermaid
// source files (sequence, dependency, state, flowchart, class) at the release
// tag via FileFetcher, sniffs each for a type-appropriate Mermaid keyword, wraps
// valid sources in a fixed ```mermaid markdown fence, and emits one Artifact per
// resolved source. The generator is fully deterministic (no LLM). A missing or
// non-Mermaid source is skipped with a logged reason, never failing siblings
// (D-08, DIAG-04).
package diagrams

import (
	"strings"
)

// mermaidKeywords maps each fixed diagram type to the set of Mermaid header
// keywords that are valid for that type. Matching is prefix-based, so the
// "state" keyword "stateDiagram" also accepts "stateDiagram-v2" (Pitfall 5).
// The "dependency" set {flowchart, graph, erDiagram} is the RESEARCH Q3
// confirmed mapping (a dependency diagram is rendered as a graph/flowchart, and
// an entity-relationship diagram is an accepted dependency representation).
var mermaidKeywords = map[string][]string{
	"sequence":   {"sequenceDiagram"},
	"class":      {"classDiagram"},
	"state":      {"stateDiagram"}, // prefix-match also accepts stateDiagram-v2
	"flowchart":  {"flowchart", "graph"},
	"dependency": {"flowchart", "graph", "erDiagram"}, // RESEARCH Q3 confirmed set
}

// firstSignificantToken returns the first whitespace-delimited token of the
// first significant line of src. It skips leading blank/whitespace-only lines,
// %% comment lines, and a leading "---"…"---" YAML frontmatter block in its
// entirety (Pitfall 1), so a frontmatter'd or comment-prefixed Mermaid source
// still sniffs correctly. Returns "" when no significant line exists.
func firstSignificantToken(src []byte) string {
	lines := strings.Split(string(src), "\n")
	i := 0

	// Skip a leading frontmatter block: a "---" line followed by content and a
	// closing "---" line. Only treat the first non-blank line as a frontmatter
	// fence when it is exactly "---".
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i < len(lines) && strings.TrimSpace(lines[i]) == "---" {
		i++ // consume opening fence
		for i < len(lines) && strings.TrimSpace(lines[i]) != "---" {
			i++
		}
		if i < len(lines) {
			i++ // consume closing fence
		}
	}

	// Skip blank lines and %% comment lines, then return the first token.
	for ; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "%%") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		return fields[0]
	}
	return ""
}

// sniffMermaid reports whether src is a valid Mermaid source for the given
// diagram type. It checks the first significant token (after skipping blanks,
// %% comments, and frontmatter) against the type's keyword set using a prefix
// match. A source whose first significant line is not a recognized keyword for
// the type (e.g. a .puml/.dot/prose first line) returns false → the generator
// skips it (D-02, DIAG-04). This answers only "valid for THIS type?"; the
// warn-but-publish cross-type decision is the generator's concern.
func sniffMermaid(src []byte, diagramType string) bool {
	first := firstSignificantToken(src)
	if first == "" {
		return false
	}
	for _, kw := range mermaidKeywords[diagramType] {
		if strings.HasPrefix(first, kw) {
			return true
		}
	}
	return false
}
