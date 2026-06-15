package findings

import (
	"context"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestStampInlineDoesNotMutateOriginal(t *testing.T) {
	c := vcs.InlineComment{File: "a.go", Body: "Avoid the N+1 query.", Severity: vcs.SeverityWarn}
	wantSK := StructuralKey("review", c)

	stamped := StampInline("review", c)

	if c.Body != "Avoid the N+1 query." {
		t.Fatalf("original body mutated: %q", c.Body)
	}
	md, stripped, ok := vcs.ParseInlineMarker(stamped)
	if !ok || stripped != "Avoid the N+1 query." {
		t.Fatalf("stamped body not parseable: ok=%v stripped=%q", ok, stripped)
	}
	if md.SK != wantSK || md.Tool != "review" || md.Sev != "warn" {
		t.Errorf("marker = %+v; want sk=%s tool=review sev=warn", md, wantSK)
	}
	// NT must be populated so cross-run Jaccard dedup can work.
	wantNT := normalizeTitle(c.Body)
	if md.NT != wantNT {
		t.Errorf("marker NT = %q; want %q", md.NT, wantNT)
	}
}

func TestNewFromPriorSeedsDedupAndSummary(t *testing.T) {
	ctx := context.Background()
	key := PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 7}
	c := vcs.InlineComment{File: "a.go", Body: "Avoid the N+1 query.", Severity: vcs.SeverityWarn}
	sk := StructuralKey("review", c)

	s := NewFromPrior(key, vcs.PriorReview{
		SummaryCommentID: "99",
		Inline: []vcs.PriorInline{{
			Tool: "review", File: "a.go", Severity: "warn",
			StructuralKey: sk, Title: "Avoid the N+1 query.",
			ExternalID: "disc-1", Resolved: false,
		}},
	})

	if !s.Enabled() {
		t.Fatal("store should be Enabled()")
	}
	has, err := s.HasFinding(ctx, key, "review", c)
	if err != nil || !has {
		t.Fatalf("HasFinding = %v, %v; want true, nil", has, err)
	}
	id, _ := s.SummaryID(ctx, key, WrapperToolKey)
	if id != "99" {
		t.Errorf("SummaryID = %q; want %q", id, "99")
	}
	priors, _ := s.ListPostedFindings(ctx, key)
	if len(priors) != 1 || priors[0].ExternalCommentID != "disc-1" {
		t.Errorf("ListPostedFindings = %+v; want one with ExternalCommentID=disc-1", priors)
	}
}

// TestNewFromPriorJaccardWithNT verifies that when a prior inline comment is
// seeded with a NormalizedTitle (from the nt= marker field), a rephrased
// version of the same finding is correctly recognised by HasFinding via the
// Jaccard fallback. This is the fix for the CI-mode runaway duplication bug:
// without NT, the seeded record only had the first-line title, which gave a
// Jaccard score below the threshold for multi-line improve suggestions.
func TestNewFromPriorJaccardWithNT(t *testing.T) {
	ctx := context.Background()
	key := PRKey{Provider: "github", RepoFullName: "o/r", PRNumber: 1}

	// Simulate an "improve" suggestion from push 1.
	originalBody := "**Suggestions:**\n- Fail fast on Kafka producer init error when enabled\n\n```suggestion\nx := 1\n```"
	c1 := vcs.InlineComment{File: "a.go", Body: originalBody, Severity: vcs.SeverityNit}

	// The marker would have been stamped with the full-body NT on push 1.
	nt := normalizeTitle(originalBody)
	sk := StructuralKey("improve", c1)

	s := NewFromPrior(key, vcs.PriorReview{
		Inline: []vcs.PriorInline{{
			Tool:            "improve",
			File:            "a.go",
			Severity:        "nit",
			StructuralKey:   sk,
			Title:           "**Suggestions:**", // first line only (as parsed by ListCadooArtifacts)
			NormalizedTitle: nt,                 // full-body NT (from nt= marker field)
			ExternalID:      "T1",
		}},
	})

	// Push 2: LLM produces a slightly rephrased body (same intent, tiny wording change).
	rephrasedBody := "**Suggestions:**\n- Fail fast on Kafka producer init error when it is enabled\n\n```suggestion\nx := 1\n```"
	c2 := vcs.InlineComment{File: "a.go", Body: rephrasedBody, Severity: vcs.SeverityNit}

	// Despite the body differing (different StructuralKey), Jaccard should match.
	has, err := s.HasFinding(ctx, key, "improve", c2)
	if err != nil {
		t.Fatalf("HasFinding error: %v", err)
	}
	if !has {
		t.Errorf("rephrased improve suggestion was not deduped: Jaccard should match via NormalizedTitle; "+
			"nt=%q rephrasedNT=%q", nt, normalizeTitle(rephrasedBody))
	}
}

