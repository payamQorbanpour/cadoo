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

func TestNilStoreIsNoop(t *testing.T) {
	var s *Store
	ctx := context.Background()
	if got, err := s.HasFinding(ctx, PRKey{}, "fp"); got || err != nil {
		t.Errorf("nil HasFinding: %v %v", got, err)
	}
	if err := s.RecordFinding(ctx, PRKey{}, "review", "fp", "", vcs.InlineComment{}); err != nil {
		t.Errorf("nil RecordFinding: %v", err)
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
