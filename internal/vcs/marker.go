package vcs

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
)

// SummaryWrapperBegin is the HTML-comment sentinel that opens the
// consolidated overview comment. It is the single source of truth for the
// marker shared between the orchestrator (which writes it) and the VCS
// adapters (which grep for it during stateless read-back).
const SummaryWrapperBegin = "<!-- cadoo:wrapper:begin -->"

// reviewedSHAPrefix and reviewedSHASuffix delimit the last-reviewed-SHA
// marker embedded inside the consolidated summary wrapper. The SHA is
// validated as exactly 40 lowercase hex chars before use (ASVS V5).
const (
	reviewedSHAPrefix = "<!-- cadoo:reviewed-sha:"
	reviewedSHASuffix = " -->"
)

// reviewedSHARe validates that a candidate SHA is exactly 40 lowercase hex chars.
var reviewedSHARe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// RenderReviewedSHA returns the HTML-comment marker that records which head
// SHA was most recently reviewed. The marker is embedded inside the
// consolidated summary wrapper, immediately after SummaryWrapperBegin, so
// subsequent stateless CI runs can retrieve it via ParseReviewedSHA.
func RenderReviewedSHA(sha string) string {
	return reviewedSHAPrefix + sha + reviewedSHASuffix
}

// ParseReviewedSHA scans body for the reviewed-sha marker and returns the
// embedded SHA only when it is exactly 40 lowercase hex characters. Any
// other value (wrong length, uppercase, non-hex chars) returns "". This
// validation prevents a forged PR comment from directing DiffBetween at an
// arbitrary object (ASVS V5 / T-08-C1).
func ParseReviewedSHA(body string) string {
	idx := strings.Index(body, reviewedSHAPrefix)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(reviewedSHAPrefix):]
	end := strings.Index(rest, reviewedSHASuffix)
	if end < 0 {
		return ""
	}
	candidate := rest[:end]
	if !reviewedSHARe.MatchString(candidate) {
		return ""
	}
	return candidate
}

// MarkerData is the machine payload embedded in every Cadoo inline comment
// so a stateless CI run can recognise its own prior findings.
type MarkerData struct {
	Tool string
	SK   string // findings.StructuralKey of the original (pristine) comment
	Sev  string // vcs.Severity string
	NT   string // normalizeTitle(full body) — for cross-run Jaccard dedup; empty on legacy markers
}

var inlineMarkerRe = regexp.MustCompile(
	`\n*<!-- cadoo:fp v=1 tool=(\S+) sk=(\S+) sev=(\S*)(?:\s+nt=(\S+))? -->`)

// InlineMarker renders the hidden marker line. It is appended only to the
// wire copy of a comment body — never to the body used for key computation.
// The optional NT field encodes the full-body normalized title as base64url
// so a subsequent stateless run can seed the Jaccard dedup store correctly.
func InlineMarker(d MarkerData) string {
	if d.NT == "" {
		return fmt.Sprintf("<!-- cadoo:fp v=1 tool=%s sk=%s sev=%s -->", d.Tool, d.SK, d.Sev)
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(d.NT))
	return fmt.Sprintf("<!-- cadoo:fp v=1 tool=%s sk=%s sev=%s nt=%s -->", d.Tool, d.SK, d.Sev, encoded)
}

// ParseInlineMarker extracts the marker from a comment body. It returns the
// parsed payload, the body with the marker (and its leading blank line)
// removed, and whether a marker was present. The NT field is decoded from
// base64url if present; legacy markers (without nt=) leave NT empty.
func ParseInlineMarker(body string) (MarkerData, string, bool) {
	loc := inlineMarkerRe.FindStringSubmatchIndex(body)
	if loc == nil {
		return MarkerData{}, body, false
	}
	stripped := strings.TrimRight(body[:loc[0]], "\n")
	md := MarkerData{
		Tool: body[loc[2]:loc[3]],
		SK:   body[loc[4]:loc[5]],
		Sev:  body[loc[6]:loc[7]],
	}
	// nt= group (index 8/9): present only in v=1 markers that include the
	// normalized-title field.
	if loc[8] >= 0 {
		encoded := body[loc[8]:loc[9]]
		if decoded, err := base64.RawURLEncoding.DecodeString(encoded); err == nil {
			md.NT = string(decoded)
		}
	}
	return md, stripped, true
}

// FirstLine returns the first line of s (no trailing newline).
func FirstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
