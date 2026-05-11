// Package slop scores a PR for "slop" — low-quality, low-context AI-generated
// PRs. Cadoo uses the score to decide whether to short-circuit a review with
// a request-for-context comment instead of running the full tool pipeline.
package slop

import (
	"fmt"
	"strings"
)

// Report is the slop classification for one PR.
type Report struct {
	IsSlop  bool
	Score   float64 // 0..1; >= 0.5 == IsSlop
	Reasons []string
}

// Threshold above which a PR is flagged as slop.
const Threshold = 0.5

// Detect scores the PR. The signals are intentionally cheap (no LLM calls)
// so this can run on every webhook before paying for a real review.
func Detect(prTitle, prBody string, totalAdditions, totalDeletions, fileCount int) Report {
	var reasons []string
	score := 0.0

	switch {
	case strings.TrimSpace(prBody) == "":
		score += 0.30
		reasons = append(reasons, "empty PR body")
	case len(prBody) < 50:
		score += 0.15
		reasons = append(reasons, "very short PR body (<50 chars)")
	}

	if totalAdditions > 1000 && fileCount > 20 {
		score += 0.25
		reasons = append(reasons,
			fmt.Sprintf("large diff (%d additions across %d files) without proportionate description", totalAdditions, fileCount))
	}

	if titleLooksGeneric(prTitle) {
		score += 0.20
		reasons = append(reasons, fmt.Sprintf("generic-looking PR title %q", prTitle))
	}

	if totalAdditions+totalDeletions == 0 && fileCount == 0 {
		score += 0.20
		reasons = append(reasons, "no source changes detected")
	}

	if score > 1 {
		score = 1
	}
	return Report{
		IsSlop:  score >= Threshold,
		Score:   score,
		Reasons: reasons,
	}
}

func titleLooksGeneric(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return true
	}
	switch s {
	case "update", "wip", "fix", "fixes", "various changes", "improvements", "changes", "patch":
		return true
	}
	return false
}
