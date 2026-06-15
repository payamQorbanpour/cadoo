package findings

import (
	"context"
	"path/filepath"
	"strings"
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

func TestStructuralKeyIsLineAgnostic(t *testing.T) {
	// Same finding at different line numbers must share a key, so a
	// later commit that shifts the anchor doesn't defeat dedup.
	c1 := vcs.InlineComment{File: "x.go", LineStart: 3, LineEnd: 3, Severity: "warn", Body: "Missing context deadline on outbound HTTP request"}
	c2 := vcs.InlineComment{File: "x.go", LineStart: 50, LineEnd: 50, Severity: "warn", Body: "Missing context deadline on outbound HTTP request"}
	if StructuralKey("review", c1) != StructuralKey("review", c2) {
		t.Errorf("line shift must not change structural key: %q vs %q",
			StructuralKey("review", c1), StructuralKey("review", c2))
	}
}

func TestStructuralKeyTitleAware(t *testing.T) {
	// Different titles at the same location are different findings, so
	// the key must distinguish them. Near-rephrasings are the jaccard
	// layer's job, not this key's.
	c1 := vcs.InlineComment{File: "x.go", LineStart: 3, Severity: "warn", Body: "Missing context deadline"}
	c2 := vcs.InlineComment{File: "x.go", LineStart: 3, Severity: "warn", Body: "Unbounded goroutine on shutdown"}
	if StructuralKey("review", c1) == StructuralKey("review", c2) {
		t.Error("distinct titles at same location must have distinct keys")
	}
}

func TestStructuralKeyDistinguishesFileAndSeverity(t *testing.T) {
	base := vcs.InlineComment{File: "x.go", LineStart: 3, Severity: "warn", Body: "msg"}
	cases := []vcs.InlineComment{
		{File: "y.go", LineStart: 3, Severity: "warn", Body: "msg"},
		{File: "x.go", LineStart: 3, Severity: "block", Body: "msg"},
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
		// Bold wrapper + a body line: both contribute now that the
		// fingerprint walks past the first line.
		"**Deferred rows.Close() silently discards errors**\n\nBody…": "deferred rows.close() silently discards errors body…",
		"[WARN] Deferred rows.Close() silently discards close errors": "deferred rows.close() silently discards close errors",
		"  plain title  ": "plain title",
	}
	for in, want := range cases {
		if got := normalizeTitle(in); got != want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeTitleSkipsCodeFencesAndHeader(t *testing.T) {
	// The improve tool renders every body as a static **Suggestions:**
	// header followed by the actual action and a fenced code block.
	// The fingerprint must vary with the action text — otherwise every
	// suggestion in a file collapses to the same StructuralKey.
	body1 := "**Suggestions:**\n- Fail fast on Kafka producer init error\n\n```suggestion\nx := 1\n```"
	body2 := "**Suggestions:**\n- Reject invalid payload before queuing\n\n```suggestion\ny := 2\n```"
	a, b := normalizeTitle(body1), normalizeTitle(body2)
	if a == b {
		t.Fatalf("distinct suggestions must produce distinct normalized titles; both = %q", a)
	}
	if strings.Contains(a, "```") || strings.Contains(b, "```") {
		t.Errorf("code fences should be stripped; got %q / %q", a, b)
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

func TestMemoryStoreDedupesExactRepost(t *testing.T) {
	s := NewMemory("")
	ctx := context.Background()
	key := PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}
	c := vcs.InlineComment{File: "run.go", LineStart: 10, LineEnd: 10, Severity: "warn", Body: "Some finding title\n\nBody"}

	if has, _ := s.HasFinding(ctx, key, "review", c); has {
		t.Fatal("fresh store should not report has")
	}
	if err := s.RecordFinding(ctx, key, "review", "", c); err != nil {
		t.Fatalf("record: %v", err)
	}
	if has, _ := s.HasFinding(ctx, key, "review", c); !has {
		t.Fatal("exact repost should dedupe")
	}
}

func TestMemoryStoreDedupesLineShift(t *testing.T) {
	// This is the main bug being fixed: line numbers shift on a new
	// commit, but the same finding should still dedupe.
	s := NewMemory("")
	ctx := context.Background()
	key := PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}
	first := vcs.InlineComment{File: "run.go", LineStart: 10, LineEnd: 10, Severity: "warn", Body: "Kafka producer init may silently fail"}
	shifted := vcs.InlineComment{File: "run.go", LineStart: 42, LineEnd: 42, Severity: "warn", Body: "Kafka producer init may silently fail"}

	_ = s.RecordFinding(ctx, key, "review", "", first)
	if has, _ := s.HasFinding(ctx, key, "review", shifted); !has {
		t.Fatal("line-shifted repost of the same finding must dedupe")
	}
}

func TestMemoryStoreDedupesRephrasingViaJaccard(t *testing.T) {
	s := NewMemory("")
	ctx := context.Background()
	key := PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}
	first := vcs.InlineComment{File: "x.go", LineStart: 5, Severity: "warn", Body: "Deferred rows.Close() silently discards errors"}
	rephrased := vcs.InlineComment{File: "x.go", LineStart: 8, Severity: "warn", Body: "Deferred rows.Close() silently discards close errors"}

	_ = s.RecordFinding(ctx, key, "review", "", first)
	if has, _ := s.HasFinding(ctx, key, "review", rephrased); !has {
		t.Fatal("rephrased title above jaccard threshold should dedupe")
	}
}

func TestMemoryStoreSeparatesByFile(t *testing.T) {
	s := NewMemory("")
	ctx := context.Background()
	key := PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}
	a := vcs.InlineComment{File: "a.go", LineStart: 10, Severity: "warn", Body: "Same title"}
	b := vcs.InlineComment{File: "b.go", LineStart: 10, Severity: "warn", Body: "Same title"}

	_ = s.RecordFinding(ctx, key, "review", "", a)
	if has, _ := s.HasFinding(ctx, key, "review", b); has {
		t.Fatal("same finding text in a different file is a different finding")
	}
}

func TestMemoryStoreSummaryAndSections(t *testing.T) {
	s := NewMemory("")
	ctx := context.Background()
	key := PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}

	if id, _ := s.SummaryID(ctx, key, "review"); id != "" {
		t.Fatal("fresh store should have no summary id")
	}
	_ = s.PutSummaryID(ctx, key, "review", "comment-42")
	if id, _ := s.SummaryID(ctx, key, "review"); id != "comment-42" {
		t.Fatalf("got %q, want comment-42", id)
	}

	_ = s.PutSection(ctx, key, "review", "## Review body")
	_ = s.PutSection(ctx, key, "describe", "## Describe body")
	secs, _ := s.AllSections(ctx, key)
	if len(secs) != 2 {
		t.Fatalf("got %d sections, want 2", len(secs))
	}
	// Sorted by tool name: "describe" < "review".
	if secs[0].Tool != "describe" || secs[1].Tool != "review" {
		t.Errorf("sections not sorted by tool: %+v", secs)
	}
}

func TestMemoryStorePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "findings.json")
	ctx := context.Background()
	key := PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}
	c := vcs.InlineComment{File: "x.go", LineStart: 1, Severity: "warn", Body: "persistent finding"}

	s1 := NewMemory(path)
	if err := s1.RecordFinding(ctx, key, "review", "", c); err != nil {
		t.Fatalf("record: %v", err)
	}
	_ = s1.PutSummaryID(ctx, key, "review", "id-1")

	// Fresh store reads back from disk.
	s2 := NewMemory(path)
	if has, _ := s2.HasFinding(ctx, key, "review", c); !has {
		t.Error("persisted finding should be found after restart")
	}
	if id, _ := s2.SummaryID(ctx, key, "review"); id != "id-1" {
		t.Errorf("persisted summary id lost: got %q", id)
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

// TestMemoryStoreListCarriesStructuralKey verifies that PostedFinding.StructuralKey
// is populated from the findingRec after a RecordFinding + ListPostedFindings
// round-trip. This covers the memory-store path that CI mode (no DB) uses.
func TestMemoryStoreListCarriesStructuralKey(t *testing.T) {
	s := NewMemory("")
	ctx := context.Background()
	key := PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 99}

	// Multi-line body — the kind whose key diverges from firstLine(body).
	body := "**Suggestions:**\n- Fail fast on Kafka producer init error when enabled\n\n```suggestion\nif err != nil { return err }\n```"
	c := vcs.InlineComment{
		File:      "internal/api/run.go",
		LineStart: 42,
		LineEnd:   42,
		Severity:  vcs.SeverityNit,
		Body:      body,
	}
	wantKey := StructuralKey("improve", c)

	if err := s.RecordFinding(ctx, key, "improve", "ext-1", c); err != nil {
		t.Fatalf("RecordFinding: %v", err)
	}
	posted, err := s.ListPostedFindings(ctx, key)
	if err != nil {
		t.Fatalf("ListPostedFindings: %v", err)
	}
	if len(posted) != 1 {
		t.Fatalf("expected 1 posted finding, got %d", len(posted))
	}
	if posted[0].StructuralKey != wantKey {
		t.Errorf("StructuralKey = %q, want %q", posted[0].StructuralKey, wantKey)
	}
}
