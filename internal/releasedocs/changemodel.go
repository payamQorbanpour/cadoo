package releasedocs

import (
	"log/slog"
	"sort"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// GroupedModel is the deterministic, ordered grouping of commits and merged
// pull-requests produced by BuildGroupedModel. It is built once from a
// ReleaseContext and passed to every Generator. The section ordering is
// canonical (matching the grouping.sections config) so that golden-file tests
// produce byte-identical output on repeated runs (Pitfall 3 — never map order).
type GroupedModel struct {
	// Sections is the ordered list of changelog sections. The order matches the
	// canonical grouping.sections config; sections with no entries are omitted.
	Sections []ChangeSection
}

// ChangeSection is one titled group of change entries in the grouped model.
type ChangeSection struct {
	// Title is the display title for this section (e.g. "Features", "Bug Fixes").
	Title string
	// Entries is the ordered list of change entries in this section.
	Entries []ChangeEntry
}

// ChangeEntry is a single item in a changelog section — either a commit or a
// merged pull-request, depending on the grouping source.
type ChangeEntry struct {
	// Title is the display summary for this entry (commit subject or PR title).
	Title string
	// Author is the commit author or PR author login.
	Author string
	// SHA is the commit SHA or merge SHA (empty for label-grouped entries that
	// only have a PR number).
	SHA string
	// PRNumber is the associated pull-request or merge-request number, or 0 if
	// this entry was derived from a commit that is not associated with a PR.
	PRNumber int64
}

// defaultSections is the built-in canonical section order used when
// grouping.sections is empty.
var defaultSections = []string{
	"Breaking Changes",
	"Features",
	"Bug Fixes",
	"Performance",
	"Other",
}

// conventionalSectionMap maps Conventional Commit type prefixes to their
// canonical display section title. This map controls how commit messages are
// classified when grouping.source = "conventional" (or the llm fallback path).
// The "Other" fallback is applied by BuildGroupedModel when no prefix matches.
var conventionalSectionMap = map[string]string{
	"feat!": "Breaking Changes",
	"fix!":  "Breaking Changes",
	"feat":  "Features",
	"fix":   "Bug Fixes",
	"perf":  "Performance",
}

// ComputeBump returns the SemverBump that describes the change from fromRef to
// toRef. Both tags are normalized to carry a leading "v" before comparison
// (semver.Compare requires it). Malformed tags yield BumpNone or BumpMajor
// (first-release) rather than a panic (T-02-03 mitigation).
//
// Rules:
//   - Empty fromRef (no prior tag) ⇒ BumpMajor (first release).
//   - Malformed fromRef with valid toRef ⇒ BumpMajor (treat as first release).
//   - Valid fromRef with malformed toRef ⇒ BumpNone (cannot determine bump).
//   - Same canonical version ⇒ BumpNone.
//   - Different major component ⇒ BumpMajor.
//   - Same major, different minor ⇒ BumpMinor.
//   - Same major+minor ⇒ BumpPatch.
func ComputeBump(fromRef, toRef string) SemverBump {
	// Normalize: semver package requires a leading "v".
	from := normalizeSemver(fromRef)
	to := normalizeSemver(toRef)

	// Empty fromRef is the first-release case.
	if from == "" || from == "v" {
		if to != "" && semver.IsValid(to) {
			return BumpMajor
		}
		return BumpNone
	}

	// Malformed toRef: cannot compute bump.
	if !semver.IsValid(to) {
		return BumpNone
	}

	// Malformed fromRef: treat as first release.
	if !semver.IsValid(from) {
		return BumpMajor
	}

	// Same canonical version.
	if semver.Canonical(from) == semver.Canonical(to) {
		return BumpNone
	}

	// Compare major components.
	if semver.Major(from) != semver.Major(to) {
		return BumpMajor
	}

	// Compare major+minor.
	if semver.MajorMinor(from) != semver.MajorMinor(to) {
		return BumpMinor
	}

	return BumpPatch
}

// normalizeSemver ensures the version string has a leading "v" as required by
// golang.org/x/mod/semver. An already-prefixed string is returned unchanged.
// An empty string returns "".
func normalizeSemver(v string) string {
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// BuildGroupedModel constructs a deterministic GroupedModel from commits and
// merged pull-requests using the grouping strategy defined in cfg.
//
// Grouping sources:
//   - "conventional" (default): parse Conventional Commit prefixes from commit
//     messages (feat/fix/perf/feat!/BREAKING CHANGE body).
//   - "labels": group merged pull-requests by their VCS labels using the
//     configured label→section map; unmapped labels fall to "Other".
//   - "llm": not implemented this phase; logs a warning and falls back to
//     conventional (Open Question 1 resolution).
//
// Section ordering is canonical: sections are sorted by their position in
// cfg.Grouping.Sections (or defaultSections when that slice is empty).
// Sections with no entries are omitted. Same input twice ⇒ byte-identical
// section ordering (Pitfall 3).
func BuildGroupedModel(commits []vcs.Commit, prs []vcs.MergedPR, cfg config.ReleaseDocs) GroupedModel {
	canonical := cfg.Grouping.Sections
	if len(canonical) == 0 {
		canonical = defaultSections
	}

	// Build a position index for O(1) lookup.
	posOf := make(map[string]int, len(canonical))
	for i, s := range canonical {
		posOf[s] = i
	}

	// sectionEntries accumulates entries per section title.
	sectionEntries := make(map[string][]ChangeEntry)

	source := cfg.Grouping.Source
	if source == "" {
		source = "conventional"
	}
	if source == "llm" {
		slog.Warn("releasedocs: grouping.source=llm is not implemented this phase; falling back to conventional")
		source = "conventional"
	}

	switch source {
	case "labels":
		buildLabelGroupedEntries(prs, cfg.Grouping.Labels, canonical, posOf, sectionEntries)
	default:
		// conventional (and any unknown source falls through to conventional)
		buildConventionalGroupedEntries(commits, canonical, posOf, sectionEntries)
	}

	// Collect non-empty sections and sort by canonical position.
	sections := make([]ChangeSection, 0, len(sectionEntries))
	for _, title := range canonical {
		entries, ok := sectionEntries[title]
		if !ok || len(entries) == 0 {
			continue
		}
		sections = append(sections, ChangeSection{Title: title, Entries: entries})
	}

	// sort.SliceStable mirrors the deterministic ordering discipline from
	// internal/orchestrator/consolidate.go:70. We sort by canonical position;
	// sections already deduplicated above so stable sort is a no-op here,
	// but retained for correctness if the collection strategy ever changes.
	sort.SliceStable(sections, func(i, j int) bool {
		pi, pj := canonicalPos(sections[i].Title, posOf), canonicalPos(sections[j].Title, posOf)
		return pi < pj
	})

	return GroupedModel{Sections: sections}
}

// buildConventionalGroupedEntries parses Conventional Commit prefixes from
// commits and adds entries to sectionEntries.
func buildConventionalGroupedEntries(
	commits []vcs.Commit,
	canonical []string,
	posOf map[string]int,
	sectionEntries map[string][]ChangeEntry,
) {
	for _, c := range commits {
		section := classifyCommit(c.Message)
		if !isKnownSection(section, posOf) {
			section = fallbackSection(canonical)
		}
		if section == "" {
			continue
		}
		subject := commitSubject(c.Message)
		sectionEntries[section] = append(sectionEntries[section], ChangeEntry{
			Title:  subject,
			Author: c.Author,
			SHA:    c.SHA,
		})
	}
}

// buildLabelGroupedEntries groups merged PRs by label using the configured
// label→section map. PRs with no matching label fall to the "Other" section
// (or the last section in canonical if "Other" is not present).
func buildLabelGroupedEntries(
	prs []vcs.MergedPR,
	labelMap map[string]string,
	canonical []string,
	posOf map[string]int,
	sectionEntries map[string][]ChangeEntry,
) {
	for _, pr := range prs {
		section := classifyByLabels(pr.Labels, labelMap)
		if !isKnownSection(section, posOf) {
			section = fallbackSection(canonical)
		}
		if section == "" {
			continue
		}
		sectionEntries[section] = append(sectionEntries[section], ChangeEntry{
			Title:    pr.Title,
			Author:   pr.Author,
			SHA:      pr.MergeSHA,
			PRNumber: pr.Number,
		})
	}
}

// classifyCommit returns the canonical section title for a commit message using
// Conventional Commit prefix detection (~30 lines, A5). Detects:
//   - Breaking: "feat!:" / "fix!:" prefix, or "BREAKING CHANGE:" in the body.
//   - "feat:" prefix → "Features".
//   - "fix:" prefix → "Bug Fixes".
//   - "perf:" prefix → "Performance".
//   - Anything else → "" (caller applies fallback).
func classifyCommit(msg string) string {
	// Check body for BREAKING CHANGE marker first — takes priority.
	if _, body, ok := strings.Cut(msg, "\n"); ok {
		if strings.Contains(body, "BREAKING CHANGE:") {
			return "Breaking Changes"
		}
	}

	// Extract the subject line.
	subject, _, _ := strings.Cut(msg, "\n")
	subject = strings.TrimSpace(subject)

	// Check for feat! or fix! (breaking-change shorthand).
	for _, breaking := range []string{"feat!:", "fix!:"} {
		if strings.HasPrefix(subject, breaking) {
			return "Breaking Changes"
		}
	}

	// Check conventional prefixes in priority order.
	// Note: check "feat!" before "feat" since HasPrefix("feat!:", "feat:") is false,
	// but we still want to avoid matching "feat!" here.
	for _, prefix := range []string{"feat:", "fix:", "perf:"} {
		if !strings.HasPrefix(subject, prefix) {
			continue
		}
		// Ensure it's exactly this prefix (not feat! which we handled above).
		if section, ok := conventionalSectionMap[prefix[:len(prefix)-1]]; ok {
			return section
		}
	}

	return ""
}

// classifyByLabels returns the first configured section for the PR's labels,
// or "" if no label is mapped.
func classifyByLabels(labels []string, labelMap map[string]string) string {
	for _, label := range labels {
		if section, ok := labelMap[label]; ok {
			return section
		}
	}
	return ""
}

// fallbackSection returns the "Other" section if present in canonical, or the
// last section in canonical, or "" if canonical is empty.
func fallbackSection(canonical []string) string {
	for _, s := range canonical {
		if s == "Other" {
			return s
		}
	}
	if len(canonical) > 0 {
		return canonical[len(canonical)-1]
	}
	return ""
}

// isKnownSection reports whether the given section title exists in posOf.
func isKnownSection(section string, posOf map[string]int) bool {
	if section == "" {
		return false
	}
	_, ok := posOf[section]
	return ok
}

// canonicalPos returns the position of title in posOf, or a large value if
// not found (should not happen after filtering).
func canonicalPos(title string, posOf map[string]int) int {
	if pos, ok := posOf[title]; ok {
		return pos
	}
	return len(posOf) + 1
}

// commitSubject extracts the first line (subject) of a commit message.
func commitSubject(msg string) string {
	subject, _, _ := strings.Cut(msg, "\n")
	return strings.TrimSpace(subject)
}
