package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DoJSON POSTs body as application/json to url with the given Authorization
// header value (used verbatim, no scheme prefix added). It retries on transient
// network errors and on HTTP 502/503/504 with 1s then 3s backoff (3 attempts
// total). The returned response, when non-nil, has not had Body read or closed.
func DoJSON(ctx context.Context, client *http.Client, method, url string, body []byte, authorization string) (*http.Response, error) {
	backoffs := []time.Duration{0, time.Second, 3 * time.Second}
	var lastErr error
	for attempt, wait := range backoffs {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d/%d: %w", attempt+1, len(backoffs), err)
			continue
		}
		if resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusServiceUnavailable ||
			resp.StatusCode == http.StatusGatewayTimeout {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("attempt %d/%d: status %d: %s", attempt+1, len(backoffs), resp.StatusCode, string(b))
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}
