// Package issuetrackers integrates Cadoo with external issue trackers
// (Jira, Linear, etc.) so the LLM can validate that a PR actually addresses
// what its linked tickets ask for.
package issuetrackers

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// Issue is the normalized model. Tracker-specific fields go in Metadata.
type Issue struct {
	Tracker   string // "jira" | "linear" | ...
	Key       string // e.g. "ENG-123"
	Title     string
	Body      string
	Status    string
	URL       string
	Assignee  string
	Labels    []string
	UpdatedAt time.Time
}

// Tracker is the surface every issue-tracker adapter implements.
type Tracker interface {
	Name() string
	// FindLinked extracts ticket references from PR text and fetches them.
	// Returning (nil, nil) on no matches is fine; transient errors should
	// be returned so the caller can log/retry.
	FindLinked(ctx context.Context, prTitle, prBody string) ([]Issue, error)
}

// CommonKeyRe matches Jira/Linear-style keys: TEAM-123. The TEAM portion is
// at least 2 uppercase letters (Linear uses 3+ chars, Jira allows 2+).
var CommonKeyRe = regexp.MustCompile(`\b([A-Z][A-Z0-9]+-\d+)\b`)

// ExtractKeys returns all unique uppercase TEAM-NNN keys mentioned in text.
func ExtractKeys(text string) []string {
	matches := CommonKeyRe.FindAllStringSubmatch(text, -1)
	seen := map[string]bool{}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		k := strings.ToUpper(m[1])
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}
