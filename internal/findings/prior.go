package findings

import "github.com/payamqorbanpour/cadoo/internal/vcs"

// StampInline returns the comment body with the hidden dedup marker
// appended. The marker encodes the StructuralKey of the PRISTINE comment
// and the full-body NormalizedTitle so a later stateless run recovers both
// the exact same key and enough text for a reliable Jaccard dedup match.
// Callers must pass the original comment for key computation and recording;
// only the value returned here goes over the wire.
func StampInline(tool string, c vcs.InlineComment) string {
	return c.Body + "\n\n" + vcs.InlineMarker(vcs.MarkerData{
		Tool: tool,
		SK:   StructuralKey(tool, c),
		Sev:  string(c.Severity),
		NT:   normalizeTitle(c.Body),
	})
}

// NewFromPrior builds an in-memory Store (no disk path) pre-populated from a
// PR's own prior Cadoo artifacts, so stateless CI-mode reuses the exact
// HasFinding / ListPostedFindings / SummaryID dedup logic with no changes
// to postSummary / postInline / resolveStalePriors.
func NewFromPrior(key PRKey, pr vcs.PriorReview) *Store {
	m := newMemoryStore("") // empty path => load()/persist() are no-ops
	// Seeded records intentionally carry no Fingerprint: it can't be
	// reconstructed from read-back (the marker only encodes tool/sk/sev/nt,
	// not line numbers or the full body). Stateless dedup keys on
	// StructuralKey via Store.has(), and postInline filters a persisting
	// finding out of the delta before posting, so RecordFinding (whose
	// memoryStore.record idempotency guard compares Fingerprint) is never
	// invoked for an already-seeded finding. The empty Fingerprint is
	// therefore moot for seeded records by design.
	recs := make([]findingRec, 0, len(pr.Inline))
	for _, pi := range pr.Inline {
		nt := pi.NormalizedTitle
		if nt == "" {
			// Legacy marker without nt= field: fall back to first-line
			// normalization. Jaccard matching will be less reliable for
			// multi-line bodies, but StructuralKey still provides exact
			// match when the LLM reproduces the same output.
			nt = normalizeTitle(pi.Title)
		}
		recs = append(recs, findingRec{
			Tool:            pi.Tool,
			File:            pi.File,
			Severity:        pi.Severity,
			StructuralKey:   pi.StructuralKey,
			NormalizedTitle: nt,
			Title:           pi.Title,
			ExternalID:      pi.ExternalID,
			Resolved:        pi.Resolved,
			Line:            pi.Line,
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
