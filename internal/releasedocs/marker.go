package releasedocs

import "strings"

// Locked release-notes marker constants. These strings are written into VCS
// release bodies and must never change once shipped — altering them would
// break idempotent splice on existing releases (Runtime State Inventory).
// Any downstream code that constructs these strings must import this constant
// rather than duplicating the literal.
const (
	// ReleaseNotesBegin opens the Cadoo-managed release-notes section inside
	// a VCS release body.
	ReleaseNotesBegin = "<!-- cadoo:release-notes:begin -->"
	// ReleaseNotesEnd closes the Cadoo-managed release-notes section.
	ReleaseNotesEnd = "<!-- cadoo:release-notes:end -->"
)

// ChangelogMarker returns the per-release idempotency marker embedded in a
// CHANGELOG pull-request body. The marker is keyed on toRef (the release tag,
// e.g. "v1.2.3") so the dispatcher can grep an existing PR body to decide
// whether to update or create (D-13).
func ChangelogMarker(toRef string) string {
	return "<!-- cadoo:changelog:" + toRef + " -->"
}

// ChangelogBranch returns the deterministic branch name the changelog
// publisher creates or updates when opening a CHANGELOG.md PR. Keyed on
// toRef so each release has exactly one associated branch.
func ChangelogBranch(toRef string) string {
	return "cadoo/changelog/" + toRef
}

// SpliceReleaseBody returns the release body to send when the release-notes
// publisher wants to inject section while preserving whatever the operator
// originally wrote. User text outside the managed markers is never touched;
// Cadoo's section is wrapped between ReleaseNotesBegin / ReleaseNotesEnd so
// subsequent runs replace it cleanly (idempotent splice, mirrors
// orchestrator.spliceCadooBody).
func SpliceReleaseBody(original, section string) string {
	section = strings.TrimSpace(section)
	startIdx := strings.Index(original, ReleaseNotesBegin)
	endIdx := strings.Index(original, ReleaseNotesEnd)

	// Already a managed body: replace just the inner section.
	if startIdx >= 0 && endIdx > startIdx {
		head := strings.TrimRight(original[:startIdx], " \n\t")
		tail := original[endIdx+len(ReleaseNotesEnd):]
		return joinReleaseBody(head, section, tail)
	}

	// First-time write: append the section after the operator's existing text.
	return joinReleaseBody(strings.TrimRight(original, " \n\t"), section, "")
}

// joinReleaseBody assembles userText + the managed section + optional tail
// into the canonical release body format. User content outside the markers is
// preserved exactly (mirrors orchestrator.joinBody).
func joinReleaseBody(userText, section, tail string) string {
	var b strings.Builder
	if userText != "" {
		b.WriteString(userText)
		b.WriteString("\n\n")
	}
	b.WriteString(ReleaseNotesBegin)
	b.WriteString("\n")
	b.WriteString(section)
	b.WriteString("\n")
	b.WriteString(ReleaseNotesEnd)
	if tail = strings.TrimLeft(tail, " \n\t"); tail != "" {
		b.WriteString("\n\n")
		b.WriteString(tail)
	}
	return b.String()
}

// HasChangelogMarker reports whether the pull-request body already contains
// the idempotency marker for toRef. The publisher uses this to decide whether
// to update the existing PR body or create a new PR (mirrors the
// strings.Contains check in internal/vcs/github/github.go for SummaryWrapperBegin).
func HasChangelogMarker(body, toRef string) bool {
	return strings.Contains(body, ChangelogMarker(toRef))
}
