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
