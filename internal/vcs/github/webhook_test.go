package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifySignatureRejectsBadSecret(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, []byte("real-secret"))
	mac.Write(body)
	header := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if err := VerifySignature("real-secret", header, body); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if err := VerifySignature("wrong-secret", header, body); err == nil {
		t.Fatal("expected mismatch")
	}
	if err := VerifySignature("real-secret", "garbage", body); err == nil {
		t.Fatal("expected prefix error")
	}
	if err := VerifySignature("", header, body); err == nil {
		t.Fatal("expected secret-not-configured error")
	}
}