// TestNewFromPriorCarriesResolvedAndLine verifies that NewFromPrior seeds the
// findingRec with Resolved=true and the anchor Line from PriorInline, and that
// HasFinding subsequently suppresses a reworded restatement of the resolved
// finding on the same line via the sticky-suppression path in memoryStore.has.
//
// The test uses a pair of bodies with Jaccard ~0.40: above
// ResolvedSuppressThreshold (0.3) but below SimilarTitleThreshold (0.5), so
// the suppression can only happen via the new resolved-prior branch.
func TestNewFromPriorCarriesResolvedAndLine(t *testing.T) {
	ctx := context.Background()
	key := PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 10}

	// Prior body tokens: {goroutine, leak, handler} — 3 tokens.
	// New body tokens: {goroutine, leak, shutdown, timeout} — 4 tokens.
	// Jaccard = |{goroutine, leak}| / |{goroutine, leak, handler, shutdown, timeout}| = 2/5 = 0.40.
	priorBody := "goroutine leak in handler"
	s := NewFromPrior(key, vcs.PriorReview{
		Inline: []vcs.PriorInline{{
			Tool:            "review",
			File:            "client.go",
			Severity:        "warn",
			StructuralKey:   "sk-carry-test",
			NormalizedTitle: normalizeTitle(priorBody),
			Title:           firstLine(priorBody),
			ExternalID:      "disc-resolved",
			Resolved:        true,
			Line:            25,
			EndLine:         25,
		}},
	})

	// Same line, Jaccard ~0.40 (above 0.3, below 0.5) — should be suppressed
	// via the resolved-prior branch that Task 3 introduces.
	reworded := vcs.InlineComment{
		File:      "client.go",
		Severity:  vcs.SeverityWarn,
		Body:      "goroutine leak on shutdown timeout",
		LineStart: 25,
		LineEnd:   25,
	}
	has, err := s.HasFinding(ctx, key, "review", reworded)
	if err != nil {
		t.Fatalf("HasFinding error: %v", err)
	}
	if !has {
		t.Error("NewFromPrior must carry Resolved+Line so a same-line restatement (Jaccard >= 0.3) is suppressed")
	}

	// Different line, zero token overlap — must NOT be suppressed.
	unrelated := vcs.InlineComment{
		File:      "client.go",
		Severity:  vcs.SeverityWarn,
		Body:      "Unbounded goroutine pool risks memory exhaustion under heavy throughput",
		LineStart: 80,
		LineEnd:   80,
	}
	has2, err := s.HasFinding(ctx, key, "review", unrelated)
	if err != nil {
		t.Fatalf("HasFinding (unrelated) error: %v", err)
	}
	if has2 {
		t.Error("unrelated finding at a different line must NOT be suppressed by the resolved prior")
	}
}

// TestNewFromPriorJaccardLegacyFallback ensures that legacy markers (without
// nt= field, NormalizedTitle=="") still work for exact SK matches (regression
// guard for backward compatibility with push-1 comments posted before the fix).
func TestNewFromPriorJaccardLegacyFallback(t *testing.T) {
	ctx := context.Background()
	key := PRKey{Provider: "github", RepoFullName: "o/r", PRNumber: 1}

	c := vcs.InlineComment{File: "a.go", Body: "Avoid the N+1 query.", Severity: vcs.SeverityWarn}
	sk := StructuralKey("review", c)

	// Legacy seeding: no NormalizedTitle (as if marker had no nt= field).
	s := NewFromPrior(key, vcs.PriorReview{
		Inline: []vcs.PriorInline{{
			Tool:            "review",
			File:            "a.go",
			Severity:        "warn",
			StructuralKey:   sk,
			Title:           "Avoid the N+1 query.",
			NormalizedTitle: "", // legacy — no nt= in marker
			ExternalID:      "T1",
		}},
	})

	// Exact same body → SK match → dedup works regardless of NT.
	has, err := s.HasFinding(ctx, key, "review", c)
	if err != nil || !has {
		t.Errorf("exact SK match should dedup even without NT: has=%v err=%v", has, err)
	}
}
