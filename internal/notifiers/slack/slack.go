// Package slack posts Cadoo run summaries to a Slack channel via an
// incoming-webhook URL. Phase 6 v1 ships the simplest reliable path: text
// + a single block of context fields. Slash-command receiver and full app
// surface are Phase 6.x.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// Notifier posts to a single Slack incoming-webhook URL.
type Notifier struct {
	WebhookURL string
	HTTPClient *http.Client
}

// New constructs a Notifier.
func New(webhookURL string) *Notifier {
	return &Notifier{
		WebhookURL: webhookURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type slackPayload struct {
	Text   string  `json:"text"`
	Blocks []block `json:"blocks,omitempty"`
}

type block struct {
	Type string         `json:"type"`
	Text *blockText     `json:"text,omitempty"`
}

type blockText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// NotifyResult posts a one-line summary plus a markdown block with the tool
// output. Empty Result silently no-ops.
func (n *Notifier) NotifyResult(ctx context.Context, pr *vcs.PullRequest, tool string, res *tools.Result) error {
	if n.WebhookURL == "" || res == nil {
		return nil
	}
	headline := fmt.Sprintf("Cadoo `/%s` on <%s|%s#%d>", tool, pr.URL, pr.RepoFullName, pr.Number)
	body := summarize(res, 1500)
	msg := slackPayload{
		Text: headline,
		Blocks: []block{
			{Type: "section", Text: &blockText{Type: "mrkdwn", Text: headline}},
		},
	}
	if body != "" {
		msg.Blocks = append(msg.Blocks, block{
			Type: "section",
			Text: &blockText{Type: "mrkdwn", Text: body},
		})
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.WebhookURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack post: status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func summarize(res *tools.Result, max int) string {
	parts := []string{}
	if res.Summary != "" {
		parts = append(parts, truncate(res.Summary, max))
	}
	if len(res.InlineComments) > 0 {
		parts = append(parts, fmt.Sprintf("_%d inline finding(s) posted._", len(res.InlineComments)))
	}
	if res.CheckRun != nil {
		parts = append(parts, fmt.Sprintf("_check run: %s — %s_", res.CheckRun.Status, res.CheckRun.Title))
	}
	return strings.Join(parts, "\n\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
