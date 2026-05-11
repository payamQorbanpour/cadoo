package learnings

import "testing"

func TestRuleZeroValue(t *testing.T) {
	var r Rule
	if r.Weight != 0 {
		t.Errorf("zero weight: %v", r.Weight)
	}
}

func TestReactionConstants(t *testing.T) {
	if Accept == Reject {
		t.Fatal("Accept must differ from Reject")
	}
	if Accept != "accept" || Reject != "reject" {
		t.Errorf("unexpected reaction strings: %q %q", Accept, Reject)
	}
}
