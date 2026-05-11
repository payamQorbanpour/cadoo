package findings

import (
	"context"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestFingerprintStable(t *testing.T) {
	c := vcs.InlineComment{File: "x.go", LineStart: 3, LineEnd: 3, Severity: "warn", Body: "hello"}
	a := Fingerprint("review", c)
	b := Fingerprint("review", c)
	if a != b {
		t.Errorf("expected stable fingerprint, got %q vs %q", a, b)
	}
	if len(a) != 16 {
		t.Errorf("expected 16-char fingerprint, got %d", len(a))
	}
}

func TestFingerprintBodyMatters(t *testing.T) {
	c1 := vcs.InlineComment{File: "x.go", LineStart: 3, Body: "v1 message"}
	c2 := vcs.InlineComment{File: "x.go", LineStart: 3, Body: "v2 message"}
	if Fingerprint("review", c1) == Fingerprint("review", c2) {
		t.Error("body change should yield different fingerprint")
	}
}

func TestStructuralKeyIgnoresBody(t *testing.T) {
	c1 := vcs.InlineComment{File: "x.go", LineStart: 3, LineEnd: 3, Severity: "warn", Body: "first phrasing"}
	c2 := vcs.InlineComment{File: "x.go", LineStart: 3, LineEnd: 3, Severity: "warn", Body: "second phrasing"}
	if StructuralKey("review", c1) != StructuralKey("review", c2) {
		t.Error("structural key must collapse rephrased bodies at the same location")
	}
}

func TestStructuralKeyDistinguishesLocation(t *testing.T) {
	base := vcs.InlineComment{File: "x.go", LineStart: 3, LineEnd: 3, Severity: "warn", Body: "msg"}
	cases := []vcs.InlineComment{
		{File: "y.go", LineStart: 3, LineEnd: 3, Severity: "warn", Body: "msg"},
		{File: "x.go", LineStart: 4, LineEnd: 4, Severity: "warn", Body: "msg"},
		{File: "x.go", LineStart: 3, LineEnd: 3, Severity: "block", Body: "msg"},
	}
	bk := StructuralKey("review", base)
	for _, c := range cases {
		if StructuralKey("review", c) == bk {
			t.Errorf("expected distinct structural key for %+v", c)
		}
	}
}

func TestNormalizeTitleStripsTagAndEmphasis(t *testing.T) {
	cases := map[string]string{
		"**Deferred rows.Close() silently discards errors**\n\nBody…": "deferred rows.close() silently discards errors",
		"[WARN] Deferred rows.Close() silently discards close errors": "deferred rows.close() silently discards close errors",
		"  plain title  ": "plain title",
	}
	for in, want := range cases {
		if got := normalizeTitle(in); got != want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJaccardCatchesRephrase(t *testing.T) {
	// The two titles from the duplicate-comment incident.
	a := tokenize(normalizeTitle("Deferred rows.Close() silently discards errors"))
	b := tokenize(normalizeTitle("Deferred rows.Close() silently discards close errors"))
	score := jaccard(a, b)
	if score < SimilarTitleThreshold {
		t.Errorf("expected Jaccard %.2f ≥ threshold %.2f for paraphrased title", score, SimilarTitleThreshold)
	}
}

func TestJaccardSeparatesDistinctFindings(t *testing.T) {
	a := tokenize(normalizeTitle("Deferred rows.Close() silently discards errors"))
	b := tokenize(normalizeTitle("Missing context deadline on outbound HTTP request"))
	score := jaccard(a, b)
	if score >= SimilarTitleThreshold {
		t.Errorf("unrelated titles should be below threshold; got %.2f", score)
	}
}

func TestTokenizeDropsStopwordsAndShorts(t *testing.T) {
	got := tokenize("the is on of x ab cdef")
	want := map[string]bool{"ab": true, "cdef": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want keys %v", got, want)
	}
	for _, tok := range got {
		if !want[tok] {
			t.Errorf("unexpected token %q", tok)
		}
	}
}

func TestNilStoreIsNoop(t *testing.T) {
	var s *Store
	ctx := context.Background()
	c := vcs.InlineComment{File: "x.go", LineStart: 1}
	if got, err := s.HasFinding(ctx, PRKey{}, "review", c); got || err != nil {
		t.Errorf("nil HasFinding: %v %v", got, err)
	}
	if err := s.RecordFinding(ctx, PRKey{}, "review", "", c); err != nil {
		t.Errorf("nil RecordFinding: %v", err)
	}
	if got, err := s.ListPostedFindings(ctx, PRKey{}); got != nil || err != nil {
		t.Errorf("nil ListPostedFindings: %v %v", got, err)
	}
	if id, err := s.SummaryID(ctx, PRKey{}, "review"); id != "" || err != nil {
		t.Errorf("nil SummaryID: %q %v", id, err)
	}
	if err := s.PutSummaryID(ctx, PRKey{}, "review", "1"); err != nil {
		t.Errorf("nil PutSummaryID: %v", err)
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("hello\nworld"); got != "hello" {
		t.Errorf("got %q", got)
	}
	if got := firstLine("single"); got != "single" {
		t.Errorf("got %q", got)
	}
}
