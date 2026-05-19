package vcs

import (
	"fmt"
	"regexp"
	"strings"
)

// SummaryWrapperBegin is the HTML-comment sentinel that opens the
// consolidated overview comment. It is the single source of truth for the
// marker shared between the orchestrator (which writes it) and the VCS
// adapters (which grep for it during stateless read-back).
const SummaryWrapperBegin = "<!-- cadoo:wrapper:begin -->"

// MarkerData is the machine payload embedded in every Cadoo inline comment
// so a stateless CI run can recognise its own prior findings.
type MarkerData struct {
	Tool string
	SK   string // findings.StructuralKey of the original (pristine) comment
	Sev  string // vcs.Severity string
}

var inlineMarkerRe = regexp.MustCompile(
	`\n*<!-- cadoo:fp v=1 tool=(\S+) sk=(\S+) sev=(\S*) -->\s*$`)

// InlineMarker renders the hidden marker line. It is appended only to the
// wire copy of a comment body — never to the body used for key computation.
func InlineMarker(d MarkerData) string {
	return fmt.Sprintf("<!-- cadoo:fp v=1 tool=%s sk=%s sev=%s -->", d.Tool, d.SK, d.Sev)
}

// ParseInlineMarker extracts the marker from a comment body. It returns the
// parsed payload, the body with the marker (and its leading blank line)
// removed, and whether a marker was present.
func ParseInlineMarker(body string) (MarkerData, string, bool) {
	loc := inlineMarkerRe.FindStringSubmatchIndex(body)
	if loc == nil {
		return MarkerData{}, body, false
	}
	m := inlineMarkerRe.FindStringSubmatch(body)
	stripped := strings.TrimRight(body[:loc[0]], "\n")
	return MarkerData{Tool: m[1], SK: m[2], Sev: m[3]}, stripped, true
}

// FirstLine returns the first line of s (no trailing newline).
func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
