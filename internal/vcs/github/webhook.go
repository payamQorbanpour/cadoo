package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	gogithub "github.com/google/go-github/v66/github"
)

// VerifySignature returns nil if signatureHeader (X-Hub-Signature-256) matches
// the HMAC-SHA256 of body keyed with secret. The header is expected to be in
// the form "sha256=<hex>".
func VerifySignature(secret string, signatureHeader string, body []byte) error {
	if secret == "" {
		return errors.New("webhook secret not configured")
	}
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return errors.New("invalid signature header (missing sha256= prefix)")
	}
	expected := signatureHeader[len("sha256="):]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	actual := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(actual)) {
		return errors.New("signature mismatch")
	}
	return nil
}

// ParseEvent decodes a GitHub webhook payload by event type. The result is
// one of go-github's *Event types (e.g. *gogithub.PullRequestEvent).
func ParseEvent(eventType string, body []byte) (any, error) {
	ev, err := gogithub.ParseWebHook(eventType, body)
	if err != nil {
		return nil, fmt.Errorf("parse %s event: %w", eventType, err)
	}
	return ev, nil
}
