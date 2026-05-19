package findings

import "github.com/payamqorbanpour/cadoo/internal/vcs"

// StampInline returns the comment body with the hidden dedup marker
// appended. The marker encodes the StructuralKey of the PRISTINE comment
// so a later stateless run recovers the exact same key. Callers must pass
// the original comment for key computation and recording; only the value
// returned here goes over the wire.
func StampInline(tool string, c vcs.InlineComment) string {
	return c.Body + "\n\n" + vcs.InlineMarker(vcs.MarkerData{
		Tool: tool,
		SK:   StructuralKey(tool, c),
		Sev:  string(c.Severity),
	})
}

// NewFromPrior builds an in-memory Store (no disk path) pre-populated from a
// PR's own prior Cadoo artifacts, so stateless CI-mode reuses the exact
// HasFinding / ListPostedFindings / SummaryID dedup logic with no changes
// to postSummary / postInline / resolveStalePriors.
func NewFromPrior(key PRKey, pr vcs.PriorReview) *Store {
	m := newMemoryStore("") // empty path => load()/persist() are no-ops
	recs := make([]findingRec, 0, len(pr.Inline))
	for _, pi := range pr.Inline {
		recs = append(recs, findingRec{
			Tool:            pi.Tool,
			File:            pi.File,
			Severity:        pi.Severity,
			StructuralKey:   pi.StructuralKey,
			NormalizedTitle: normalizeTitle(pi.Title),
			Title:           pi.Title,
			ExternalID:      pi.ExternalID,
		})
	}
	if len(recs) > 0 {
		m.findings[key] = recs
	}
	if pr.SummaryCommentID != "" {
		m.summaries[summaryRefKey{PR: key, Tool: WrapperToolKey}] = pr.SummaryCommentID
	}
	return &Store{mem: m}
}
