package gitlab

import (
	"crypto/subtle"
	"errors"
	"fmt"

	glab "gitlab.com/gitlab-org/api/client-go"
)

// VerifyToken returns nil if the X-Gitlab-Token header matches the secret
// configured for the project. GitLab webhook secrets are bare tokens — there
// is no HMAC over the body the way GitHub does it.
func VerifyToken(secret, headerToken string) error {
	if secret == "" {
		return errors.New("webhook secret not configured")
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(headerToken)) != 1 {
		return errors.New("token mismatch")
	}
	return nil
}

// ParseEvent decodes a GitLab webhook payload by event type. The result is
// one of go-gitlab's *Event types (e.g. *glab.MergeEvent).
func ParseEvent(eventType string, body []byte) (any, error) {
	ev, err := glab.ParseWebhook(glab.EventType(eventType), body)
	if err != nil {
		return nil, fmt.Errorf("parse %s event: %w", eventType, err)
	}
	return ev, nil
}
