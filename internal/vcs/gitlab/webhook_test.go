package gitlab

import "testing"

func TestVerifyToken(t *testing.T) {
	if err := VerifyToken("s3cret", "s3cret"); err != nil {
		t.Errorf("expected ok, got %v", err)
	}
	if err := VerifyToken("s3cret", "wrong"); err == nil {
		t.Error("expected mismatch")
	}
	if err := VerifyToken("", "anything"); err == nil {
		t.Error("expected secret-not-configured")
	}
}
