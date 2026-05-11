package slop

import "testing"

func TestDetectEmptyBodyAndGenericTitle(t *testing.T) {
	r := Detect("update", "", 50, 10, 5)
	if !r.IsSlop {
		t.Errorf("expected slop, got score=%f reasons=%v", r.Score, r.Reasons)
	}
}

func TestDetectGoodPRPasses(t *testing.T) {
	r := Detect("Add caching to user service", "Reduces p95 latency on /me from 450ms to 80ms by caching the role lookup. Test plan: hit the endpoint 100x and confirm < 100ms p95.", 30, 5, 2)
	if r.IsSlop {
		t.Errorf("expected non-slop, got %+v", r)
	}
}

func TestDetectLargeUnexplainedDiff(t *testing.T) {
	r := Detect("Various changes", "", 1500, 200, 40)
	if !r.IsSlop {
		t.Errorf("expected slop for large unexplained diff, got %+v", r)
	}
}

func TestDetectScoreCappedAtOne(t *testing.T) {
	r := Detect("update", "", 5000, 1000, 100)
	if r.Score > 1.0 {
		t.Errorf("score %f > 1", r.Score)
	}
}
